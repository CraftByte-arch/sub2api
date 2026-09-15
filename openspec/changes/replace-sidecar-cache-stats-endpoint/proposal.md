## Why

The sidecar's cache-hit-rate enrichment currently calls the per-account statistics endpoint. On HC2, its query can choose a historical full scan despite an empty dirty-day table, causing PostgreSQL CPU saturation when the sidecar retries several account reads during overview refreshes.

The Sub2API usage-record page already exposes the same cache-read signal through a bounded, indexed administrator statistics path. The sidecar should use that path and must avoid turning transient failures into a repeat-query storm.

## What Changes

- Replace sidecar cache-hit-rate enrichment reads from the per-account statistics endpoint with the existing administrator usage-statistics endpoint scoped to one account and the current day.
- Match the cache-hit-rate formula displayed by the Sub2API usage-record page: cache-read tokens divided by input plus cache-read tokens.
- Preserve the best-effort nature of the metric while caching successful snapshots and applying a bounded retry backoff after failed reads.
- Keep the existing sidecar response shape and account-list display unchanged.

## Capabilities

### New Capabilities

- `sidecar-account-cache-hit-rate-resilient-read`: Safely obtains and presents a current-day per-account cache-hit rate without invoking the heavy account-statistics query path.

### Modified Capabilities

- None.

## Impact

- Affects only `account-auto-scheduler/internal/core/client.go` and its focused tests.
- Uses the existing read-only Sub2API administrator HTTP endpoint; no Sub2API code, database schema, permissions, or deployment changes are required.
- The sidecar container will be rebuilt and recreated on HC2; the running `sub2api-canary` container and image remain untouched.
