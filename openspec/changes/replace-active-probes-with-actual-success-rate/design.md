## Context

The sidecar currently owns a one-second due loop that calls either Sub2API's account-test endpoint or a directly authorized upstream route. Those synthetic calls feed a failure/recovery state machine that can suspend and restore API-key scheduling. The sidecar also already has an optional, least-privilege PostgreSQL pool for compact online-user aggregates.

Sub2API independently records every selected-account gateway attempt into `account_performance_minute`, rolls closed hours into `account_performance_hourly`, flushes attempts asynchronously every five seconds, and retains minute/hourly data for seven/ninety days. The aggregate outcome distinguishes success, client cancellation, rate limit, authentication, upstream 4xx/5xx, transport, protocol, timeout, and other failure. Active account tests and direct sidecar probes do not traverse the gateway-attempt recorder.

Production sampling on HC2 showed that repeatedly aggregating the entire latest hour from the minute table costs about 2.3 seconds and roughly 1,900 PostgreSQL buffer pages, while reading a three-minute tail costs about 38 ms and a 24-hour closed-hour aggregate costs about 26 ms. Database and sidecar run on different machines, so both query work and result traffic must remain bounded.

## Goals / Non-Goals

**Goals:**

- Guarantee that the sidecar emits no scheduled or administrator-triggered account probe traffic.
- Preserve manual scheduling, group bindings, multiplier protection, balance synchronization/alerts, upstream login/sync, and notifications.
- Show a group-specific, attempt-level actual success rate for the latest rolling hour, with a 24-hour reference.
- Exclude client cancellations from the denominator and expose exact numerator, denominator, sample sufficiency, freshness, and last observed bucket.
- Read only existing compact account-performance aggregates through the existing sidecar read-only pool.
- Make reads demand-driven, coalesce concurrent requests, retain last-good data on transient failure, and avoid a full-hour scan on every page refresh.
- Keep stored probe settings and history compatible with rollback without executing them.

**Non-Goals:**

- Automatically suspending or restoring accounts based on actual success rate.
- Changing Sub2API scheduling, billing, request handling, schemas, migrations, or account-performance collection.
- Reading raw `usage_logs`, calculating unique end-user request success, or claiming exact-to-the-second freshness.
- Deleting stored historical probe results or encrypted direct-probe material in this change.

## Decisions

### Enter passive mode without removing manual scheduling infrastructure

The process will construct the existing engine because its serialized account state and manual scheduling method remain in use, but it will not call `Engine.Start`. A new passive-mode transition will disable every stored probe policy, clear future due times and transient counters, and restore only suspensions whose `ManagedSuspended` flag proves ownership by the former probe state machine. Administrator-owned stopped accounts remain stopped. Failures to restore one account are logged and do not prevent the sidecar from starting.

Probe mutation, run-now, and direct-probe endpoints will return an explicit HTTP 410 response. Read-only compatibility data may remain available internally, while the browser removes all probe creation/edit/run/authorization controls and historical detection bars. This defense-in-depth prevents an old browser bundle or direct API caller from reactivating probes.

Deleting the engine and serialized fields was rejected because manual scheduling and balance-alert state currently share that compatibility surface, and destructive state migration would make rollback riskier.

### Reuse the existing aggregate database service and pool

Actual-success SQL and cache code will live in a separate file under the existing aggregate-backed `onlineusers` package so it can reuse the already bounded PostgreSQL pool and dedicated read-only credential. It will have its own mutex and in-flight request coalescing so a slow success-rate read cannot block online-user cache access.

Opening a second pool was rejected because the deployed read-only role is deliberately limited to two connections and the existing online service can already consume both during concurrent summary/detail reads. Refactoring the whole package into a generic database layer was rejected as unnecessary sidecar churn for one additional aggregate feature.

### Maintain a demand-driven rolling minute-bucket cache

The cache key is `(bucket_start, group_id, account_id)`, and each immutable bucket stores additive success, attempt, cancellation, and failover counts. On a cold cache or a gap of at least one hour, one bounded query bootstraps the latest hour grouped by minute/group/account. Otherwise a refresh starts two minutes before the previous data boundary and replaces every returned bucket in that overlap, absorbing late asynchronous flushes without double counting. Buckets older than the rolling window are pruned.

The cache TTL is two minutes. Concurrent overview requests share one query. No background goroutine polls PostgreSQL, so a closed administrator page causes no success-rate reads. Query context timeout is five seconds; a recent last-good snapshot is returned as stale after failure, and data becomes unavailable rather than silently current after ten minutes.

Querying the complete hour every two minutes was rejected based on HC2 measurements. Persisting the minute cache was rejected because a one-time bootstrap after process restart is acceptable and avoids per-minute state-file writes.

### Use closed hourly aggregates for the 24-hour reference

One cached query groups the latest 24 closed UTC hours by group/account. This intentionally excludes the current incomplete hour and returns its `data_through` boundary so the UI can label the reference precisely. It refreshes no more than once every five minutes and is executed in the same single-flight refresh as required.

Combining raw current-hour logs was rejected. Reusing the existing Sub2API account-performance HTTP endpoint was also rejected because that endpoint additionally scans request-level usage logs for exact latency percentiles, which this display does not need.

### Present rates with samples and explicit data state

Each group-account row receives the matching group-specific metric. The primary line is the rolling one-hour percentage; the secondary line is `success / effective attempts`. Fewer than 20 effective attempts is marked `样本较少`, zero attempts is `暂无真实调用`, and stale/unavailable data is stated in text. A tooltip contains the 24-hour rate, its completed-hour boundary, cancellations, failovers, and freshness notice. Color supplements these labels but is never the only status signal. Numeric fields use tabular figures and reserve stable layout space.

The top summary replaces the obsolete automatic-probe suspension count with the number of accounts having real traffic samples in the latest hour.

## Risks / Trade-offs

- [The account-performance recorder is best effort and can drop samples] → Read the existing lightweight collection-health endpoint and mark the display partial when collection reports degraded health.
- [The first request after a long idle period requires a full-hour bootstrap] → Run it asynchronously behind a bounded timeout, keep the page responsive, and temporarily show the inexpensive 24-hour reference or a loading state.
- [Minute rows are updated repeatedly during the open minute] → Replace overlapping bucket snapshots rather than adding deltas, and always re-read a two-minute overlap.
- [The read-only role lacks new table grants] → Treat the metric as independently unavailable; deployment adds only `SELECT` on the two aggregate tables.
- [An account has traffic in multiple groups] → Keep group/account keys and never project a site-wide account rate into a group row.
- [Low-traffic accounts produce volatile percentages] → Show sample counts and a low-sample label rather than hiding or over-trusting the value.
- [Dormant probe data remains stored] → It is never exposed as active state or executed, and can be removed in a separately approved destructive cleanup later.

## Migration Plan

1. Deploy the sidecar-only image with passive-mode startup and aggregate reads disabled when grants are absent.
2. Before switching the sidecar image, grant the existing `sidecar_reader` role `SELECT` on `account_performance_minute` and `account_performance_hourly`.
3. Restart only `account-auto-scheduler`; verify passive transition logs, no account-test/direct-probe requests, actual-success freshness, and unchanged Sub2API container identities/restart counts.
4. Confirm accounts formerly marked `ManagedSuspended` are restored only when that ownership flag was present, while administrator-stopped accounts remain stopped.
5. Roll back by restoring the previous sidecar image. Stored probe policy/history remains compatible; re-enabling active probes after rollback requires an explicit administrator action because policies were persisted disabled.

## Open Questions

None.
