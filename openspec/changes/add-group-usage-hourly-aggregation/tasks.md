## 1. Durable Storage

- [x] 1.1 Add PostgreSQL migrations for group-hour buckets, aggregate state, and required indexes.
- [x] 1.2 Implement the group usage aggregation repository with atomic bucket writes and compatible summary reads.

## 2. Usage Log Integration

- [x] 2.1 Wire synchronous usage-log inserts to increment only newly inserted grouped logs transactionally.
- [x] 2.2 Wire normal and best-effort batch inserts, including fallbacks, to aggregate only newly inserted grouped logs.
- [x] 2.3 Remove the legacy query implementation from the active read path while retaining it as the initialization fallback.

## 3. Automatic Backfill And Retention

- [x] 3.1 Implement the PostgreSQL-coordinated resumable backfill worker and aggregate verification.
- [x] 3.2 Start the worker from application wiring without Redis and add retention cleanup for group-hour buckets.

## 4. Verification

- [x] 4.1 Add repository and service tests for atomic increments, idempotent retries, compatibility, and soft-deleted groups.
- [x] 4.2 Run focused tests and full backend validation; document any environment limitations.

Validation: `cd backend && go test ./...` passed. The PostgreSQL integration suite was invoked but skipped by its test harness because the local Docker daemon is unavailable; live PostgreSQL execution remains to be verified in CI or another Docker-enabled environment.
