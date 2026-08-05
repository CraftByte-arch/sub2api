## Why

The administrator group-management page currently aggregates every retained `usage_logs` row to show group total and today usage. On the migrated single-core PostgreSQL database this scans millions of rows, causing slow page loads and CPU saturation.

## What Changes

- Add PostgreSQL-persisted, per-group UTC hourly usage-cost buckets.
- Record each newly persisted usage log into its group hour bucket without Redis and without changing the existing group usage-summary API contract.
- Read group historical and today usage from the hourly buckets after historical data has been backfilled and verified.
- Run a resumable, rate-limited historical backfill automatically after deployment; retain the legacy query until the aggregate is ready.
- Exclude soft-deleted groups from the summary, matching the administrator group list.

## Capabilities

### New Capabilities

- `group-usage-hourly-aggregation`: Persisted group usage-hour buckets, resumable backfill, and compatible group-usage summary reads.

### Modified Capabilities

- None.

## Impact

- Affects the usage-log repository insert paths, group usage-summary repository query, and existing usage-log retention cleanup.
- Adds PostgreSQL tables and indexes through a forward-only migration.
- Does not change the HTTP endpoint, frontend contract, Redis usage, or user-facing group-management workflow.
