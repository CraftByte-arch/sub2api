## 1. State and encryption foundations

- [x] 1.1 Extend scheduler models and JSON state migration with notification settings, account/group balance thresholds, and persisted alert-edge state.
- [x] 1.2 Add store interfaces and clone/default logic for notification settings, group thresholds, and alert state without breaking existing state files.
- [x] 1.3 Add separate-AAD encrypted byte helpers to the existing credential box and unit-test round trips, tamper rejection, and disabled-key behavior.

## 2. Bark delivery and transition evaluator

- [x] 2.1 Implement the Bark client with endpoint validation, Basic Auth, AES-128-CBC/PKCS#7 payload encryption, random IVs, bounded HTTP timeouts, and secret-safe errors.
- [x] 2.2 Implement the notification coordinator and projection interfaces for available balances, physical usable group capacity, final multipliers, and protection state.
- [x] 2.3 Implement account threshold precedence and edge-triggered low-balance/recovery state transitions.
- [x] 2.4 Implement group zero/one/many capacity transitions and multiplier-change deduplication with protection status in messages.
- [x] 2.5 Wire the coordinator into the server lifecycle with a bounded evaluation interval; ensure delivery failures never stop probe, sync, or protection loops.

## 3. Administrator API

- [x] 3.1 Add admin-authenticated notification settings/status and test-send endpoints with masked responses and validation errors.
- [x] 3.2 Add account and group balance-threshold endpoints or request fields, including clear/unset behavior and overview metadata.
- [x] 3.3 Add API tests for authorization, secret redaction, threshold precedence, validation, and test-delivery failure isolation.

## 4. Administrator UI

- [x] 4.1 Add account-level balance alert threshold controls to the existing account configuration dialog.
- [x] 4.2 Add group-level threshold controls and effective-threshold indicators to the group view.
- [x] 4.3 Add a Bark settings dialog with endpoint, device key, encryption-key instructions, Basic Auth fields, configured status, and test-send action.
- [x] 4.4 Add accessible loading/error/success states and ensure notification secrets never render back into the page.

## 5. Verification and documentation

- [x] 5.1 Add unit tests for AES payload compatibility, edge-state persistence, recovery thresholds, restart behavior, and multiplier messages.
- [x] 5.2 Run Go tests, race tests, vet, JavaScript syntax checks, OpenSpec validation, and diff checks; fix regressions from the existing group-protection work.
- [x] 5.3 Document Bark setup, the required 16-byte app encryption key, Basic Auth fields, alert semantics, and failure/recovery behavior in the scheduler README.
