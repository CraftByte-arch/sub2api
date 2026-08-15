## ADDED Requirements

### Requirement: Administrator-only upstream workspace
The sidecar SHALL expose upstream management only inside the existing administrator-authenticated scheduler page and SHALL protect every upstream API with the same validated Sub2API administrator JWT middleware used by existing scheduler APIs.

#### Scenario: Administrator opens the upstream tab
- **WHEN** a validated administrator selects the `Upstreams` secondary tab
- **THEN** the page SHALL lazily request upstream data and SHALL retain the existing group/account tab state

#### Scenario: Non-administrator calls an upstream API
- **WHEN** a request has no valid administrator JWT or belongs to a non-administrator
- **THEN** the sidecar SHALL reject it without returning upstream, identity, key, binding, or credential metadata

### Requirement: Upstream candidate aggregation
The sidecar SHALL group local API-key accounts by a normalized redacted `base_url`, merge those candidates with persisted upstreams, and SHALL NOT include OAuth or setup-token account credentials in upstream correlation.

#### Scenario: Accounts share one deployment root
- **WHEN** multiple local API-key accounts have base URLs that normalize to the same deployment root
- **THEN** the upstream list SHALL contain one upstream entry with all of those local account summaries

#### Scenario: Persisted upstream has no current local account
- **WHEN** a saved upstream identity remains but no local account currently references its base URL
- **THEN** the upstream SHALL remain visible and SHALL indicate that it has zero associated local accounts

#### Scenario: Local account is removed or moved
- **WHEN** a bound local account is deleted, changes away from API-key type, has an invalid base URL, or changes to another normalized upstream root
- **THEN** the sidecar SHALL clear that local binding while retaining the persisted upstream, identities, encrypted credentials, recharge configuration, and last-good remote-key snapshot

#### Scenario: Automatically derived candidate loses its last account
- **WHEN** an upstream candidate was never persisted and its final matching local API-key account is removed or moved
- **THEN** the candidate SHALL disappear from the next authoritative upstream list

#### Scenario: Administrator adds a deployment URL manually
- **WHEN** an administrator submits a valid HTTP(S) deployment root not present in local account metadata
- **THEN** the sidecar SHALL assign the same stable ID that normalization would derive and SHALL include it in subsequent upstream lists

### Requirement: Credential-free site detection and explicit override
The sidecar SHALL detect supported site families through credential-free bounded requests and SHALL allow an administrator to override the detected family before authentication.

#### Scenario: Sub2API is detected
- **WHEN** the Sub2API public-settings endpoint returns a recognizable successful response
- **THEN** detection SHALL report `sub2api` with evidence and SHALL preselect that adapter

#### Scenario: NewAPI is detected
- **WHEN** the NewAPI status endpoint returns a recognizable successful response
- **THEN** detection SHALL report `newapi` with evidence and SHALL preselect that adapter

#### Scenario: Detection is inconclusive
- **WHEN** neither supported anonymous contract is recognized
- **THEN** detection SHALL report `unknown`, SHALL send no submitted credential to either family, and SHALL require a family override before login

### Requirement: Multiple upstream identities and authentication methods
The sidecar SHALL support multiple named identities for one upstream and SHALL accept password login, access token, or manually supplied cookie/session material through the selected family adapter.

#### Scenario: Password login succeeds
- **WHEN** an administrator submits valid credentials to the explicitly selected adapter and authentication succeeds
- **THEN** the sidecar SHALL save only reusable returned session material, SHALL discard the password, and SHALL mark that identity connected

#### Scenario: Login requires CAPTCHA or two-factor authentication
- **WHEN** the selected upstream reports CAPTCHA, interactive verification, or two-factor authentication is required
- **THEN** the sidecar SHALL stop the login attempt, SHALL mark the actionable required state, and SHALL NOT attempt to bypass the challenge

#### Scenario: Administrator supplies a token manually
- **WHEN** an administrator supplies a valid access token for the selected adapter
- **THEN** the sidecar SHALL verify it before persistence and SHALL allow it to coexist with other identities for that upstream

