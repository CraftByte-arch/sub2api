## 1. Sub2API optional scheduling field

- [x] 1.1 Define the `final_cost_multiplier` extra key and shared finite-number helpers without changing account billing fields.
- [x] 1.2 Make OpenAI cost ordering prefer a valid final cost multiplier and fall back to the existing probe signal when absent or invalid.
- [x] 1.3 Make profit admission and profit preview use the same final-cost-first, account-rate-fallback rule.
- [x] 1.4 Add unit tests proving override, zero-value support, invalid-value fallback, and unchanged legacy behavior.
- [x] 1.5 Move final-cost parsing into a dedicated scheduling-signal file and ensure it never copies or mutates `Account.RateMultiplier`.

## 2. Sidecar synchronization

- [x] 2.1 Parse and expose the optional final cost multiplier in the sidecar account model while keeping the probe multiplier separate.
- [x] 2.2 Use an existing admin-authenticated Extra-merge path that updates only `extra.final_cost_multiplier` and never writes `rate_multiplier`.
- [x] 2.3 Reconcile final multipliers after binding changes, recharge-rate changes, upstream key sync, and periodic synchronization; clear values when calculation is unavailable.
- [x] 2.4 Add sidecar tests for calculation, write/clear behavior, and idempotent fallback cases.
- [x] 2.5 Reuse the existing admin bulk-update Extra merge endpoint and remove the dedicated final-cost endpoint.

## 3. Verification and documentation

- [x] 3.1 Run focused Go tests for backend and sidecar packages and fix regressions.
- [x] 3.2 Run formatting and relevant static checks.
- [x] 3.3 Update the OpenSpec task checkboxes and summarize deployment implications (no DB migration; old accounts keep original behavior).
- [x] 3.4 Verify standalone Sub2API behavior and billing are unchanged when the sidecar/field is absent.
