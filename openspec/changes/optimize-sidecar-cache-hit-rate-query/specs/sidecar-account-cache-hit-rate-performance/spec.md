## ADDED Requirements

### Requirement: Sidecar skips cache-detail reads for idle accounts

The sidecar SHALL derive a zero-valued cache projection for every requested account with no current-day requests, without calling an additional main-service statistics endpoint.

#### Scenario: Overview includes idle accounts

- **WHEN** the batch today-usage response reports zero or omits the request count for an account
- **THEN** the overview exposes a present zero-percent cache projection for that account and makes no per-account cache-detail request for it

### Requirement: Sidecar bounds active-account cache-detail reads

The sidecar SHALL use the existing per-account administrator cache-statistics endpoint only for accounts with current-day requests, with at most four concurrent calls and a five-minute successful-result cache.

#### Scenario: Overview has active accounts

- **WHEN** accounts have current-day requests
- **THEN** the sidecar fetches cache-token details only for those accounts and attaches them to the existing `today_usage.cache` response shape

#### Scenario: Overview refreshes within five minutes

- **WHEN** an active account's cache projection was loaded successfully during the last five minutes
- **THEN** the sidecar reuses that projection without another main-service cache-detail request

### Requirement: Base overview remains available on cache-detail failure

The sidecar SHALL preserve batch today usage if an active-account cache-detail request fails.

#### Scenario: An active-account detail request fails

- **WHEN** a cache-detail request returns an error or times out
- **THEN** the account's cache projection is unavailable while all base today-usage data remains available

### Requirement: Sub2API production service remains unchanged

The optimization SHALL be implemented entirely in the sidecar and SHALL not require a Sub2API code, route, schema, configuration, database-permission, image, or container change.

#### Scenario: HC2 release

- **WHEN** the optimized sidecar is deployed to HC2
- **THEN** only the `account-auto-scheduler` container image is replaced and `sub2api-canary` remains unchanged
