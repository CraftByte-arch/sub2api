---
change: refactor-image-studio-edit-storage
verified-at: 2026-07-19
mode: full
result: pass
---

# Image Studio Edit Input Storage Verification

## Conclusion

The implementation satisfies the OpenSpec proposal, design, delta specification, and all 22 tracked tasks. No Critical or Important findings remain. Production deployment was not performed.

## Scope Verified

- One to four ordered reference images, browser-side same-dimension WebP compression at quality 0.72, and unchanged mask upload.
- Multipart edit-job creation with server-managed files under `DATA_DIR` and relative-path-only database metadata.
- API Key Worker multipart `/v1/images/edits` forwarding and OAuth Responses edit reconstruction.
- Input deletion after durable completion, next-cleanup convergence after transient deletion failure, TTL expiration, user deletion, and orphan cleanup.
- Legacy data URL materialization, terminal migration redaction, and stale-running atomic redaction after path persistence failure.
- Shared storage health gating limited to Image Studio asynchronous creation and claims.

## Evidence

- `openspec status --change refactor-image-studio-edit-storage --json`: all artifacts complete.
- `openspec validate --type change refactor-image-studio-edit-storage --json`: 1 passed, 0 failed.
- `openspec validate refactor-image-studio-edit-storage --strict`: valid.
- `go test ./... -count=1` in `backend/`: pass.
- `go vet ./...` in `backend/`: pass.
- Change-scoped golangci-lint v2.9: 0 issues.
- Focused repository and service suites: pass.
- PostgreSQL 18.1 / Redis 8.4 integration suite: pass, including four-image API Key, one-image authenticated download, OAuth, production cleanup-loop TTL, durable cleanup retry, stale legacy redaction, and user deletion.
- Image Studio frontend focused suites: 13 files / 221 tests pass.
- Full frontend suite: 168 files / 1207 tests pass.
- Frontend lint: 0 errors and 3 pre-existing warnings; typecheck and production build pass.
- Fresh Comet build guard command `make build`: pass for backend and frontend.
- Task-level spec and quality reviews: approve.
- Whole-change review findings on cleanup convergence and legacy payload retention were fixed in `4e5d9ecf5c16c7180f7d19ec253bb3c558f344a2` and re-reviewed: approve.

## Known Baseline And Residual Risk

- Full-tree golangci-lint still reports eight pre-existing baseline findings; the change introduces no new lint issue. This report does not claim full CI lint is clean.
- `ListExpiredInputs` uses an `OR` eligibility condition plus `ORDER BY CASE`. Under a sustained set of permanently undeletable durable rows, existing TTL rows can be delayed across cleanup rounds and the current index cannot provide the requested ordering directly. The cleanup batch is bounded and ordinary transient failures converge; a later change can split durable and TTL queries into independent quotas or add a targeted partial index.
- The integration harness starts and pings Redis, but the Image Studio fixture does not inject a Redis-backed dependency. PostgreSQL, filesystem, handler, Worker, cleanup, and download paths are exercised directly.

## Operational Boundary

Deployment must remain schema-first and requires every Image Studio API/Worker instance to share a persistent writable `DATA_DIR`. No production or `mdc1` operation was executed during verification.
