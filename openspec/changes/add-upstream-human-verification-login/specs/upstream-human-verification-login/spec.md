## ADDED Requirements

### Requirement: Backward-compatible CAPTCHA capability discovery
The sidecar SHALL check for a supported local CAPTCHA only for password-mode connections whose adapter exposes the optional challenge capability. An upstream that does not explicitly advertise a supported local CAPTCHA MUST continue through the existing login behavior.

#### Scenario: Upstream has no CAPTCHA
- **WHEN** an administrator starts password login and the upstream reports that local CAPTCHA is disabled
- **THEN** the sidecar returns `required: false` and the administrator UI continues through the existing connection endpoint without displaying CAPTCHA controls

#### Scenario: Capability discovery is unavailable
- **WHEN** the public capability endpoint is unavailable, unrecognized, or lacks supported CAPTCHA fields
- **THEN** the sidecar treats local CAPTCHA as not detected and preserves the existing password login behavior

#### Scenario: Non-password login
- **WHEN** an administrator connects with Token or Cookie/session authentication
- **THEN** the UI and sidecar skip CAPTCHA discovery and use the existing authentication path unchanged

#### Scenario: External verification provider is enabled
- **WHEN** the upstream explicitly requires Turnstile, reCAPTCHA, Tencent, Aliyun, slider, or another unsupported browser verification provider
- **THEN** the sidecar rejects automated completion with an explicit `CAPTCHA_REQUIRED` response and instructs the administrator to use a browser-derived session

### Requirement: Server-origin local CAPTCHA challenge
The sidecar SHALL request supported local CAPTCHA material from the configured upstream management-site address and return a bounded image through an administrator-protected API without submitting the administrator's upstream password.

#### Scenario: Local CAPTCHA is available
- **WHEN** an authenticated administrator starts a password challenge for a Sub2API upstream that advertises `local_captcha_enabled`
- **THEN** the sidecar obtains the CAPTCHA from that upstream and returns `required: true`, an opaque attempt ID, a validated image data URL, the provider type, and an expiry time

#### Scenario: Credentials are not sent during challenge creation
- **WHEN** the UI requests a local CAPTCHA image
- **THEN** the request to the upstream contains no administrator username, password, Token, Cookie/session credential, or persisted identity material

#### Scenario: Upstream image is unsafe
- **WHEN** the upstream returns an unsupported media type, invalid base64, an empty challenge ID, control characters, or an image exceeding the configured limit
- **THEN** the sidecar rejects the challenge with a bounded error and does not return the unsafe image to the browser

### Requirement: Scoped and ephemeral challenge lifecycle
The sidecar MUST keep incomplete challenge material only in memory for no more than five minutes and MUST enforce administrator, upstream, identity, management-site, and single-use boundaries.

#### Scenario: Matching administrator completes a challenge
- **WHEN** the administrator who started the challenge submits it for the same upstream, optional identity, and management-site address before expiry
- **THEN** the sidecar atomically consumes the challenge and permits one login attempt with the stored upstream challenge material

#### Scenario: Challenge scope does not match
- **WHEN** a different administrator, upstream, identity, or management-site address attempts to use the opaque attempt ID
- **THEN** the sidecar rejects the request without disclosing stored challenge material

#### Scenario: Challenge is expired or reused
- **WHEN** an attempt is older than five minutes or has already been consumed
- **THEN** the sidecar rejects it and requires the administrator to request a fresh CAPTCHA

#### Scenario: Sidecar restarts
- **WHEN** the sidecar restarts while a CAPTCHA dialog is open
- **THEN** the pending attempt is lost without changing persisted upstream identities or credentials

### Requirement: Human-entered CAPTCHA login completion
The administrator UI SHALL display the upstream-provided image and SHALL let the administrator manually enter and refresh the CAPTCHA. The final login SHALL originate from the sidecar using the stored challenge context and the current password form values.

#### Scenario: Successful CAPTCHA login
- **WHEN** the administrator submits a valid CAPTCHA answer and valid upstream credentials
- **THEN** the sidecar sends the stored upstream `captcha_id`, the manual `captcha_code`, any matching challenge Cookie, and the credentials to the upstream login endpoint from the sidecar network path
- **AND** the existing verification, encrypted credential persistence, balance retrieval, key synchronization, and identity status flow completes unchanged

#### Scenario: CAPTCHA answer fails
- **WHEN** the upstream rejects a consumed CAPTCHA answer or the challenge has expired
- **THEN** the UI retains the connection form values, reports the bounded failure, and allows the administrator to request a fresh image without persisting incomplete login material

#### Scenario: Administrator refreshes CAPTCHA
- **WHEN** the administrator selects the refresh action before completing login
- **THEN** the UI requests a new opaque challenge and stops using the previous attempt

### Requirement: Sensitive challenge data handling
The sidecar MUST NOT persist or log the CAPTCHA answer, CAPTCHA image, upstream challenge ID, challenge Cookie, password, Token, Cookie/session credential, or incomplete login response.

#### Scenario: Challenge is stored
- **WHEN** a local CAPTCHA challenge is active
- **THEN** only its opaque attempt wrapper exists in process memory and `state.json` remains unchanged

#### Scenario: Error is reported
- **WHEN** challenge discovery or CAPTCHA login fails
- **THEN** logs and administrator responses contain only bounded error codes and messages without sensitive challenge or credential values
