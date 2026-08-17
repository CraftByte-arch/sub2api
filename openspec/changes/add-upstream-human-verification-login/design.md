## Context

The sidecar currently supports password, Token, and Cookie/session authentication through site-specific adapters. Password login is a single request: the administrator submits credentials, the adapter logs in from the sidecar, the manager verifies the returned session, and only successful material is encrypted and persisted. CAPTCHA failures are classified but require the administrator to log in elsewhere and copy a session, which fails for upstreams that bind sessions to the login IP.

Sub2API-compatible sites can advertise `local_captcha_enabled` through `/api/v1/settings/public`, issue a base64 image plus `captcha_id` through `/api/v1/auth/captcha`, and accept `captcha_id` plus `captcha_code` in `/api/v1/auth/login`. The challenge and final login must originate from the same management-site URL and server egress. Existing NewAPI and no-CAPTCHA paths must remain unchanged.

## Goals / Non-Goals

**Goals:**

- Support human-entered local image or numeric CAPTCHAs for Sub2API password login.
- Keep challenge creation and final login on the sidecar so IP-bound sessions are created from the same server egress used for later synchronization.
- Preserve existing password, Token, and Cookie/session behavior when no supported challenge is advertised.
- Keep incomplete challenges memory-only, short-lived, single-use, and scoped to the authenticated administrator and upstream identity operation.
- Reuse the current successful-login credential encryption, verification, persistence, and immediate synchronization path.

**Non-Goals:**

- Automatically solving CAPTCHAs or integrating third-party solving services.
- Bypassing Turnstile, reCAPTCHA, Tencent, Aliyun, slider, device-fingerprint, or browser-integrity challenges.
- Running a remote interactive browser in this change.
- Modifying Sub2API production code, its database, or the sidecar state schema.
- Persisting passwords, CAPTCHA images, CAPTCHA answers, incomplete sessions, or challenge records.

## Decisions

### Use a preflight challenge endpoint only for password login

The administrator UI will call an administrator-protected challenge endpoint before the existing password connection request. The manager asks an optional adapter capability whether a local challenge is required. A `required: false` result immediately continues through the existing connection endpoint. Token and Cookie/session modes skip preflight entirely.

This keeps the successful connection contract intact and ensures credentials are not transmitted merely to obtain an image. An alternative was to attempt login first and return a challenge from the failed request, but that would submit passwords unnecessarily and would persist the current failed-identity status path.

### Keep adapter challenge support optional and fail open to current behavior

Local-CAPTCHA discovery is exposed through a small optional adapter interface implemented by the Sub2API adapter. NewAPI and any future adapters without the interface behave as `required: false`. If the public settings response is unavailable, unrecognized, or lacks CAPTCHA fields, discovery also returns `required: false`; the existing login path remains authoritative.

When settings explicitly select an external verification provider, the adapter returns the existing `CAPTCHA_REQUIRED` class with browser-login guidance. The sidecar does not attempt to fabricate or solve an external-provider token.

### Wrap upstream challenge material in an opaque web-layer attempt

The adapter returns the upstream `captcha_id`, validated image data, and any response Cookie needed to preserve challenge context. The web server stores those values in an in-memory challenge store and returns only a cryptographically random opaque attempt ID, image data, provider label, and expiry time.

Attempts are bound to administrator ID, upstream ID, optional identity ID, and the normalized management-site URL. Completion atomically consumes the attempt before login and supplies the stored upstream `captcha_id` and Cookie to the adapter. This prevents browser tampering with upstream challenge material and gives the sidecar deterministic single-use and scope enforcement.

An alternative was to return the raw upstream `captcha_id` directly to the browser. It would be simpler but would delegate expiry, replay, and cross-administrator isolation entirely to heterogeneous upstream implementations.

### Use a five-minute memory-only lifecycle

Challenge attempts expire after five minutes, are deleted on successful lookup regardless of the subsequent login result, and are never written to `state.json`. Expired entries are opportunistically pruned during store operations. A failed or mistyped CAPTCHA therefore requires a fresh image, matching the behavior of typical local-CAPTCHA implementations.

Server restart intentionally invalidates pending challenges. The administrator can request a new challenge without affecting any previously connected identity.

### Validate image and input boundaries before returning them

The sidecar accepts only bounded `data:image/png`, `data:image/jpeg`, or `data:image/webp` base64 images from the upstream. CAPTCHA IDs and answers have explicit size and control-character limits. Error responses and logs never contain the image, answer, password, upstream challenge ID, Cookie, Token, or encrypted credential envelope.

### Reuse the current network and credential lifecycle

Challenge discovery, image retrieval, final password login, session verification, immediate key/balance synchronization, and later periodic synchronization all use the upstream management-site address from the sidecar. Any process-wide proxy configuration therefore applies consistently. Successful material is persisted only through the existing `CredentialBox` encryption path.

## Risks / Trade-offs

- [An upstream requires a browser challenge despite advertising local CAPTCHA] → Return a bounded, explicit failure and retain Token/Cookie guidance; do not downgrade to automation or a solver.
- [A challenge depends on response cookies] → Capture challenge-response cookies in the memory-only attempt and attach them only to the matching final login.
- [The management-site URL or identity changes after image retrieval] → Bind and validate those dimensions when consuming the attempt; reject mismatches and require refresh.
- [A sidecar restart loses an in-progress CAPTCHA] → Treat this as expected ephemeral state and let the administrator request a new image.
- [Untrusted image data is oversized or malformed] → Enforce media type, base64 decoding, and decoded-size limits before returning it to the browser.
- [Capability discovery adds a request to password login] → Limit it to password mode, fail open to the existing login path, and avoid periodic probing or persistent capability state.
- [IP binding also includes browser fingerprinting or TLS characteristics] → This HTTP flow may remain insufficient; a future remote-browser capability is required rather than weakening this design.

## Migration Plan

1. Deploy the updated sidecar image only; no Sub2API migration or restart is required.
2. Existing persisted identities and credentials load without transformation because the state schema is unchanged.
3. Validate a no-CAPTCHA Sub2API login and Token/Cookie login before enabling a local-CAPTCHA site.
4. Validate local-CAPTCHA challenge retrieval and login from HC2, followed by balance and key synchronization.
5. Roll back by restoring the previous sidecar image. Pending challenges disappear, while all previously persisted identities remain usable.

## Open Questions

- Remote interactive browser support for external verification providers is intentionally deferred to a separate change.
