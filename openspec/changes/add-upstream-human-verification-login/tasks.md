## 1. Adapter Challenge Capability

- [x] 1.1 Add bounded local-CAPTCHA challenge and completion fields without changing persisted upstream models.
- [x] 1.2 Implement Sub2API public-settings discovery, supported image retrieval, and explicit external-provider rejection.
- [x] 1.3 Submit CAPTCHA ID, manual code, and challenge Cookie during password login while preserving no-CAPTCHA, Token, and session behavior.

## 2. Ephemeral Challenge API

- [x] 2.1 Add an opaque in-memory challenge store with five-minute expiry, scope validation, pruning, and atomic single-use consumption.
- [x] 2.2 Add the optional manager challenge capability using the resolved management-site address without persisting incomplete state.
- [x] 2.3 Add administrator-protected challenge creation and connection completion handling with bounded structured errors.

## 3. Administrator UI

- [x] 3.1 Add local-CAPTCHA image, manual code, expiry, and refresh controls to the upstream connection dialog.
- [x] 3.2 Implement password preflight, challenge completion, refresh, expiry/error recovery, and unchanged non-password behavior.
- [x] 3.3 Add responsive and accessible styling for the CAPTCHA controls and update administrator guidance.

## 4. Verification

- [x] 4.1 Add adapter tests for no CAPTCHA, discovery failure, local CAPTCHA, external CAPTCHA, image validation, challenge Cookie, and successful login payloads.
- [x] 4.2 Add challenge-store and administrator API tests for authorization, scope, expiry, single use, and unchanged connection behavior.
- [x] 4.3 Add static UI assertions and JavaScript syntax checks for the new challenge flow.
- [x] 4.4 Update sidecar documentation and run Go tests, race tests, vet, JavaScript checks, diff checks, and strict OpenSpec validation.