#### Scenario: Identity expires
- **WHEN** saved authentication material is rejected and cannot be refreshed
- **THEN** the identity SHALL be marked expired while its last successful synchronized snapshot remains available with its timestamp

### Requirement: Credential confidentiality
The sidecar MUST encrypt every reusable access token, refresh token, and cookie at rest with AES-256-GCM under `AUTO_SCHEDULER_CREDENTIAL_KEY`, MUST use identity-bound associated data, and MUST never persist a login password or plaintext API key.

#### Scenario: Credential encryption is configured
- **WHEN** verified reusable authentication material is stored
- **THEN** the state file SHALL contain only a versioned nonce and ciphertext and the browser response SHALL contain no ciphertext or secret value

#### Scenario: Credential encryption is not configured
- **WHEN** the sidecar starts without a valid credential key
- **THEN** existing account scheduling SHALL continue, session metadata SHALL report connection features disabled, and credential-dependent operations SHALL fail with an actionable service-unavailable response

#### Scenario: Upstream request redirects across origins
- **WHEN** an authenticated upstream request receives a redirect to a different scheme or host
- **THEN** the sidecar SHALL reject the redirect and SHALL NOT forward Authorization or Cookie data

#### Scenario: Upstream returns a secret in an error body
- **WHEN** an upstream error response contains request or credential material
- **THEN** the sidecar SHALL return and log only a bounded sanitized error code and message

### Requirement: Versioned state migration
The state store SHALL preserve all existing scheduler configuration and history while adding upstreams, identities, key snapshots, and bindings in state version 2.

#### Scenario: Version 1 state is opened
- **WHEN** the upgraded sidecar opens an existing version 1 scheduler state file
- **THEN** it SHALL initialize empty upstream collections in memory without losing managed accounts or probe history and SHALL write version 2 atomically on the next mutation

#### Scenario: Public upstream views are produced
- **WHEN** stored upstream state is serialized to an API response
- **THEN** dedicated public views SHALL omit encrypted authentication material, HMAC keys, raw headers, and complete remote keys

### Requirement: Sub2API metadata synchronization
The Sub2API adapter SHALL verify the identity and synchronize all accessible user keys through paginated existing user APIs, including status, group, quota, use, and effective group multiplier data.

#### Scenario: Sub2API synchronization succeeds
- **WHEN** a connected Sub2API identity has valid reusable authentication material
- **THEN** the sidecar SHALL replace that identity's normalized key snapshot and SHALL record the successful synchronization time

#### Scenario: User-specific group rate exists
- **WHEN** Sub2API returns a user-specific rate for a key's group
- **THEN** the synchronized key SHALL display that effective rate in preference to the group's default rate

### Requirement: NewAPI metadata synchronization
The NewAPI adapter SHALL synchronize paginated token metadata, group ratios, quota, and use, and SHALL distinguish fixed group ratios from the dynamic `auto` group.

#### Scenario: Fixed NewAPI group is synchronized
- **WHEN** a NewAPI token belongs to a group with a numeric returned ratio
- **THEN** the synchronized key SHALL display that ratio and identify it as a fixed group value

#### Scenario: Auto group has a recent consumption log
- **WHEN** a NewAPI token uses `auto` and a recent unambiguous matching log contains numeric `other.group_ratio`
- **THEN** the synchronized key SHALL display the observed dynamic multiplier and observation time

#### Scenario: Auto group has no matching log
- **WHEN** a NewAPI token uses `auto` but no suitable recent log exists
- **THEN** the synchronized key SHALL display `dynamic` with no numeric observation and SHALL NOT substitute zero

#### Scenario: NewAPI returns masked keys
- **WHEN** the token list exposes only masked key strings
- **THEN** the sidecar SHALL retain the masked display value and SHALL mark the key unavailable for exact automatic matching

### Requirement: Version-tolerant NewAPI login identity
The NewAPI adapter SHALL support current authentication bundles and legacy cookie sessions and SHALL send the authenticated NewAPI user ID when the selected deployment requires it.

