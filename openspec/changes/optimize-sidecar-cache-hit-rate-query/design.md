## Context

The overview already fetches basic daily usage in one request through Sub2API's batch endpoint, but `core.Client.GetTodayStatsBatch` then opens up to eight concurrent `GET /admin/accounts/:id/stats?days=1` requests. Each of those full account-stat calls computes history, model distribution, and endpoint distributions that the cache-hit label does not need. HC2 already provides the sidecar with `AUTO_SCHEDULER_ONLINE_DATABASE_URL` for read-only aggregate features, and PostgreSQL is configured in the same Asia/Shanghai day boundary used by Sub2API's today-stat query.

## Goals / Non-Goals

**Goals:**

- Replace the overview's cache-hit N+1 reads with one bounded sidecar database query per cache miss.
- Preserve the existing `cache_read / (input + cache_creation + cache_read)` calculation and nested `today_usage.cache` API contract.
- Keep base overview data available even if cache statistics cannot be loaded.
- Reuse the sidecar's existing read-only pool, timeout, short TTL cache, stale-data behavior, and single-flight pattern.
- Make no code, schema, API, container, or configuration change to Sub2API.

**Non-Goals:**

- Changing the cache-hit formula, billing, the main service's usage endpoints, or its existing aggregation tables.
- Adding a new database connection or a background polling job.
- Displaying cache statistics for date ranges other than today's Sub2API-defined day.

## Decisions

### Add bulk cache-stat retrieval to the existing sidecar database service

`onlineusers.Service` already owns the optional read-only PostgreSQL pool used for passive account-success and online-user data. It will gain a bulk `GetTodayAccountCacheStats` read that accepts the overview account IDs and returns a map keyed by account ID.

The query will group `usage_logs` once by `account_id`, summing input, cache-creation, and cache-read tokens since `CURRENT_DATE`. `CURRENT_DATE` is evaluated by PostgreSQL using its configured timezone, which is Asia/Shanghai on HC2 and matches Sub2API's `timezone.Today()` boundary. Every requested positive ID receives a zero-valued projection when it has no rows, matching a successful old per-account request with no model usage.

Alternative considered: call `/admin/usage/stats?account_id=...` for each account. That endpoint is lighter than `/accounts/:id/stats`, but it remains N+1 and would still add one main-service request per account. Direct database aggregation is one round trip, requires no main-service change, and uses the connection the sidecar already has.

### Keep the core client responsible only for base today usage

`core.Client.GetTodayStatsBatch` will stop enriching `WindowStats.Cache`; it will only invoke the existing batch today-stat endpoint. The web overview will attach bulk cache projections after loading the base stats.

Alternative considered: put the database URL in `core.Client`. That would mix two transport layers and duplicate pool lifecycle/configuration. Keeping the query in `onlineusers.Service` preserves the current separation between the Sub2API HTTP client and sidecar read-only database facilities.

### Use short keyed cache, single-flight, and stale-success fallback

The cache-stat service will cache a result for the normalized account-ID set for one minute, share concurrent requests for the same set, and retain the last successful result for that set. A configured database failure returns the most recent successful result marked stale when available; otherwise the overview simply leaves `today_usage.cache` absent. It must not invoke the old N+1 fallback after a configured database failure.

If the optional database connection is absent, the server will use a compatibility fallback to the existing bounded per-account HTTP enrichment so standalone sidecar deployments do not lose the cache indicator. HC2 has the configured database path and will never take that fallback under normal operation.

### Preserve response and UI contracts

No UI changes are necessary: it already renders a cache object as a compact rate and renders a missing cache object as `缓存命中 —`. The bulk map is attached to the existing `model.WindowStats.Cache` field, so existing static UI and API consumers remain compatible.

## Risks / Trade-offs

- [The direct database connection is unavailable] → Preserve base overview data; return the latest short-lived good cache projection if one exists; show the existing unavailable indicator otherwise.
- [The database timezone diverges from Sub2API's configured timezone in another environment] → Depend on the already shared HC2 configuration; retain the HTTP compatibility path for deployments without the configured direct database.
- [A large account fleet changes while an overview is cached] → Key caches by normalized requested IDs and re-query when the set changes.
- [A query scans more current-day rows than the old account-specific index scans] → It is one aggregate scan rather than N full stats requests, has a five-second bounded context, and HC2's current plan uses an existing usage-log time index.

## Migration Plan

1. Add focused unit/contract tests for the single bulk query, cache behavior, stale/error behavior, and no-N+1 overview flow.
2. Build and test only `account-auto-scheduler` from the sidecar worktree.
3. Create a new sidecar-only image, update only the `account-auto-scheduler` container on HC2, and wait for health.
4. Verify the live overview returns cache data and confirm the Sub2API container image and uptime are unchanged.
5. Roll back by restarting `account-auto-scheduler` with its prior image tag if the sidecar health or overview verification fails.

## Open Questions

None. HC2 already has the read-only database URL configured and PostgreSQL confirms the matching Asia/Shanghai timezone.
