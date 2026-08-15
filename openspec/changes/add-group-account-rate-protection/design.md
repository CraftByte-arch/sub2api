## Context

`account-auto-scheduler` currently persists upstream key bindings and projects the final recharge-rate × group-rate multiplier only from keys that are still physically bound in Sub2API. That loses the intended group membership as soon as a key is removed, so an automatic profit guard cannot both unbind a key and continue showing it as a protected member. The scheduler also stores check history and probe text but does not retain token usage or the estimated cost of its own checks.

The change must remain sidecar-first: reuse Sub2API's existing administrator account/group update endpoint and model-pricing endpoint, keep billing `rate_multiplier` untouched, and preserve old state files and accounts without protection settings.

## Goals / Non-Goals

**Goals:**

- Persist one optional protection record per `(group_id, account_id)` with the exact upstream identity/key source needed to restore the binding after an automatic guard trip.
- Treat `final_multiplier > protection_multiplier` as protected/unbound and `final_multiplier <= protection_multiplier` as eligible for automatic binding.
- Keep protected logical members visible in overview data even while Sub2API reports no group membership.
- Make manual release and manual removal operations race-safe and idempotent, with removal deleting the logical record before any physical unbind.
- Capture usage returned by legacy or direct streaming checks where available, resolve current model pricing through the existing administrator API, and accumulate probe-only cost at a 1.0 multiplier.
- Keep UI actions administrator-protected, keyboard reachable, confirmation guarded, loading-aware, and responsive at the existing 375px breakpoint.

**Non-Goals:**

- Changing Sub2API's billing multiplier, account status, schedulable flag, quota limits, or database schema.
- Rebinding accounts that have no protection record and were manually removed from a group.
- Guessing cost when a check returns no usage or the model-pricing endpoint has no matching model.
- Splitting or reserving upstream money; the protection record is an operational binding intent, not a billing ledger.

## Decisions

### 1. Persist a shadow binding record instead of inferring intent from physical membership

Add an optional state map keyed by `groupID/accountID`. Each record stores the protection threshold, normalized upstream ID, identity ID, remote key ID, creation/update timestamps, and the last guard decision. The source tuple lets the manager derive the final multiplier after physical unbinding. Existing v3 JSON remains loadable; the state version is advanced only after accepting all prior versions and defaulting the new map to empty.

### 2. Use a serialized reconciliation coordinator

The manager owns a mutex around protection mutations and reconciliation. Automatic transitions update Sub2API first for a guard trip/recovery, while explicit removal deletes the shadow record first and then unbinds. This ordering means a failed physical unbind can be retried, but a removed account can never be re-bound later because no logical record remains. Manual release binds first and deletes the protection record only after success; a failed bind leaves the record intact for retry.

### 3. Preserve protected members in all account projections

Overview and binding-dialog projections merge current Sub2API memberships with protection records. A protected record whose physical binding is absent is exposed as a logical member with status `rate_protected`, the saved final multiplier, and the configured threshold. The ordinary `unbound` status remains for accounts with no active physical or logical relationship. Bulk binding saves interpret a protected logical member as selected until the administrator explicitly deselects it, which removes the protection record and prevents future automatic rebinding.

### 4. Reconcile on every authoritative multiplier refresh and on binding mutations

After upstream synchronization, startup/periodic local reconciliation, protection edits, manual release, and binding-dialog saves, the manager lists current accounts, derives protection decisions from persisted key snapshots, and applies only necessary physical changes. Unknown, stale, ambiguous, or unavailable multipliers do not toggle a binding; the last safe physical state is retained and the UI shows the unresolved reason.

### 5. Keep probe-cost statistics sidecar-local

`ProbeOutcome` and direct SSE parsers gain optional normalized usage (input/output/cache tokens plus model). The Sub2API client reads `/admin/channels/model-pricing?model=...` with the existing admin key and calculates input/output/cache cost at multiplier `1.0`. Each check updates a persistent per-account aggregate with request count, token totals, estimated cost, last observed cost, and last usage timestamp. Unknown usage or pricing increments request/history state but leaves monetary totals unchanged and marks the amount unavailable.

### 6. Extend the existing dense operations UI, not the navigation model

The account row keeps final multiplier and protection multiplier side by side. Protected state uses a warning badge and text, never color alone. A small protection editor handles setting/clearing the threshold; a warning-state row exposes “解除倍率保护”, and every row exposes “移除绑定”. Both destructive actions use the existing native `<dialog>` pattern with visible consequences, cancel/escape, focusable controls, disabled/loading states, and a success/error toast.

## Risks / Trade-offs

- [Sub2API binding update fails after a guard decision] → Keep the shadow record and retry on the next reconciliation; expose an action error without silently changing the logical membership.
- [A key snapshot is deleted or becomes stale while protected] → Keep the logical record visible, mark the multiplier unavailable, and do not guess or rebind until a fresh source tuple is available.
- [A manual bulk save races with periodic reconciliation] → Serialize both through the manager mutex and delete protection records before applying deselection unbinds.
- [Model pricing is unavailable] → Preserve token/request counters, set the monetary total's availability flag false for that observation, and never fabricate a cost.
- [Usage events differ by protocol] → Normalize OpenAI Chat/Responses, Anthropic, and Gemini usage shapes; direct tests without a terminal usage event remain cost-unavailable.
- [Old state files have no protection or statistics fields] → Accept prior state versions, initialize empty maps/zero aggregates, and never alter unprotected bindings.

## Migration Plan

1. Deploy the sidecar with the new optional state fields; no Sub2API migration is needed.
2. On startup, load v3 state, initialize protection records and probe statistics as empty, then reconcile existing physical bindings without creating protection records.
3. Configure protection per group-account from the administrator UI; verify one automatic unbind, one automatic recovery, manual release, and manual removal.
4. Roll back by restoring the previous sidecar image; unknown optional JSON fields are ignored by the older binary and existing physical bindings remain the source of truth.

## Open Questions

None. Equality is intentionally treated as safe (`final_multiplier <= protection_multiplier`), and a missing usage/pricing result is explicitly non-monetary rather than estimated.
