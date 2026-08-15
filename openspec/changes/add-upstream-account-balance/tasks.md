## 1. Balance Model and Adapters

- [x] 1.1 Add optional identity balance snapshot models, public views, state deep cloning, and backward-compatible model/store tests.
- [x] 1.2 Extend Sub2API connect and sync results with authenticated USD balance and adapter fixtures.
- [x] 1.3 Extend NewAPI connect and sync results with user quota, same-origin `quota_per_unit` normalization, raw-quota fallback, and adapter fixtures.

## 2. Manager and Administrator View

- [x] 2.1 Persist fresh balance results on connect/sync, retain and mark last-good values stale on failure, and add manager regression tests.
- [x] 2.2 Expose redacted per-identity balance metadata and a non-summing upstream balance summary through existing administrator APIs with handler tests.

## 3. Upstreams Interface

- [x] 3.1 Render compact upstream and identity balance states, units, timestamps, stale labels, and unavailable states in the existing Upstreams tab.
- [x] 3.2 Add JavaScript behavior and CSS regression coverage for single/multiple identities, raw quota, errors, and 375px responsive layout.

## 4. Documentation, Verification, and Deployment

- [x] 4.1 Document balance sources, NewAPI conversion, freshness, multiple-identity semantics, limitations, and sidecar-only rollback.
- [x] 4.2 Run formatting, all Go tests, race tests, vet, JavaScript syntax checks, strict OpenSpec validation, and browser desktop/mobile verification.
- [x] 4.3 Back up hc2 state, deploy only `account-auto-scheduler`, refresh representative upstream identities, verify displayed balances, and confirm other containers were not restarted.

## 5. Bound Sub2API Account Quota Projection

- [x] 5.1 Add sidecar quota-projection models and a minimal client writer using the existing administrator bulk Extra merge endpoint without sending `rate_multiplier`.
- [x] 5.2 Derive per-bound-account remaining quota from fresh identity USD balance divided by the remote-key multiplier, including full-balance fan-out, zero-multiplier unlimited behavior, and immediate zero-balance exhaustion.
- [x] 5.3 Reconcile quota after connect/sync/start/periodic/binding changes, preserve unsupported or stale observations, and clear only sidecar-managed limits after explicit unbinding or account movement.
- [x] 5.4 Add client/manager regression coverage and run formatting, all Go tests, race tests, vet, JavaScript syntax checks, strict OpenSpec validation, and final diff review.
