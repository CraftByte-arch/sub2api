## ADDED Requirements

### Requirement: Password login material is encrypted for automatic re-login
The sidecar SHALL retain the username and password only for a successfully connected password-mode upstream identity, SHALL store the re-login fields only inside the existing encrypted credential envelope, and SHALL NOT expose the password, session material, or new re-login field names through public APIs, logs, status messages, or plaintext state fields. The existing authenticated Principal display SHALL remain unchanged.

#### Scenario: Successful password connection
- **WHEN** an administrator successfully connects a Sub2API or NewAPI upstream by username and password
- **THEN** the sidecar stores the resulting session material together with the username and password inside the identity's encrypted credential envelope

#### Scenario: Non-password connection
- **WHEN** an administrator connects an upstream by Token or Cookie/session
- **THEN** the sidecar stores no password re-login material and preserves the existing authentication behavior

### Requirement: Expired sessions fall back to one password re-login
The sidecar SHALL keep the existing session validation and refresh flow as the first choice and, when an expired password-mode identity cannot be restored by that flow, SHALL attempt at most one password login during that synchronization run.

#### Scenario: Refresh restores the session
- **WHEN** an expired session has a usable refresh token or Cookie and refresh succeeds
- **THEN** the sidecar continues synchronization without submitting the stored password

#### Scenario: Refresh fails and password re-login succeeds
- **WHEN** an expired session cannot be refreshed and the encrypted material contains a complete username and password
- **THEN** the sidecar performs one password login, verifies the new session, continues the current balance, Key, and multiplier synchronization, and encrypts the new session material for future runs

#### Scenario: Identity has no stored password
- **WHEN** an expired Token, Cookie/session, or legacy password identity has no complete stored password material
- **THEN** the sidecar does not attempt password login and reports the existing session-expired or refresh failure state

### Requirement: Automatic re-login preserves verification boundaries
The sidecar MUST NOT bypass CAPTCHA, two-factor authentication, browser challenges, IP verification, or upstream access controls during automatic password re-login.

#### Scenario: Human verification is required
- **WHEN** the upstream requires CAPTCHA, two-factor authentication, or another unsupported human verification step during automatic re-login
- **THEN** the sidecar stops after that single login attempt, records the corresponding safe identity status, and requires administrator action

#### Scenario: Management site access is denied
- **WHEN** the initial authenticated session check is rejected as management-site access denial rather than session expiration
- **THEN** the sidecar does not submit the stored password and preserves the access-denied classification

### Requirement: Existing encrypted identities remain compatible
The sidecar SHALL read credential envelopes created before this change without migration and SHALL preserve their prior refresh-only behavior until the administrator reconnects them with a password.

#### Scenario: Legacy credential envelope
- **WHEN** the sidecar decrypts an existing envelope that contains session fields but no username or password fields
- **THEN** synchronization proceeds with the previous validation and refresh behavior and no automatic password login is attempted
