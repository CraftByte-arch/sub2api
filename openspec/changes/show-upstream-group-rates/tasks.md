## 1. Upstream group snapshot model

- [x] 1.1 Add an identity-scoped remote-group snapshot model and state migration/deep-copy support.
- [x] 1.2 Extend Sub2API and NewAPI synchronization results to retain every available group, its reported platform, and its applicable multiplier without adding requests.
- [x] 1.3 Preserve the last good group snapshot as stale when a subsequent refresh cannot fetch group data.

## 2. Management API projection

- [x] 2.1 Return sorted group snapshots from the upstream manager identity view.
- [x] 2.2 Calculate a group final multiplier only when both the recharge rate and group multiplier are known.
- [x] 2.3 Add focused model, adapter, manager, and persistence tests for the new projection and migration behavior.

## 3. Upstream-management interface

- [x] 3.1 Add a compact, responsive available-group table beneath each expanded upstream identity.
- [x] 3.2 Render accessible platform labels, rate states, final multipliers, freshness indicators, resource summary count, and search matching.
- [x] 3.3 Add front-end regression checks and validate script syntax, formatting, and test suite results.
