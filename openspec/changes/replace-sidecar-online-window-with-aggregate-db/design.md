## Context

HC2 runs the sidecar and Sub2API on the same host while PostgreSQL is hosted on another machine whose transferred bytes are billable. The deployed online-user feature currently queries Sub2API request-detail endpoints and enriches the result through additional administrator APIs. A proposed in-memory sliding window reduced repeated full scans but would still run continuously and transfer raw rows even when no administrator page was open.

Sub2API already maintains `channel_monitor_v2_user_metrics_1m`, a per-minute user/group aggregate covering successful usage and recorded failures, plus a durable `channel_monitor_v2_watermarks` row. It also maintains `user_dashboard_route_daily` and its readiness state for per-user daily requests, tokens, and actual cost. The sidecar can read these tables without changing Sub2API source.

## Goals / Non-Goals

**Goals:**

- Remove raw request-detail reads and all user-level sliding-window state from the sidecar.
- Make online reads demand-driven so a closed administrator page causes no additional sidecar database queries.
- Preserve global and per-group distinct-user semantics for the latest ten aggregate minutes, including recorded failures.
- Return administrator user identities and today usage only from compact aggregate reads when the detail dialog is opened.
- Keep every unrelated scheduling, upstream, notification, and administrator feature operational when database access is absent or broken.
- Keep the PostgreSQL credential least-privilege, read-only, bounded to two connections, and separate from Sub2API's writer credential.

**Non-Goals:**

- Exact-to-the-second presence or ten-second freshness; minute-bucket precision and aggregation lag are accepted and exposed.
- Adding or changing Sub2API tables, migrations, source code, billing, scheduling, Redis, or request handling.
- Falling back to raw usage or Ops detail APIs when aggregate data is unavailable.
- Running a permanent background presence poll in the sidecar.

## Decisions

### Use a separate optional PostgreSQL-backed online-user service

The sidecar will add a small online-user service that owns a `database/sql` pool using the pgx PostgreSQL driver. It is injected into the web server separately from the existing Sub2API HTTP client. Database configuration is optional: an empty DSN installs an unavailable reader instead of failing process startup.

This keeps database concerns out of the existing scheduling HTTP client and makes failure isolation explicit. Reusing the Sub2API database writer credentials was rejected because the online feature needs no write authority.

The pool will use at most two open connections, one idle connection, bounded connection lifetime, and per-query context timeouts. The deployment role will default to read-only and receive only the table/column grants required by the two queries.

### Query a watermark-anchored aggregate window

Summary reads will use one PostgreSQL statement with a CTE that reads `channel_monitor_v2_watermarks.data_through` and aggregates distinct users from `channel_monitor_v2_user_metrics_1m` in `[data_through-10m, data_through)`. Presence rows require either a positive success count or positive error count. The statement returns one compact row per active group plus the global distinct count.

Anchoring to `data_through` avoids undercounting the newest minute while aggregation is still catching up. The response explicitly reports the watermark and lag relative to current time. A transactionally updated aggregate table and watermark let a single statement observe a coherent PostgreSQL snapshot.

Using wall-clock `now-10m` was rejected because a normal one-minute aggregation delay would incorrectly omit recent traffic. Reading raw logs to fill the trailing gap was rejected because it would restore the billable traffic the change is intended to remove.

### Separate summary and detail reads

The fast summary endpoint returns readiness, global count, per-group counts, watermark, lag, and freshness flags. It does not join users or daily usage. The browser uses this endpoint for page load and visible-page polling.

The detail endpoint is called only when the administrator opens or refreshes the online-user dialog. It derives one row per active user/group pair from the minute aggregate, joins only `users.id`, `users.email`, and `users.username`, and left-joins current-day totals from `user_dashboard_route_daily`. If the daily aggregate readiness row is false, identity and presence remain available while today usage fields remain null and the response is marked partial.

### Replace sliding state with bounded snapshot caches

The service will maintain separate 60-second summary and detail caches. A cache miss performs one query; concurrent misses share the same in-flight result. Cache contents are immutable deep copies to callers.

This is a response cache, not a sliding window: it stores no request events and performs no periodic work. When no administrator requests online data, the sidecar issues no online database reads.

On a transient query failure, a recent last-good snapshot can be returned with `stale=true` and a generic notice. Aggregate lag beyond three minutes is stale; beyond ten minutes the response is not ready and must not present old counts as current. Raw HTTP fallback is never attempted.

### Keep browser authentication and failure isolation unchanged

Both summary and detail endpoints remain behind the existing administrator-session middleware. Database credentials never reach browser JavaScript. Missing or failed database configuration affects only the online indicators and dialog; overview, account scheduling, upstream synchronization, notifications, and all other handlers remain available.

### Use direct grants instead of a sidecar-owned schema migration

Deployment creates a dedicated PostgreSQL login with `default_transaction_read_only=on`, a connection limit of two, schema usage, table-level SELECT on the aggregate tables and watermarks, and column-level SELECT on `users(id,email,username)`. The sidecar does not run DDL or migrations.

Views were considered as a stable contract but rejected for this iteration because they add unmanaged schema objects outside Sub2API migrations. The queries will fail closed with an unavailable response if a future Sub2API migration removes the expected contract.

## Risks / Trade-offs

- [Minute aggregates lose exact request seconds] → Display minute precision and the aggregation watermark; do not claim ten-second freshness.
- [Channel Monitor V2 aggregation stops or its watermark stalls] → Mark lag over three minutes stale and lag over ten minutes unavailable without raw fallback.
- [Direct schema coupling breaks after an upstream migration] → Keep all SQL in one sidecar package, fail only the online feature, and cover expected schema behavior with query tests.
- [A sidecar compromise exposes database credentials] → Use a dedicated read-only role, column-limited user access, connection limit two, TLS whenever the database supports it, and no access to account/API-key credential tables. HC2's current PostgreSQL endpoint does not offer TLS, so its accepted deployment uses only the trusted private network with `sslmode=disable`.
- [Multiple tabs trigger duplicate queries] → Use a 60-second cache and coalesce concurrent cache misses.
- [Daily aggregate backfill is not ready] → Keep presence available and return nullable today usage with a partial notice.
- [Database is unreachable during startup] → Do not fail sidecar startup; retry naturally on later cache misses and keep unrelated features available.

## Migration Plan

1. Build and verify the sidecar with optional PostgreSQL configuration and no background online-user goroutine.
2. Create the dedicated HC2 sidecar reader role and exact grants using the existing database administrator connection; do not alter aggregate schemas.
3. Add the sidecar DSN to the HC2 environment, back up the prior environment and state file, and deploy only `account-auto-scheduler` from a committed immutable image.
4. Verify summary/detail responses, watermark lag, administrator authentication, zero raw online API calls, and unchanged container identities for all other services.
5. Roll back by restoring the prior sidecar image and environment. The read-only role can remain harmlessly or be removed after rollback verification.

## Open Questions

None.
