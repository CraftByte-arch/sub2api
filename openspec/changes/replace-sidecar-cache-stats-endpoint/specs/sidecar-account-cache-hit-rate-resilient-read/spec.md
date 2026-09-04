## ADDED Requirements

### Requirement: Sidecar uses the indexed usage-statistics source for cache-hit enrichment

The sidecar SHALL obtain a current-day cache-hit projection for an active account through the existing Sub2API administrator usage-statistics endpoint scoped to that account. It MUST NOT call the per-account account-statistics endpoint for this projection.

#### Scenario: Active account has daily usage
- **WHEN** an account has at least one request in the sidecar's current-day batch usage result and no fresh sidecar cache projection exists
- **THEN** the sidecar requests the usage-statistics endpoint for that account's current-day usage and attaches the decoded projection to the account result

#### Scenario: Idle account has no daily usage
- **WHEN** an account has no request in the sidecar's current-day batch usage result
- **THEN** the sidecar exposes a known-zero cache projection without issuing a per-account usage-statistics request

### Requirement: Sidecar matches the main usage-record cache-hit formula

The sidecar SHALL calculate cache hit rate as cache-read tokens divided by the sum of input tokens and cache-read tokens. A zero denominator SHALL produce a known zero-percent projection.

#### Scenario: Usage statistics include cache-read tokens
- **WHEN** the usage-statistics response reports positive input or cache-read token totals
- **THEN** the sidecar reports the cache-hit rate using `cache_read / (input + cache_read)`

#### Scenario: Usage statistics report no prompt tokens
- **WHEN** the usage-statistics response reports zero input and zero cache-read tokens
- **THEN** the sidecar reports a known cache-hit rate of 0 percent

### Requirement: Cache-hit enrichment is bounded and failure-resilient

The sidecar SHALL cache successful per-account projections for five minutes, coalesce concurrent reads for the same account, and apply a bounded retry backoff after a failed read. It MUST preserve base account usage output if the optional enrichment is unavailable.

#### Scenario: A successful projection is still fresh
- **WHEN** an account's cache projection was successfully read within the five-minute cache lifetime
- **THEN** the sidecar reuses it without another usage-statistics request

#### Scenario: Concurrent overview reads need the same account projection
- **WHEN** two sidecar requests need an uncached projection for the same account at the same time
- **THEN** the sidecar performs at most one usage-statistics request for that account and shares its result

#### Scenario: Usage-statistics request fails
- **WHEN** a cache-hit enrichment request fails
- **THEN** the sidecar keeps the base usage output available and does not repeat that account's request until the retry-backoff period expires
