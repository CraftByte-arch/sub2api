## Why

Some Sub2API-compatible upstreams require a local image or numeric CAPTCHA during password login and may bind the resulting session to the login IP. A token copied from an administrator's local browser can therefore fail when the sidecar later uses it from HC2, preventing balance, key, and multiplier synchronization.

## What Changes

- Add a server-origin, human-in-the-loop login challenge for upstreams that explicitly enable a local CAPTCHA.
- Let the administrator request the CAPTCHA from the sidecar, view the upstream-provided image in the existing login dialog, manually enter the code, and complete the password login from the same HC2 egress path.
- Keep CAPTCHA attempts short-lived, single-use, administrator-scoped, and memory-only; never persist CAPTCHA codes, images, passwords, or incomplete login material.
- Preserve the current direct password, Token, and Cookie/session login behavior for upstreams without a supported CAPTCHA or when capability discovery is unavailable.
- Continue returning explicit guidance for browser-based challenges such as Turnstile, reCAPTCHA, sliders, or other unsupported verification providers instead of attempting to bypass them.
- Keep all changes inside `account-auto-scheduler`; Sub2API production code and storage remain unchanged.

## Capabilities

### New Capabilities

- `upstream-human-verification-login`: Server-origin local-CAPTCHA discovery, challenge lifecycle, administrator entry flow, and backward-compatible upstream login behavior.

### Modified Capabilities

None.

## Impact

- Affected code: `account-auto-scheduler/internal/upstream`, `account-auto-scheduler/internal/web`, and the embedded administrator UI assets.
- Affected APIs: new administrator-protected endpoints for starting and completing a local-CAPTCHA login challenge; existing upstream connection endpoints retain their current contracts for non-CAPTCHA login modes.
- Persistence: no state version or schema change; successful credentials continue to use the existing encrypted credential envelope.
- External systems: supported CAPTCHA and login requests originate from the sidecar's configured management-site path and network egress.
