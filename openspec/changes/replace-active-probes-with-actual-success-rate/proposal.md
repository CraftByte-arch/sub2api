## Why

The sidecar's periodic account probes consume upstream quota, create highly regular outbound traffic, and report synthetic health that can differ from real user traffic. Sub2API already records bounded per-account performance aggregates for actual gateway attempts, so the sidecar can present a more representative signal without continuing active probes or scanning raw request logs.

## What Changes

- **BREAKING** Stop the sidecar from scheduling, authorizing, or manually triggering active account probes; existing stored probe policy and history remain readable for rollback compatibility but are no longer executed.
- Restore any account suspension still explicitly owned by the former probe state machine, then clear that ownership so passive health cannot later change scheduling state.
- Add group-account actual success-rate data sourced from `account_performance_minute` and `account_performance_hourly`, excluding client cancellations from the denominator.
- Maintain a demand-driven, incremental one-hour minute-bucket cache in the sidecar, plus a cached 24-hour hourly reference, without reading `usage_logs`.
- Show the one-hour success rate, sample counts, low-sample/empty/stale states, and a 24-hour reference in each account row while preserving manual scheduling, balance, multiplier protection, upstream synchronization, and notifications.
- Reuse the sidecar's existing optional read-only PostgreSQL connection and fail only the passive-success display when aggregate access is absent or unavailable.

## Capabilities

### New Capabilities

- `sidecar-passive-account-health`: Defines removal of active probing and presentation of actual group-account success rates from compact existing aggregates.

### Modified Capabilities

None.

## Impact

- Affected code is confined to `account-auto-scheduler`: process wiring, the aggregate reader, overview API models/handlers, static administrator UI, and tests.
- No Sub2API application source, scheduling algorithm, billing path, database schema, or raw usage query is changed.
- Deployment must grant the existing dedicated sidecar read-only role `SELECT` on `account_performance_minute` and `account_performance_hourly`; no write or credential-table access is required.
- Stored active-probe configuration remains backward-compatible but becomes dormant.
