## Why

The sidecar account list currently presents administrator quota as low-emphasis used/limit text, so an exhausted account is easy to miss. Direct upstream probes can also return a 403 response whose body explicitly says the balance or quota is insufficient, but that failure is currently shown as a generic probe error instead of an actionable balance problem.

## What Changes

- Add a sidecar-only administrator-balance projection to the overview response without exposing Sub2API account internals that are not needed by the browser.
- Emphasize remaining administrator balance in each API Key account row and display an explicit `余额不足` state when a configured quota is exhausted.
- Classify probe failures as `balance_insufficient` only when an HTTP 403 response also contains recognized balance/quota-insufficient semantics.
- Persist and expose the latest structured failure reason, clear it after a non-failing result, and show `余额不足导致检测失败` in the account status while preserving the original upstream error in the detailed reason and history.
- Keep balance-insufficient checks in the existing failure-counting and automatic suspend/recovery state machine.

## Capabilities

### New Capabilities

- `account-auto-scheduler-balance-health`: Covers administrator-balance presentation and structured insufficient-balance probe failure classification in the sidecar scheduler.

### Modified Capabilities

None.

## Impact

- Affects only `account-auto-scheduler` model, probe error handling, engine state, overview projection, static account-list UI, and tests.
- Adds backward-compatible JSON fields to sidecar state and overview payloads; existing persisted state remains readable.
- Does not modify Sub2API backend/frontend code, account billing, quota mutation, scheduling thresholds, or upstream credentials.
