## ADDED Requirements

### Requirement: Sub2API password login SHALL negotiate credential-envelope support
The sidecar SHALL request the Sub2API management site's `/api/v1/auth/credential-key` endpoint immediately before an administrator-initiated password login and SHALL select the login representation from the returned protocol capability rather than from the site's hostname.

#### Scenario: Upstream advertises a supported credential flow
- **WHEN** the credential-key endpoint returns successful JSON containing `RSA-OAEP-256+A256GCM`, a key ID, a valid RSA public key, server time, and unexpired key and flow deadlines
- **THEN** the sidecar uses encrypted credential-envelope login for that attempt

#### Scenario: Traditional upstream returns a missing-route status
- **WHEN** the credential-key endpoint returns HTTP 404 or 405
- **THEN** the sidecar sends the existing traditional Sub2API password request without changing its fields or behavior

#### Scenario: Traditional upstream returns its SPA document
- **WHEN** the credential-key endpoint returns a successful HTML document instead of protocol JSON
- **THEN** the sidecar treats the endpoint as unsupported and sends the existing traditional Sub2API password request

#### Scenario: Other authentication modes are used
- **WHEN** the upstream type is NewAPI or the administrator selects Token or Cookie/session login
- **THEN** the sidecar does not request the Sub2API credential-key endpoint and preserves the existing login path

### Requirement: Supported credential flows SHALL produce a browser-compatible envelope
For a supported flow, the sidecar SHALL generate a fresh AES-256-GCM key and 12-byte IV, SHALL encrypt a JSON credential payload containing `email`, `password`, and upstream-adjusted `issued_at`, SHALL use the key ID as additional authenticated data, SHALL wrap the AES key with RSA-OAEP-SHA256, and SHALL encode binary envelope fields as unpadded base64url.

#### Scenario: Encrypted password login is submitted
- **WHEN** a supported credential flow is valid and envelope generation succeeds
- **THEN** the login body contains the captcha fields and `credential_envelope` with `algorithm`, `key_id`, `encrypted_key`, `iv`, and `ciphertext`, and contains no top-level `email` or `password`

#### Scenario: Upstream server time differs from HC2 time
- **WHEN** the credential-key response reports a server time offset from the local clock
- **THEN** the decrypted `issued_at` is derived from the upstream time and remains within the advertised flow lifetime

### Requirement: Credential-flow and captcha Cookies SHALL remain in one ephemeral same-origin attempt
The sidecar SHALL merge Cookies returned by credential-key discovery with Cookies already captured for the local captcha and SHALL send the merged Cookie header only to the same normalized management origin on the final login request.

#### Scenario: Local captcha and credential flow are both enabled
- **WHEN** the captcha challenge supplies one Cookie and credential-key discovery supplies an authentication-flow Cookie
- **THEN** the final login request includes both Cookie values together with the opaque captcha ID and administrator-entered code

#### Scenario: Credential flow is enabled without a local captcha
- **WHEN** no captcha challenge is required but credential-key discovery succeeds
- **THEN** the sidecar submits the encrypted envelope with the authentication-flow Cookie and no captcha fields

### Requirement: Secure-protocol failures SHALL NOT downgrade to plaintext credentials
Once the credential-key endpoint responds as a protocol endpoint rather than an explicitly unsupported route, the sidecar MUST reject invalid, expired, unsupported, denied, rate-limited, malformed, or unavailable credential-flow material without posting top-level account credentials to `/api/v1/auth/login`.

#### Scenario: Algorithm or public key is unsupported
- **WHEN** successful protocol JSON contains an unknown algorithm, a non-RSA key, an unsafe RSA key, or an undecodable public key
- **THEN** the login fails with a sanitized credential-flow error and the login endpoint is not called

#### Scenario: Key or flow has expired
- **WHEN** `expires_at` or `flow_expires_at` is not later than the upstream-adjusted current time
- **THEN** the login fails before encryption and does not send a traditional password request

#### Scenario: Credential-key endpoint is denied or temporarily unavailable
- **WHEN** discovery returns HTTP 401, 403, 429, a server error, a network error, or non-HTML malformed success content
- **THEN** the login reports the credential-flow discovery failure and does not downgrade to the traditional password body

### Requirement: Credential-flow material SHALL not become durable or observable
The sidecar SHALL keep the public-key response, flow Cookies, generated symmetric key, IV, plaintext credential serialization, and encrypted envelope in request-local memory only and SHALL not persist or log those values.

#### Scenario: Login succeeds or fails
- **WHEN** an encrypted login attempt completes by any outcome
- **THEN** no credential-flow key material, envelope, password, full Cookie, or upstream access token is added to the sidecar state file, API response, or application logs

### Requirement: Credential-flow rejection SHALL be classified accurately
The sidecar SHALL recognize upstream responses indicating that a browser credential flow is required and SHALL distinguish them from management-site IP denial while preserving existing invalid-credential, captcha, 2FA, and session-expiry classifications.

#### Scenario: Upstream rejects a traditional body with browser-flow wording
- **WHEN** the login response contains `browser credential flow is required`
- **THEN** the administrator receives an actionable encrypted-login compatibility error rather than the generic management-site access-denied message