#### Scenario: Legacy password login returns the user directly
- **WHEN** NewAPI password login returns a successful user object directly in `data` plus a session cookie
- **THEN** the adapter SHALL retain that user's numeric ID, SHALL send it as `New-Api-User` during verification and synchronization, and SHALL connect the identity

#### Scenario: Administrator supplies a legacy token or session
- **WHEN** an administrator supplies a NewAPI token or Cookie/session together with a valid numeric NewAPI user ID
- **THEN** the adapter SHALL encrypt the user ID with the reusable credential and SHALL send it only as the `New-Api-User` request header

#### Scenario: Legacy session is rejected without a user ID
- **WHEN** a manually supplied NewAPI token or Cookie/session is rejected and no NewAPI user ID was supplied
- **THEN** the sidecar SHALL return an actionable error asking for the user ID instead of presenting only a generic expired-session message

#### Scenario: NewAPI explicitly rejects password credentials
- **WHEN** a NewAPI login response states that the username or password is invalid or the user is disabled
- **THEN** the sidecar SHALL return a dedicated sanitized credential-rejection code, and the interface SHALL direct the administrator to manually enter that upstream's credentials and prefer the site username on legacy deployments

#### Scenario: Browser offers credentials for the administrator host
- **WHEN** the upstream password dialog is opened inside the Sub2API administrator origin
- **THEN** the form SHALL prevent reuse of the current administrator site's saved password so it cannot be mistaken for the target upstream password

### Requirement: Upstream recharge and final multipliers
The sidecar SHALL allow an administrator to set or clear one recharge rate per upstream, SHALL normalize both supported input conventions to positive finite `CNY / USD`, and SHALL derive final multipliers without mutating synchronized group multipliers.

#### Scenario: One CNY buys multiple USD
- **WHEN** an administrator configures `1 CNY = 5 USD`
- **THEN** the sidecar SHALL store the canonical recharge rate as `0.2 CNY/USD`

#### Scenario: Multiple CNY buy one USD
- **WHEN** an administrator configures `5 CNY = 1 USD`
- **THEN** the sidecar SHALL store the canonical recharge rate as `5 CNY/USD`

#### Scenario: Final multiplier is available
- **WHEN** the canonical recharge rate is `0.2 CNY/USD` and a synchronized key's group multiplier is `0.8`
- **THEN** the public key view and administrator interface SHALL show the final multiplier as `0.16`

#### Scenario: An operand is unknown
- **WHEN** either the recharge rate or key group multiplier is unavailable
- **THEN** the final multiplier SHALL remain unknown and the interface SHALL identify which value still needs configuration or synchronization

#### Scenario: Recharge rate is cleared
- **WHEN** an administrator clears the upstream recharge-rate setting
- **THEN** the persisted canonical and input values SHALL be removed while synchronized key group multipliers remain unchanged

#### Scenario: Groups and accounts shows the bound final multiplier
- **WHEN** a current local API-key account is explicitly bound to one non-stale synchronized upstream Key with a configured recharge rate and group multiplier
- **THEN** the groups-and-accounts overview SHALL show that Key's calculated final multiplier and operands without requesting an upstream synchronization

#### Scenario: Groups and accounts never falls back to the probe multiplier
- **WHEN** an API-key account has no current explicit upstream-Key binding, has a stale or mismatched binding, has multiple active bindings, or lacks either final-multiplier operand
- **THEN** the groups-and-accounts overview SHALL show an actionable uncalculated reason and SHALL NOT display the independent Sub2API billing-probe multiplier

### Requirement: Manual local account binding
The sidecar SHALL allow an administrator to batch bind synchronized remote keys to current local API-key accounts associated with the same normalized upstream.

#### Scenario: Valid bindings are saved
- **WHEN** every submitted local account exists, is API-key based, and belongs to the upstream candidate set
- **THEN** the sidecar SHALL atomically save the binding set using stable identity and remote-key IDs

