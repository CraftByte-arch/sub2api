---
comet_change: refactor-image-studio-edit-storage
role: technical-design
canonical_spec: openspec
---

# Image Studio Edit Input Storage Design

## Context

Image Studio currently serializes edit reference images and masks as data URLs inside `image_studio_jobs.request_payload`. The asynchronous worker forwards that JSON body to `/v1/images/edits`. This fails for API Key accounts because the Images edit endpoint expects multipart file parts, and it also makes PostgreSQL retain large user-provided image payloads after the task no longer needs them.

The implementation will move edit inputs into the existing persistent `DATA_DIR` boundary. New edit jobs upload binary files, store only controlled relative paths in PostgreSQL, and reconstruct the protocol-specific upstream request when a worker executes the job. Generation jobs and output retention remain unchanged.

OpenSpec is the canonical capability specification. This document defines the implementation structure and failure-handling details.

## Goals

- Accept one to four ordered reference images and one optional mask for asynchronous edit jobs.
- Compress references in the browser to same-dimension WebP at quality 0.72 before upload while preserving mask fidelity.
- Keep image bytes and data URLs out of new database rows.
- Send a correct multipart `/v1/images/edits` request for API Key accounts and retain the existing OAuth Responses edit path.
- Delete edit inputs after durable completion, expiration, user deletion, failed creation, or orphan detection.
- Migrate active legacy data URL jobs on first claim and redact terminal legacy payloads.
- Degrade only the Image Studio asynchronous subsystem when its shared storage is unavailable.

## Non-Goals

- No object storage integration.
- No change to the public gateway `/v1/images/edits` contract.
- No support for more than four references or more than one mask.
- No lossy mask conversion or dimension changes.
- No change to generated output retention, thumbnail behavior, or billing semantics.
- No cross-resource transaction between PostgreSQL and the filesystem.

## Component Boundaries

### Frontend

`frontend/src/extensions/image-studio/ImageStudioView.vue` owns selection state and submission UX. It must preserve reference order, enforce the one-to-four limit, stop the entire submission if any conversion fails, and leave the mask binary unchanged.

`frontend/src/extensions/image-studio/imageStudioApi.ts` owns wire encoding. Generation continues to call the existing JSON job endpoint. Edit creation builds `FormData`, appends each compressed reference under the repeated `image` field in display order, appends the optional `mask`, and encodes the remaining edit parameters as form fields. It must not set the multipart `Content-Type` header manually because the browser supplies the boundary.

A small compression helper should decode each source image, render it at its original pixel dimensions, and call `canvas.toBlob("image/webp", 0.72)`. At most four images are processed per submission. The helper returns `File` objects with deterministic WebP names and rejects unsupported or undecodable inputs before any network request starts.

### HTTP Handler And Service

`backend/internal/handler/image_studio_job_handler.go` remains the resource endpoint and dispatches by content type:

- JSON requests retain the generation flow.
- Multipart requests are accepted only for edit jobs.
- New JSON edit payloads containing legacy `images`, `mask`, or data URLs are rejected with a compatibility error.

The handler parses scalar form fields into the existing edit request model, streams uploaded files into `ImageStudioInputStore`, and calls the job service only after storage finalization succeeds. Authentication, available-group checks, cost estimation, and response serialization remain shared with generation jobs.

`backend/internal/service/image_studio_job_service.go` orchestrates storage and repository operations. It computes `input_expires_at` using the independent input-retention setting, builds a sanitized non-binary `request_payload`, and rolls back the finalized upload directory if job creation fails.

### Input Store

Add a focused service under `backend/internal/service` that owns all Image Studio input filesystem behavior. Its public operations should cover:

```go
StageEditInputs(ctx context.Context, images []UploadedFile, mask *UploadedFile) (*StagedEditInputs, error)
OpenInputs(paths []string, maskPath *string) (*OpenedEditInputs, error)
RemoveInputs(paths []string, maskPath *string) error
MaterializeLegacy(ctx context.Context, images []string, mask *string) (*StagedEditInputs, error)
Probe() error
CleanupOrphans(ctx context.Context, referenced map[string]struct{}, now time.Time) error
```

Exact types may follow local conventions, but the storage boundary must remain independent from PostgreSQL. The store resolves every path beneath one root:

```text
DATA_DIR/image-studio/
  inputs/<random-upload-id>/image-01.webp
  inputs/<random-upload-id>/image-02.webp
  inputs/<random-upload-id>/mask.png
  inputs/<random-upload-id>/.spool-<attempt-id>.multipart
```

Database paths are relative to `DATA_DIR/image-studio`, use normalized separators, and are generated only by the server. Every open or removal operation rejects absolute paths, `..` components, symlink escapes, and any resolved path outside the root.

