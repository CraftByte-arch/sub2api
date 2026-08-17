## 1. State and persistence

- [x] 1.1 Add backward-compatible group protection default model/state fields, validation, cloning, and store CRUD with load/migration tests.
- [x] 1.2 Add an explicit scope/source marker to protection records and public views, preserving old records as account-level protection.

## 2. Protection reconciliation

- [x] 2.1 Implement effective-threshold resolution with account-level priority and group-level fallback, including the no-final-multiplier no-op rule.
- [x] 2.2 Extend automatic reconciliation to create/remove group-derived logical relationships, unbind/rebind safely, and never recreate a manually removed relationship.
- [x] 2.3 Update protection-aware single/bulk binding operations and deletion of a group default so they remain idempotent and do not bypass an exceeded guard.
- [x] 2.4 Add state-machine tests for precedence, equality, unavailable multipliers, automatic recovery, manual removal, and default deletion.

## 3. Administrator API and projections

- [x] 3.1 Add administrator-protected GET/PUT/DELETE group default endpoints with finite non-negative validation and structured errors.
- [x] 3.2 Include group defaults and inherited/effective protection views in overview and binding-dialog data while preserving platform filtering and logical membership.
- [x] 3.3 Add HTTP tests for authorization, validation, persistence, precedence projection, and no-rebind behavior.

## 4. Administrator UI

- [x] 4.1 Add group-header controls and dialog for setting, editing, and clearing a default protection multiplier.
- [x] 4.2 Show effective protection value/source and unavailable/ exceeded states in account rows, while keeping account-level actions distinct.
- [x] 4.3 Keep binding dialog selection aligned with logical group-derived membership and add responsive/accessibility coverage.

## 5. Verification and documentation

- [x] 5.1 Update sidecar documentation with group/account precedence and manual-removal semantics.
- [x] 5.2 Run formatting, unit/race tests, vet, JavaScript syntax checks, strict OpenSpec validation, and final diff review.
