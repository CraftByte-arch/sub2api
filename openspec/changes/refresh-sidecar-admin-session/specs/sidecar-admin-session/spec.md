## ADDED Requirements

### Requirement: Sidecar uses the current same-origin administrator session
The sidecar SHALL read the current same-origin `localStorage` access token when loading and before authenticated API requests, instead of relying on a stale URL or session-storage token.

#### Scenario: Sidecar page is refreshed
- **WHEN** the sidecar page reloads while the administrator is still logged in
- **THEN** it uses the current same-origin access token and loads without requiring the administrator to reopen the menu

#### Scenario: Another tab rotates the access token
- **WHEN** the Sub2API admin tab writes a new access token to same-origin storage
- **THEN** the open sidecar adopts the new token for subsequent requests

### Requirement: Sidecar refreshes an expired access token in the browser
The sidecar SHALL call the same-origin existing refresh endpoint with the browser's refresh token when an authenticated request receives an expired-session response, and SHALL retry the request once with the new access token.

#### Scenario: Refresh succeeds
- **WHEN** a sidecar API request receives HTTP 401 and a valid refresh token exists
- **THEN** the browser rotates and persists the access token pair and retries the failed request once
- **AND** the refresh token is not sent to the sidecar backend or stored in sidecar state

#### Scenario: Refresh requests race
- **WHEN** multiple sidecar requests detect expiration concurrently
- **THEN** only one refresh request is made in that document and all waiting requests use its result

### Requirement: Expired sessions return to administrator login
The sidecar SHALL redirect to the same-origin administrator login route with a fixed internal redirect path when access and refresh tokens are no longer valid.

#### Scenario: Embedded sidecar session expires
- **WHEN** refresh fails or no refresh token is available while the sidecar is embedded in the admin shell
- **THEN** the top-level window navigates to `/login?redirect=/custom/account-auto-scheduler`

#### Scenario: Login completes
- **WHEN** the administrator successfully logs in, including any required 2FA
- **THEN** the existing login flow navigates back to `/custom/account-auto-scheduler`

### Requirement: Authentication secrets are not placed in sidecar URLs
The sidecar SHALL NOT persist or construct a long-lived URL containing an access token or refresh token.

#### Scenario: Authenticated page address is inspected
- **WHEN** the sidecar is loaded or a token is refreshed
- **THEN** the browser address contains no `token` or `refresh_token` query parameter