Each upload is written into a random private staging directory. A file is copied to a temporary name with a bounded reader, closed, decoded, and validated before it is renamed atomically to its final name. Reference validation covers count, per-file size, detected MIME, supported format, dimensions, and decodability. Mask validation additionally requires a supported transparency-capable format, usable alpha content, and dimensions equal to the first reference. Any failure removes the whole staging directory.

The store finalizes all files before the repository creates the job. If PostgreSQL creation fails, the service removes the finalized directory. A crash between these steps leaves an unreferenced random directory, which the orphan scanner removes after a grace period.

### Repository And Schema

Add one migration after the current latest migration. `image_studio_jobs` gains:

```text
input_image_paths JSONB NOT NULL DEFAULT '[]'::jsonb
input_mask_path TEXT NULL
input_expires_at TIMESTAMPTZ NULL
input_deleted_at TIMESTAMPTZ NULL
```

`input_image_paths` is an ordered JSON string array. The repository validates its decoded cardinality and shape; paths are internal fields and must not be exposed by handler response DTOs. `request_payload` keeps only model, prompt, size, quality, output format, and other non-binary fields needed to reconstruct the attempt.

The repository needs guarded operations for these transitions:

- Persist legacy paths and redact legacy binary payload fields in one update.
- Claim a queued job only if its input has not expired.
- Atomically fail an expired queued job with `input_expired`.
- List expired non-running inputs in bounded batches.
- Mark inputs deleted without changing output lifecycle fields.
- List upload directory references for orphan reconciliation.

The migration directly removes `images` and `mask` from terminal legacy edit payloads. It does not rewrite active legacy payloads because their files do not yet exist. Migration tests must assert both field creation and terminal redaction behavior.

### Worker

`backend/internal/service/image_studio_job_worker.go` loads edit inputs before calling an upstream provider. It validates that paths exist, remain root-confined, are readable, and have not passed `input_expires_at`. Input path, missing-file, corrupt-file, and expiry errors are terminal storage failures and never enter the upstream retry loop.

For API Key accounts, add an edit-specific request builder that writes a multipart body to a short-lived spool file. It appends repeated `image` parts in database order, the optional `mask`, and the supported scalar edit fields. The HTTP request streams the spool file and supplies the generated content type and a stable content length. The spool is removed in a deferred cleanup after success, upstream failure, cancellation, or response parse failure. Stale spool files are also eligible for bounded cleanup.

For OAuth accounts, reuse the current Responses image-edit conversion. Stored files may be encoded as data URLs only in memory for the duration of that attempt, under the same total input-size limit used by upload validation. Those temporary values must never be written to the job payload or logs.

Legacy active edit jobs with no stored paths are materialized before normal input validation. The worker decodes one to four data URL references and the optional mask through the same store validation path, then performs one repository update that writes the paths and redacts `images` and `mask`. If the update fails, it removes the new directory. If the process crashes after file finalization, orphan cleanup handles the directory. Invalid legacy cardinality or bytes produce a terminal compatibility error and redact binary fields where possible.

## State And Data Flow

```text
browser files
  -> same-size WebP compression for references
  -> multipart job request
  -> staged and validated files
  -> atomic file finalization
  -> queued database row with relative paths
  -> worker claim guarded by input expiry
  -> protocol-specific upstream request
  -> output assets plus settlement payload durable
  -> input directory deletion and input_deleted_at
  -> existing settlement and output-retention flow
```

The key lifecycle transitions are:

| State | Input behavior |
| --- | --- |
| Creation fails before DB insert | Remove staging/finalized upload directory. |
| `queued`, before TTL | Retain inputs for claim. |
| `queued`, after TTL | Guarded transition to failed `input_expired`, then delete inputs. |
| `running` | Cleanup skips the job even if TTL passes. |
| Retryable upstream failure | Requeue and retain inputs until TTL. |
| Terminal upstream failure | Retain inputs until TTL, then cleanup. |
| Terminal storage failure | Do not call upstream; mark failed and retain any safely addressable inputs until TTL for consistent terminal-task handling. |
| Output and settlement recovery data durable | Delete inputs immediately even while settlement retry continues. |
| User deletes job | Remove input and output files before deleting the row. |

Input deletion is idempotent. Missing files count as already deleted. The repository sets `input_deleted_at` after removal; if that update fails, the next cleanup pass repeats the operation safely. Deletion of inputs after durable completion must not roll back a successful upstream result.

## Cleanup And Availability

The existing cleanup loop gains bounded phases:

1. Atomically expire queued jobs and select expired failed/non-running input rows.
2. Remove referenced input directories and mark their deletion timestamps.
3. Preserve the existing output asset cleanup behavior.
4. Scan old input directories that have no repository reference and exceed the orphan grace period.
5. Remove stale multipart spool files after a shorter spool grace period.

Cleanup never removes files for a `running` job. Repository selection and state changes must make expiration and worker claim mutually exclusive. All cleanup operations are root-confined and idempotent, and one failed item must not abort the rest of a bounded batch.

