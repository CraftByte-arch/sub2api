## Context

The overview already obtains each account's current-day request count through the existing batch endpoint. A cache hit rate is deterministically zero when that count is zero. Accounts with requests need model-level cache token totals, which are available only through an existing per-account administrator endpoint without changing Sub2API.

HC2's sidecar database role correctly exposes only purpose-built aggregate tables. It has no access to `usage_logs`, and none of the readable aggregates is keyed by account with cache tokens. Expanding that privilege or adding a database view would change shared database security/schema and is intentionally out of scope.

## Decision

The core client enriches cache statistics after the batch today-usage result:

1. Normalize the requested account IDs.
2. Materialize a zero-valued cache projection for every account whose current-day request count is zero or absent.
3. Call the existing `/admin/accounts/:id/stats?days=1` endpoint only for the remaining active accounts, with four workers and a five-second overall budget.
4. Cache successful per-account projections in the sidecar for five minutes. A failed active-account read remains unavailable and never blocks base usage output.

This preserves the existing response shape and cache-hit calculation while avoiding unnecessary per-account requests for idle accounts. With the observed HC2 overview (41 accounts, about 7 accounts with recent traffic), a cold refresh avoids roughly 34 unnecessary calls; subsequent refreshes inside five minutes make none.

## Non-Goals

- Reading raw usage logs from the sidecar.
- Database grants, views, migrations, or background aggregation jobs.
- Any Sub2API code, route, schema, configuration, or container change.
