## Context

`GET /api/v1/admin/groups/usage-summary` currently joins `groups` with every retained `usage_logs` row and aggregates all historical cost at read time. The production database retains roughly 88 days of logs and runs on a single core, so this low-frequency administrative read can still monopolize CPU.

Usage logs have idempotent inserts and several persistence paths: synchronous single insert, normal batching, best-effort batching, and synchronous fallback. A counter updated outside the successful usage-log insertion can double count retries or lose data after a partial failure.

## Goals / Non-Goals

**Goals:**

- Preserve the current HTTP endpoint, service method, and response shape.
- Store group cost in durable PostgreSQL UTC-hour buckets without Redis.
- Increment a bucket only when its source usage-log row was newly persisted.
- Backfill retained history automatically, resumably, and at a bounded rate after deployment.
- Return only non-deleted groups, matching the administrator group list.
- Preserve existing rolling-retention semantics for `total_cost`.

**Non-Goals:**

- Permanent lifetime group usage beyond the `usage_logs` retention window.
- Replacing other dashboard aggregation tables or their scheduler.
- Adding a user-visible progress or configuration interface.
- Caching the administrative response in Redis.

## Decisions

### Use one durable hourly table, not a total counter

`group_usage_hourly(group_id, bucket_start, actual_cost, updated_at)` has a `(group_id, bucket_start)` primary key and a `bucket_start` cleanup index. Historical total is the sum of its buckets; today is the sum of today's buckets.

This adds approximately one row per group per active hour, which is tiny relative to retained raw logs. A separate lifetime-total table would create another hot row per group and would change the existing retained-history meaning of the UI.

### Update buckets atomically only for inserted usage logs

PostgreSQL `INSERT ... ON CONFLICT ... DO UPDATE SET actual_cost = table.actual_cost + EXCLUDED.actual_cost` performs a row-locked atomic increment. The usage-log repository will execute this update in the same transaction as the raw log insertion, after identifying rows whose idempotent insert succeeded.

Normal and best-effort batches will combine newly inserted logs by group before applying bucket increments. This reduces contention for a high-traffic group and prevents duplicate updates for idempotent retries. A standalone `Add(groupID, delta)` helper remains intentionally small, but it is not used as an independent post-insert asynchronous action.

### Keep all new aggregation logic in one repository file

`backend/internal/repository/group_usage_aggregation.go` owns bucket SQL, compatible reads, transactional increment helpers, and resumable backfill state operations. Existing repository files only receive narrow integration hooks for their current insert paths and retention cleanup.

The existing `GetAllGroupUsageSummary(ctx, todayStart)` method remains the service contract. It delegates to the new implementation, so the handler, route, and frontend require no changes.

### Read only complete aggregate data

The aggregate state table records whether the initial backfill is ready. Before then, the compatible query uses the legacy raw-log aggregation to avoid returning partial history. Newly inserted logs still increment their buckets during this period. Backfill replaces each completed range from the raw logs, so these live increments are preserved without a separate cutover handoff. Once verification succeeds, reads use hourly buckets. The aggregate query filters `groups.deleted_at IS NULL`.

For browser timezones whose local midnight is not aligned to a UTC hour, the query sums complete hourly buckets and reads at most the initial partial hour from raw logs. This preserves the legacy timezone contract without reintroducing an all-history scan.

### Run automatic, resumable backfill with PostgreSQL coordination

The application starts a dedicated background worker after repository wiring. State and progress are persisted in PostgreSQL; a PostgreSQL advisory lock elects one worker across replicas without Redis. The worker recomputes a bounded historical window in chronological chunks, persists progress after each successful chunk, and pauses between chunks.

Backfill uses idempotent replacement of bounded completed-hour bucket values and validates each chunk against the matching raw-log range for active groups. Because a live insert updates both its raw row and bucket in one statement, a concurrent replacement either includes the row or leaves its later atomic increment intact. The final transaction recomputes the remaining tail and marks reads ready without issuing a second full-history verification scan.

## Risks / Trade-offs

- **[Concurrent writes to one group-hour row]** -> Batch inserted logs by group and make one upsert per group per batch; no per-request total table is added.
- **[Backfill consumes single-core database CPU]** -> Use bounded chunks, statement timeouts, persisted progress, and a pause between chunks. The worker never performs one full-history grouping query.
- **[Automatic worker restarts or multi-replica deployment]** -> Persist the cursor and use a PostgreSQL advisory lock; completed chunks are safe to recompute.
- **[Partial history shown during initialization]** -> Keep the legacy query until aggregate verification marks state ready.
- **[Data after soft deletion appears in the API]** -> Filter deleted groups in the aggregate read, matching Ent's group list behavior.
- **[Non-hour timezone offsets]** -> Query only the first partial hour in `usage_logs`; all remaining today data comes from buckets.

## Migration Plan

1. Apply forward-only schema migration for the bucket and aggregate-state tables and indexes.
2. Deploy the application. Live inserts begin updating group-hour buckets transactionally.
3. The leader worker automatically backfills completed historical chunks, including a final recomputation after the deployment hour closes.
4. Compare active-group aggregate results to the legacy calculation and mark the state ready only after a match.
5. Read the hourly table after readiness. During a rollback or failed backfill, leave state unready so the existing raw query remains authoritative.
6. Delete buckets older than the configured raw usage-log retention cutoff to retain the prior `total_cost` semantics.

## Open Questions

- None. The implementation preserves retained-history totals rather than introducing permanent lifetime totals.
