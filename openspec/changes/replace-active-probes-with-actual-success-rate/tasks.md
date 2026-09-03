## 1. Passive runtime transition

- [x] 1.1 Add an idempotent engine passive-mode transition that disables stored probe policies, clears due/running state, and restores only probe-owned suspensions.
- [x] 1.2 Stop starting the active-probe loop and reject every probe mutation, run-now, authorization, and revocation route without outbound requests.
- [x] 1.3 Add engine and web tests covering administrator stops, probe-owned restoration failures, dormant compatibility data, and removed endpoints.

## 2. Aggregate actual-success service

- [x] 2.1 Add group-account success-rate response models with one-hour, completed-24-hour, sample, cancellation, failover, freshness, and collection-health fields.
- [x] 2.2 Implement the cold one-hour bootstrap, overlapping incremental minute refresh, pruning, two-minute cache, single-flight coalescing, and stale last-good behavior on the existing read-only database pool.
- [x] 2.3 Implement the five-minute cached 24-hour completed-hour query and merge it with the rolling one-hour snapshot without reading raw usage logs.
- [x] 2.4 Add aggregate service tests for formulas, group isolation, incremental bucket replacement, gaps, cache concurrency, stale/unavailable handling, and bounded SQL ranges.

## 3. Overview and administrator UI

- [x] 3.1 Inject actual-success snapshots into the overview response without making other overview data fail when the aggregate source is unavailable.
- [x] 3.2 Replace probe controls, automatic-stop summary/filter state, policy copy, and history bars with accessible actual-success metrics and a 24-hour detail tooltip.
- [x] 3.3 Preserve responsive account rows, manual scheduling controls, account balance alerts, binding actions, upstream features, and clear low-sample/empty/stale text states.
- [x] 3.4 Add handler, JavaScript contract, and static UI tests for the new display and absence of active-probe interactions.

## 4. Verification and release readiness

- [x] 4.1 Update sidecar configuration/deployment documentation with the two read-only aggregate grants and passive-mode behavior.
- [x] 4.2 Run formatting, Go unit/race tests, vet, build, JavaScript syntax checks, strict OpenSpec validation, and diff checks.
- [x] 4.3 Audit the final diff to prove no Sub2API production source or migration changed and document the HC2 sidecar-only deployment/rollback procedure.
