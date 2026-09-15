## Context

The sidecar enriches the account overview's batch usage totals with a per-account cache-hit rate. Its current source, `GET /admin/accounts/:id/stats?days=1`, can enter an inefficient historical query branch on HC2. A single overview refresh can therefore submit several expensive reads, and unsuccessful reads are retried on the next refresh.

The running Sub2API usage-record page already reads `GET /admin/usage/stats` with an account filter. That request has a short server-side snapshot cache and uses the indexed usage-log query path. The sidecar must remain an HTTP-only consumer of Sub2API and must not require a database/schema change.

## Goals / Non-Goals

**Goals:**

- Obtain cache-hit data with the existing usage-statistics endpoint, scoped to one account and the current day.
- Use the same formula as the Sub2API usage-record interface: `cache_read / (input + cache_read)`.
- Keep successful data cached in the sidecar and coalesce concurrent reads for the same account.
- Back off failed reads so a temporary main-service or network failure cannot trigger a new request every overview poll.
- Preserve the current optional `WindowStats.Cache` response contract and rendering.

**Non-Goals:**

- Changing Sub2API HTTP handlers, repositories, caches, SQL, schema, roles, or deployment.
- Reading `usage_logs` directly from the sidecar.
- Changing the presentation layout or adding a new UI control.
- Claiming second-level freshness for a best-effort daily metric.

## Decisions

### Read the usage-statistics endpoint instead of account statistics

For each active account without a fresh sidecar snapshot, the sidecar will request the existing administrator usage-statistics endpoint with the account filter and the current-day range. It will decode the aggregate input and cache-read token totals directly rather than aggregating account-model rows.

This avoids the known heavy endpoint and aligns the semantic source with the main service page. Direct database reads are rejected because the sidecar's role intentionally lacks `usage_logs` access and changing database privileges would expand the blast radius.

### Preserve a five-minute success cache, add a short failure backoff, and singleflight by account

Successful per-account cache projections will retain the existing five-minute in-process TTL. Failed reads will record a short expiry, so the next overview refresh returns an unavailable cache metric without issuing another upstream request. Concurrent callers for the same account will share one in-flight read.

The five-minute cache keeps the normal overview poll cheap. A short failed-read backoff limits pressure during outages while allowing the metric to recover automatically. A global cache refresh job is rejected because it would query accounts that no administrator is viewing.

### Match the main service formula

The returned projection will use `cache_read_tokens / (input_tokens + cache_read_tokens)`. Cache-creation tokens are not included because the usage-record page's cache-hit card does not include them in its denominator.

## Risks / Trade-offs

- [The existing usage endpoint changes its response shape in a future Sub2API version] → Treat decode errors as best-effort failures, preserve base overview data, and cover the expected envelope/fields with focused client tests.
- [A prolonged endpoint outage leaves cache rate unavailable] → Keep the display optional and apply a bounded retry backoff rather than competing with production traffic.
- [Five-minute snapshots are not live] → This retains the prior sidecar freshness contract and avoids turning an informational metric into a polling load source.
- [An administrator sees a value that the main service refreshed more recently] → Both values use the same formula and source; the sidecar's documented in-process cache intentionally trades freshness for stability.

## Migration Plan

1. Build and run focused sidecar tests locally.
2. Build a new sidecar-only image and transfer it to HC2.
3. Record the current `sub2api-canary` image and start time, then recreate only `account-auto-scheduler` with `docker compose ... up -d --no-deps --force-recreate`.
4. Verify sidecar health and that the sidecar no longer emits requests to `/admin/accounts/:id/stats`.
5. Confirm `sub2api-canary` image and start time are unchanged. Rollback consists solely of restoring the prior sidecar image and recreating the sidecar; no main-service rollback is involved.

## Open Questions

- None. The existing HC2 production endpoint will be probed read-only before release to verify its accepted current-day query form and response fields.
