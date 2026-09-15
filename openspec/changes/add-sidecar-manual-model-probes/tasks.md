## 1. Backend
- [x] 1.1 Add validated manual multi-model probe request/response types and admin route.
- [x] 1.2 Reuse direct credential export and streaming prober without mutating scheduler state.
- [x] 1.3 Add reasoning-effort propagation for supported upstream protocols with default behavior unchanged.

## 2. Frontend
- [x] 2.1 Add account-row/detail manual probe controls and dialog markup.
- [x] 2.2 Add multi-model selection, prompt/reasoning controls, loading, result and error states.
- [x] 2.3 Add responsive styles and accessibility labels.

## 3. Validation
- [x] 3.1 Add backend and static UI regression tests.
- [x] 3.2 Run syntax, full tests, vet and diff validation.

## 4. Regression fixes
- [x] 4.1 Proxy Sub2API's real per-account model list and remove hard-coded model candidates.
- [x] 4.2 Rebuild the manual-probe dialog with existing modal/form/theme patterns and accessible async states.
- [x] 4.3 Read exclusive-group users through the existing read-only database pool while keeping mutations on the administrator HTTP API.
- [x] 4.4 Add regression tests and rerun full sidecar validation.

## 5. Configuration-independent manual probing
- [x] 5.1 Remove the obsolete managed-account configuration prerequisite and add engine regression coverage.
- [x] 5.2 Add manual probe to the account overflow menu, validate UI behavior, and deploy the sidecar.
