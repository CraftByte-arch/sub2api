## Context

The sidecar currently connects to Sub2API upstreams by posting `email`, `password`, and optional local-captcha fields directly to `/api/v1/auth/login`. A hardened Sub2API-compatible deployment instead exposes `/api/v1/auth/credential-key`, sets a short-lived same-origin authentication-flow Cookie, and requires the account credentials inside an `RSA-OAEP-256+A256GCM` envelope. The deployed browser obtains this material immediately before login and the upstream rejects the traditional request with `browser credential flow is required`.

HC2 capability probing on August 17, 2026 found three observable classes among the configured Sub2API upstreams: a valid credential-flow JSON response, explicit 404 responses, and one SPA-style HTML response for the unknown API route. Compatibility therefore cannot assume either protocol globally and must not be tied to a hostname.

The sidecar already owns the password for the duration of one administrator-initiated login, has a same-origin HTTP client, and carries local-captcha cookies through an opaque in-memory challenge. No persistent model or browser API change is required.

## Goals / Non-Goals

**Goals:**

- Negotiate the hardened credential-envelope protocol automatically for Sub2API password login.
- Reproduce the browser envelope with Go standard-library cryptography and upstream-adjusted time.
- Preserve all response Cookies required by the captcha and credential flows on the final same-origin login request.
- Retain the exact legacy password request for upstreams that clearly do not expose the credential endpoint.
- Fail closed when a server appears to expose the secure protocol but returns invalid, expired, unsupported, or transiently denied key material.
- Keep NewAPI, Token, Cookie/session, persisted state, and administrator-facing request contracts unchanged.

**Non-Goals:**

- Bypassing Cloudflare, Turnstile, sliding captchas, or IP-bound browser verification.
- Copying browser-only `cf_clearance`, refresh-token, or fingerprint headers into the sidecar.
- Adding a hostname-specific switch or administrator configuration for this protocol.
- Modifying the Sub2API service, database schema, or official login behavior.

## Decisions

### 1. Probe only inside Sub2API password login

Immediately before a password login POST, the Sub2API adapter requests `/api/v1/auth/credential-key`. This keeps the decision local to the only authentication mode that needs account-credential encryption. NewAPI and the existing Token and Cookie/session branches do not execute the probe.

The probe occurs after any UI captcha has been fetched but before login submission. This matches the observed browser ordering and lets the adapter merge the existing captcha Cookie with the new authentication-flow Cookie without changing the web challenge store.

**Alternative considered:** detect and persist the capability while creating or synchronizing an upstream. This would add state migration and could become stale after an upstream upgrade, so it is rejected.

### 2. Select secure or legacy behavior from the response contract

The adapter selects the secure path only when a successful JSON response contains the supported algorithm, key identifier, public key, server time, key expiry, and flow expiry. Explicit HTTP 404 or 405 responses select the existing legacy path. A successful HTML document that clearly represents an SPA fallback for an unknown API route also selects the legacy path because one current upstream behaves this way.

All other responses fail the login before account credentials are posted. In particular, authorization failures, rate limits, server errors, malformed JSON, unsupported algorithms, invalid keys, and expired material do not downgrade to plaintext.

**Alternative considered:** fall back for every non-successful probe. This maximizes compatibility but can disclose a password in a traditional JSON body when a secure endpoint is temporarily unavailable or intercepted, so it is rejected.

### 3. Generate the browser-compatible envelope in a dedicated helper

A new upstream helper performs the cryptographic work:

1. Parse the upstream SPKI RSA public key from its encoded representation.
2. Generate a random 32-byte AES key and 12-byte GCM IV with `crypto/rand`.
3. Serialize `{email,password,issued_at}` where `issued_at` uses the upstream server-time offset.
4. Encrypt the serialized credentials with AES-256-GCM using the key ID bytes as additional authenticated data.
5. Encrypt the AES key with RSA-OAEP and SHA-256.
6. Encode the encrypted key, IV, and ciphertext with unpadded base64url.

The resulting login body contains captcha fields plus `credential_envelope`; it does not contain top-level `email` or `password`. The helper uses only the Go standard library and exposes no secret values in errors.

**Alternative considered:** execute browser JavaScript or add a WebCrypto-compatible dependency. Go's standard primitives exactly cover the required operations, so both alternatives add unnecessary runtime and dependency risk.

### 4. Keep all flow material ephemeral and same-origin

The public key metadata, authentication-flow Cookie, AES key, IV, plaintext credential JSON, and resulting envelope exist only during one `Connect` call. The response Cookie is merged by name with the existing local-captcha Cookie and sent only through the adapter's existing same-origin redirect policy. None of this material is written to `state.json`, returned to the browser, or logged.

### 5. Use protocol-specific errors

Credential-key transport failures, unsupported algorithms, invalid keys, expiry, and envelope-generation failures receive distinct sanitized adapter codes. If an upstream still returns `browser credential flow is required`, the response is classified as a credential-flow incompatibility rather than a management-IP denial. Existing credential, captcha, 2FA, and session-expiry classifications remain ordered ahead of the generic HTTP 403 mapping.

## Risks / Trade-offs

- **[SPA HTML fallback could conceal a nonstandard security page]** → Limit this fallback to HTTP 2xx HTML that contains clear document markup and no valid credential-flow JSON; 403 and other denial responses never downgrade.
- **[Upstream clock skew invalidates the envelope]** → Derive `issued_at` from the server time returned with the key and recheck both expiry fields before encryption.
- **[RSA key format varies]** → Accept the observed base64-encoded SPKI form and standard PEM `PUBLIC KEY`, but require an RSA key of a safe size and reject all other key types.
- **[Cookie merge loses flow state]** → Reuse the existing cookie-by-name merge helper and test that captcha and authentication-flow Cookies are both present on the final login request.
- **[Protocol changes again]** → Match the declared algorithm strictly and fail with an actionable unsupported-protocol error instead of guessing.

## Migration Plan

1. Ship the change only in the `account-auto-scheduler` image; no state migration is needed.
2. Validate legacy password, encrypted password with and without local captcha, Token, Cookie/session, and NewAPI tests before deployment.
3. Recreate only the HC2 sidecar container while retaining its existing environment and state volume.
4. Roll back by restoring the previous sidecar image; persisted data remains compatible because no schema changes occur.

## Open Questions

None. The live protocol fields and legacy response classes have been observed from HC2, and the implementation can remain capability-based rather than site-specific.
