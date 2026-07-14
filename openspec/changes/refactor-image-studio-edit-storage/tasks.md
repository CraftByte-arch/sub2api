## 1. Database And Storage Foundation

- [x] 1.1 Add an image studio input-storage migration with ordered input paths, mask path, input expiration/deletion metadata, terminal legacy payload redaction, and migration regression coverage.
- [x] 1.2 Extend the ImageStudio job domain model and repository reads/writes for input file metadata without exposing paths in user responses.
- [ ] 1.3 Implement a root-confined input storage service with temporary files, atomic finalize, MIME/content/size validation, rollback, and idempotent directory removal.
- [ ] 1.4 Implement active legacy data URL materialization and payload redaction for one to four references plus an optional mask.

## 2. Multipart Job Creation And Frontend Upload

- [ ] 2.1 Accept multipart edit job creation with repeated ordered `image` fields, an optional `mask`, metadata fields, and full rollback on validation or database failure.
- [ ] 2.2 Keep JSON generation job creation behavior unchanged and reject new edit-job data URL payloads with a clear compatibility error.
- [ ] 2.3 Update Image Studio to support one to four references, compress each reference to same-dimension WebP at quality 0.72, and upload compressed Files through multipart without data URL conversion.
- [ ] 2.4 Preserve the mask binary and transparency, stop submission on any compression failure, and cover frontend cardinality, ordering, compression, and multipart behavior with tests.

## 3. Protocol-Correct Worker Execution

- [ ] 3.1 Load and validate stored input files before execution and classify missing, expired, corrupt, or unsafe paths as terminal storage errors.
- [ ] 3.2 Build API Key `/v1/images/edits` multipart requests with ordered repeated image parts, optional mask, and all supported edit metadata.
- [ ] 3.3 Convert stored files through the existing OAuth Responses edit path without persisting image bytes and preserve existing billing/result decoding behavior.
- [ ] 3.4 Add Worker regression tests for one/four images, ordering, mask handling, API Key multipart, OAuth conversion, retries, and terminal storage failures.

## 4. Input Lifecycle And Cleanup

- [ ] 4.1 Delete input files idempotently after result assets and settlement recovery data are durable, while retaining output history under the existing output retention policy.
- [ ] 4.2 Retain inputs for retryable failures, expire queued/failed inputs at the configured safe TTL, mark queued jobs `input_expired`, and never delete files from an actively running job.
- [ ] 4.3 Extend user task deletion to remove input and output directories before deleting the database record.
- [ ] 4.4 Add bounded orphan-directory scanning and cleanup with root confinement, grace periods, and repository/filesystem failure tests.

## 5. Verification And Operational Readiness

- [ ] 5.1 Run focused backend migration, repository, handler, storage, Worker, billing, and cleanup tests, then run the complete backend test suite and linters required by the repository.
- [ ] 5.2 Run Image Studio frontend unit tests, type checking, and production build, including multipart upload and four-reference scenarios.
- [ ] 5.3 Verify in an integration environment that new edit rows contain no base64, API Key upstream requests are multipart, OAuth edits still succeed, inputs disappear after success/TTL/delete, and outputs remain downloadable until output retention expires.
- [ ] 5.4 Document deployment ordering, shared `DATA_DIR` requirements, legacy cleanup metrics, database growth checks, and roll-forward recovery steps.
