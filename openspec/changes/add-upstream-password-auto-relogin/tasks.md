## 1. Encrypted login material

- [x] 1.1 Extend `AuthMaterial` with optional password re-login fields and helpers while preserving legacy envelope compatibility
- [x] 1.2 Add encryption and redaction tests proving password login material remains inside the credential ciphertext

## 2. Adapter fallback behavior

- [x] 2.1 Preserve password login inputs in successful Sub2API and NewAPI login material
- [x] 2.2 Add Sub2API refresh-first, single password re-login fallback without bypassing challenge or access-denied states
- [x] 2.3 Add NewAPI refresh-first, single password re-login fallback without changing Token or Cookie/session behavior

## 3. Verification

- [x] 3.1 Add adapter tests for successful automatic re-login, refresh priority, legacy credentials, and human-verification failure
- [x] 3.2 Add manager persistence coverage proving re-login session material is re-encrypted and no plaintext credential is exposed
- [x] 3.3 Run formatting, focused tests, full sidecar tests, and production build validation

## 4. Release

- [ ] 4.1 Commit only this change's sidecar code, tests, and OpenSpec artifacts
- [ ] 4.2 Back up HC2 sidecar state, deploy only the `account-auto-scheduler` container, and verify health plus unaffected Sub2API containers
