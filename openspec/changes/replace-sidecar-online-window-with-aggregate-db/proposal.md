## Why

The sidecar online-user view currently derives presence from Sub2API request-detail APIs. A continuously refreshed sliding window would repeatedly transfer raw rows from the remote PostgreSQL database even when the administrator page is not open, while Sub2API already maintains compact per-minute user/group aggregates and per-day user usage aggregates.

## What Changes

- Replace raw request-detail scanning and the sidecar-owned user-level sliding window with on-demand reads from existing PostgreSQL aggregate tables.
- Add an optional, least-privilege PostgreSQL connection owned by the sidecar; deployments without this configuration keep all unrelated scheduler functions unchanged and report online data as unavailable.
- Cache aggregate summaries for 60 seconds, coalesce concurrent cache misses, and fetch detailed online users only when the administrator opens the dialog.
- Anchor the ten-minute presence window to the durable aggregation watermark and expose aggregation lag, stale, partial, and unavailable states.
- Use the existing daily per-user route aggregate for today requests, tokens, and actual cost; do not scan raw usage logs and do not automatically fall back to raw Sub2API request APIs.
- Remove the pending background sliding-window refresh implementation and its process-lifetime goroutine.

## Capabilities

### New Capabilities

- `sidecar-aggregate-online-users`: Administrator online-user summaries and details read on demand from least-privilege PostgreSQL aggregate data with bounded caching and explicit freshness state.

### Modified Capabilities

None.

## Impact

- Affects only `account-auto-scheduler/**` plus deployment configuration and database grants for a dedicated read-only role.
- Adds a PostgreSQL client dependency to the sidecar and optional database environment variables.
- Reads `channel_monitor_v2_user_metrics_1m`, `channel_monitor_v2_watermarks`, `user_dashboard_route_daily`, `user_dashboard_route_daily_state`, and limited user identity columns.
- Does not modify Sub2API source, billing, scheduling, Redis, aggregate schemas, or request processing.
