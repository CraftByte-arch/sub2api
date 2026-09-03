## Why

The sidecar currently fills the account cache-hit indicator by calling Sub2API's full per-account statistics endpoint once for every account. On a cold cache this creates an N+1 request pattern, delays the overview, and produces canceled database work in the main service even though the sidecar already has a read-only aggregate database connection.

## What Changes

- Replace per-account HTTP cache-stat enrichment on the overview path with one bounded, read-only PostgreSQL aggregation for all displayed account IDs.
- Preserve the existing cache-hit calculation, payload shape, and unavailable-state behavior.
- Cache and single-flight the sidecar's bulk cache-stat result, retain the latest successful snapshot during a transient database failure, and never fall back to the N+1 main-service route after a configured database query fails.
- Keep Sub2API production code, routes, schemas, and deployment untouched.

## Capabilities

### New Capabilities

- `sidecar-account-cache-hit-rate-performance`: Efficient, best-effort bulk collection of today's account cache-hit statistics inside the sidecar.

### Modified Capabilities

None.

## Impact

- Affects only `account-auto-scheduler` database-read service, overview assembly, core client, focused tests, and HC2 sidecar deployment.
- Reuses the existing `AUTO_SCHEDULER_ONLINE_DATABASE_URL` read-only connection and `usage_logs` index; no Sub2API API or database migration is introduced.
