## 1. Credential-flow protocol

- [x] 1.1 Add request-local credential-key discovery and classify supported, explicitly legacy, and fail-closed responses.
- [x] 1.2 Add standard-library RSA-OAEP/AES-256-GCM envelope generation with upstream-adjusted time, expiry validation, safe key parsing, and base64url encoding.

## 2. Sub2API adapter integration

- [x] 2.1 Integrate capability negotiation into Sub2API password login, merge credential-flow and captcha Cookies, and preserve the exact legacy request on supported fallback responses.
- [x] 2.2 Classify browser-credential-flow rejection and credential discovery failures with actionable sanitized adapter errors.

## 3. Regression coverage

- [x] 3.1 Test secure login by decrypting the submitted envelope and verifying algorithm fields, upstream time, absence of top-level credentials, and merged Cookies with and without captcha.
- [x] 3.2 Test 404, 405, and SPA HTML legacy fallbacks and verify Token, Cookie/session, and NewAPI behavior remain unchanged.
- [x] 3.3 Test unsupported algorithms, malformed or unsafe keys, expired material, denial, rate limiting, server failure, and network failure without plaintext downgrade.

## 4. Validation

- [x] 4.1 Run formatting, unit tests, race tests, vet, JavaScript syntax checks, diff checks, and strict OpenSpec validation.