At startup and periodically thereafter, the input store probes its root by creating, reading, and deleting a private file. Probe failure marks only Image Studio asynchronous storage unavailable. The job creation endpoint returns 503, and workers pause new claims while continuing the periodic probe. Unrelated APIs and existing output downloads remain available.

Every application instance that accepts or executes Image Studio jobs must mount the same persistent `DATA_DIR`. A local per-container directory is unsupported because the instance that claims a job may differ from the instance that accepted its upload.

## Configuration

Add `image_studio_input_retention_hours` to the existing settings model, parsing, update, admin API, and admin UI. Its default is 24 and it is independent of `image_studio_retention_value` / `image_studio_retention_unit`, which continue to control output files. Invalid or non-positive values fall back to 24 hours.

Upload limits and cleanup batch/grace values should use existing configuration patterns. If no suitable setting already exists, keep conservative service constants in this change rather than expanding the public administrative surface beyond the confirmed retention setting.

## Error Classification

Use stable error codes so task history and operations can distinguish causes:

- `input_expired`: input TTL elapsed before claim.
- `input_missing`: a referenced file does not exist.
- `input_invalid`: bytes cannot be decoded or do not match stored expectations.
- `input_path_invalid`: path fails root confinement.
- `input_storage_unavailable`: storage probe or required filesystem operation failed.
- `legacy_input_invalid`: a legacy data URL task cannot be materialized.

These errors are terminal for the current task and do not trigger an upstream request. Provider transport errors, rate limits, and retryable 5xx responses continue through the existing retry classification and retain inputs while they remain unexpired.

## Deployment And Rollback

Deployment is schema-first:

1. Apply the additive migration and terminal-payload redaction.
2. Deploy a backend that reads both stored paths and active legacy data URLs, with shared `DATA_DIR` mounted and storage probes passing.
3. Deploy the multipart frontend so no new data URL edit jobs are created.
4. Observe storage health, legacy materialization, database payload size, orphan cleanup, and input deletion metrics.

Roll forward is the preferred recovery. An old backend cannot execute new path-only jobs. Before a forced rollback, disable new edit submissions and drain or terminally resolve path-only queued/running jobs. PostgreSQL physical file size may not shrink immediately after JSONB redaction; normal vacuum reclaims reusable space, while `VACUUM FULL` remains a separate maintenance decision.

## Testing Strategy

### Frontend

- Compression preserves width and height, produces WebP quality 0.72, supports four ordered references, and rejects conversion failures without sending a request.
- Edit creation emits multipart with repeated ordered `image` fields and an unchanged mask; generation creation remains JSON.
- The UI enforces one-to-four references, one mask, and stable ordering.

### Backend Unit And Repository

- Migration fields, defaults, terminal JSONB redaction, and active legacy preservation.
- Root confinement, symlink/path traversal rejection, MIME spoofing, decode failure, size limits, mask alpha/dimensions, atomic finalization, rollback, and idempotent removal.
- Repository scan/write order, hidden path fields, guarded claim versus expiry, deletion timestamps, and bounded orphan-reference queries.
- Handler parsing for one and four images, duplicate mask rejection, JSON generation compatibility, edit data URL rejection, and rollback after validation or DB failure.

### Worker And Lifecycle

- API Key multipart shape, reference ordering, optional mask, scalar metadata, streamed spool behavior, and cleanup on every exit path.
- OAuth conversion without database persistence of bytes.
- Retryable provider failures retain input; terminal storage failures never call upstream.
- Successful result persistence deletes input before settlement-only retries.
- Queued expiry is atomic, running cleanup is skipped, user deletion removes both input and output, and orphan cleanup is bounded.
- Active legacy materialization updates paths and redacts payload atomically; invalid legacy jobs fail without repeated decoding attempts.
- Storage probe failure rejects creation and pauses claims without breaking unrelated APIs.

### Integration And Regression

- New edit rows contain no base64 or data URLs.
- One-image and four-image API Key edits reach upstream as multipart and succeed.
- OAuth edit behavior and billing/result decoding remain unchanged.
- Inputs disappear after success, TTL, and manual deletion while outputs remain downloadable until the existing output retention expires.
- Run focused backend and frontend suites, then the repository's full backend tests, frontend type check, and production build.

## Observability

Log job ID and upload directory ID, never filenames supplied by users or image bytes. Add counters or structured events for upload validation failures, storage probe status, legacy materialization, terminal input errors, input cleanup, orphan cleanup, and spool cleanup. Operators should be able to detect persistent database growth, a missing shared mount, and cleanup backlog without inspecting user content.

## Scope Control

Implementation should extend the existing Image Studio handler, service, worker, repository, settings, and frontend extension. The input store and multipart builder are new focused units because they isolate filesystem and protocol concerns. Avoid refactoring the public gateway image handler, shared billing transaction, unrelated media storage, or the general API client.
