## ADDED Requirements

### Requirement: Identity-scoped authenticated balance collection
The sidecar SHALL collect the actual authenticated account balance for each connected Sub2API or NewAPI upstream identity during connection and synchronization, and SHALL associate the result with that identity rather than the upstream or any individual API key.

#### Scenario: Sub2API identity is synchronized
- **WHEN** a connected Sub2API identity returns a finite balance from its authenticated profile endpoint
- **THEN** the sidecar SHALL persist that value as the identity's USD account balance with the observation time

#### Scenario: Upstream has multiple identities
- **WHEN** two or more identities are connected to the same upstream
- **THEN** the sidecar SHALL maintain and expose a separate balance snapshot for each identity and SHALL NOT merge them into one spendable total

### Requirement: Conservative NewAPI quota normalization
The sidecar SHALL interpret NewAPI user quota using conversion data published by the same upstream origin. It SHALL convert quota to USD only when `quota_per_unit` is positive and finite, and otherwise SHALL retain and label the value as raw upstream quota.

#### Scenario: NewAPI publishes a valid quota unit
- **WHEN** `/api/user/self` returns quota `2500000` and the same upstream's public status returns `quota_per_unit` `500000`
- **THEN** the sidecar SHALL expose an identity balance of `5 USD` and retain the conversion metadata

#### Scenario: NewAPI conversion is unavailable
- **WHEN** NewAPI returns a finite user quota but omits or returns an invalid `quota_per_unit`
- **THEN** the sidecar SHALL expose the finite value with unit `quota`, SHALL label it as raw upstream quota, and SHALL NOT guess a monetary amount

### Requirement: Last-successful balance freshness
The sidecar SHALL retain the last successfully observed identity balance when a later synchronization fails and SHALL expose that snapshot as stale together with its original observation time.

#### Scenario: Balance refresh fails after a prior success
- **WHEN** an identity has a stored balance and a later authenticated synchronization fails
- **THEN** the sidecar SHALL preserve the amount and observation time, mark the snapshot stale, and update only that identity's synchronization error state

#### Scenario: Balance refresh succeeds again
- **WHEN** a stale identity later synchronizes successfully with a supported balance value
- **THEN** the sidecar SHALL replace the stored snapshot and clear its stale state

### Requirement: Administrator-only redacted balance views
The existing administrator-protected upstream list SHALL expose only non-secret balance metadata: amount, unit/currency, source, observation time, freshness, and bounded conversion metadata. Balance retrieval SHALL remain server-side and SHALL reuse the existing encrypted upstream credential boundary.

#### Scenario: Administrator lists upstreams
- **WHEN** a validated Sub2API administrator opens the Upstreams tab
- **THEN** the response SHALL include each identity's public balance snapshot without credentials, cookies, tokens, ciphertext, raw headers, or upstream response bodies

#### Scenario: Non-administrator requests balances
- **WHEN** a caller without a current validated administrator JWT requests the upstream list or synchronization route
- **THEN** the sidecar SHALL reject the caller before making a credentialed upstream request

### Requirement: Operational upstream balance presentation
The Upstreams tab SHALL show each identity's balance value or explicit unavailable state, observation time, and stale state. The upstream header SHALL summarize availability without summing multiple identity balances, and existing synchronization actions SHALL refresh keys and balances together.

#### Scenario: One identity has a current balance
- **WHEN** an upstream contains exactly one identity with a current balance snapshot
- **THEN** its upstream header and expanded identity row SHALL show that balance with its unit and updated time

#### Scenario: Multiple identities have balances
- **WHEN** an upstream contains multiple identities with balance snapshots
- **THEN** the upstream header SHALL show the number of account balances and the expanded view SHALL show each identity separately without a combined amount

#### Scenario: Last balance is stale
- **WHEN** an identity's latest synchronization failed after a previous balance success
- **THEN** the interface SHALL show the last value, its observation time, and a visible stale label without relying on color alone

#### Scenario: Balance is unavailable
- **WHEN** no supported balance has been observed for an identity
- **THEN** the interface SHALL show `暂不可获取` or an equivalent explicit state and SHALL NOT display zero

#### Scenario: Narrow viewport displays balances
- **WHEN** the upstream view renders at 375 CSS pixels wide
- **THEN** balance values, status text, timestamps, and synchronization controls SHALL reflow without overlap or horizontal page scrolling

### Requirement: Compatible state and isolated deployment
Optional balance fields SHALL remain compatible with existing state version 3 data, and deployment SHALL rebuild or restart only the scheduler sidecar.

#### Scenario: Existing state has no balance fields
- **WHEN** the upgraded sidecar loads a valid v3 state created before upstream balances existed
- **THEN** it SHALL preserve all upstream identities, credentials, keys, bindings, and scheduler history while initializing balance snapshots as unavailable

#### Scenario: Sidecar-only release
- **WHEN** upstream balance support is deployed to hc2
- **THEN** the deployment SHALL update only `account-auto-scheduler` and verify that Sub2API, database, proxy, and Nginx UI containers were not restarted

### Requirement: Bound account quota follows fresh upstream balance
The sidecar SHALL project a fresh USD identity balance onto each explicitly bound local API-key account using the bound remote key's group multiplier, and SHALL use Sub2API's existing administrator Extra merge API without changing account billing multipliers.

#### Scenario: Fresh balance and positive group multiplier
- **WHEN** an identity has a fresh balance of `100 USD`, a bound remote key has group multiplier `0.5`, and the local account has `quota_used = 10`
- **THEN** the sidecar SHALL set that local account's `quota_limit` to `200` and reset `quota_used` to `0` so the displayed remaining quota is `200`

#### Scenario: Same balance observation is reconciled repeatedly
- **WHEN** a previously applied balance observation is encountered again after local usage has increased
- **THEN** the sidecar SHALL NOT reset `quota_used` again until a newer authoritative balance observation is available

#### Scenario: Multiple local accounts use one site balance
- **WHEN** multiple local accounts are bound to keys under the same upstream identity
- **THEN** each account SHALL independently receive the full identity balance divided by its own bound key group multiplier, without splitting the balance among accounts

#### Scenario: Authoritative zero balance
- **WHEN** the fresh normalized identity balance is zero
- **THEN** the resulting local account quota SHALL be immediately exhausted, including when its previous `quota_used` was zero, so native Sub2API scheduling excludes it

#### Scenario: Balance becomes positive again
- **WHEN** a previously exhausted managed account later receives a positive fresh upstream balance
- **THEN** the sidecar SHALL replace the exhaustion sentinel with the projected quota limit, reset `quota_used` to zero, and native Sub2API scheduling SHALL be able to include it again

#### Scenario: Group multiplier is zero
- **WHEN** a bound key has an exact group multiplier of zero
- **THEN** the sidecar SHALL represent the account quota as unlimited with `quota_limit = 0` and `quota_used = 0` because requests do not consume the upstream identity balance

#### Scenario: Projection input is not authoritative
- **WHEN** the identity balance is stale, unavailable, non-USD/raw quota, non-finite, or the bound key multiplier is missing, negative, non-finite, stale, or ambiguous
- **THEN** the sidecar SHALL NOT overwrite the account's existing quota limit from that observation

#### Scenario: Managed binding is explicitly removed or moved
- **WHEN** an account quota was managed by this projection and its current explicit binding is later removed or moved to another normalized upstream
- **THEN** the sidecar SHALL disable the managed quota limit without changing account billing multiplier, status, or administrator schedulable state