#### Scenario: Invalid local account is submitted
- **WHEN** a binding references a missing, non-API-key, or differently normalized local account
- **THEN** the sidecar SHALL reject the invalid binding without exposing any key material

#### Scenario: Bound remote key disappears
- **WHEN** a later synchronization no longer returns a previously bound remote key
- **THEN** the sidecar SHALL preserve and visibly mark the stale binding until an administrator clears it

### Requirement: Explicit step-up automatic matching
The sidecar SHALL perform automatic exact-key matching only after a distinct administrator action and SHALL use the current browser administrator JWT to call the existing step-up-protected Sub2API account export endpoint.

#### Scenario: Step-up authorization is required
- **WHEN** Sub2API rejects the forwarded export request with a step-up code
- **THEN** the sidecar SHALL return that actionable requirement and SHALL NOT retry with the machine Admin API key

#### Scenario: Unique exact keys match
- **WHEN** both a local exported key and a remote full key are available and their derived HMAC fingerprints match uniquely
- **THEN** the sidecar SHALL save the binding and SHALL discard both plaintext keys after the in-memory comparison

#### Scenario: Key is masked or match is ambiguous
- **WHEN** a remote key is not available in full or a fingerprint maps to more than one candidate
- **THEN** automatic matching SHALL leave it unbound and SHALL report `unavailable` or `ambiguous` without using prefix or suffix matching

### Requirement: Isolated refresh scheduling and resilient snapshots
Upstream synchronization SHALL be independent of health-probe scheduling and the group overview poll, SHALL support manual refresh, and SHALL optionally refresh connected identities on a configurable low-frequency interval.

#### Scenario: User has not opened the upstream tab
- **WHEN** the scheduler page loads and remains on `Groups & Accounts`
- **THEN** the browser SHALL make no upstream-list or upstream-synchronization request

#### Scenario: Upstream tab remains visible
- **WHEN** the administrator has opened the upstream tab and the page remains visible
- **THEN** the browser SHALL periodically reload the sidecar upstream list so local account additions, edits, moves, and removals are reflected without triggering upstream-origin synchronization

#### Scenario: Administrator returns to the upstream tab
- **WHEN** the upstream workspace was previously loaded and the administrator selects it again
- **THEN** the browser SHALL request a fresh authoritative upstream list instead of reusing the old in-memory list

#### Scenario: One identity fails during refresh
- **WHEN** an upstream refresh contains multiple identities and one request fails
- **THEN** the failure SHALL update only that identity's attempt state and SHALL preserve every last successful key snapshot

#### Scenario: Periodic synchronization is disabled
- **WHEN** `AUTO_SCHEDULER_UPSTREAM_SYNC_SECONDS` is zero
- **THEN** no periodic upstream synchronization SHALL run while manual synchronization remains available

### Requirement: Operational upstream user interface
The upstream view SHALL follow the existing console's light/dark visual tokens and SHALL provide keyboard-accessible tabs, expandable upstream rows, identity status, key metadata, connection controls, loading, empty, and error states without relying on color or hover alone.

#### Scenario: Upstream row is expanded
- **WHEN** an administrator activates an upstream disclosure control by pointer or keyboard
- **THEN** the page SHALL show associated local accounts, login identities, synchronized keys, binding state, quota/use, group, multiplier source, and synchronization timestamps

#### Scenario: Connection dialog changes authentication method
- **WHEN** an administrator selects password, token, or cookie/session mode
- **THEN** the dialog SHALL reveal only relevant visibly labeled fields, keep a clear cancel route, and restore focus to its trigger when closed

#### Scenario: Narrow viewport renders key details
- **WHEN** the viewport is 375 CSS pixels wide
- **THEN** key information and controls SHALL reflow without horizontal page scrolling, text overlap, or inaccessible controls

#### Scenario: Reduced motion is requested
- **WHEN** the browser reports `prefers-reduced-motion: reduce`
- **THEN** nonessential tab, disclosure, dialog, and status transitions SHALL be disabled
