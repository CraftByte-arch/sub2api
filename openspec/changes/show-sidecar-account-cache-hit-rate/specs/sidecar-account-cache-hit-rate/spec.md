## ADDED Requirements

### Requirement: Sidecar calculates today's account cache hit rate
The sidecar SHALL calculate an account's daily cache hit rate as cache-read tokens divided by the sum of input, cache-creation, and cache-read tokens returned by the existing Sub2API per-account statistics API.

#### Scenario: Account has cache reads
- **WHEN** today's model statistics contain input, cache-creation, or cache-read tokens
- **THEN** the sidecar exposes their sums, the prompt-token denominator, and the calculated percentage in the account's today-usage view

#### Scenario: Account has a known zero denominator
- **WHEN** the statistics request succeeds but all prompt-token components are zero
- **THEN** the sidecar exposes a known cache hit rate of 0.0 percent

### Requirement: Cache enrichment is best-effort
The sidecar MUST preserve the existing account overview when one or more cache-stat enrichment requests fail.

#### Scenario: One account statistics request fails
- **WHEN** the batch today-usage request succeeds and a per-account statistics request fails
- **THEN** the overview still returns the account's requests, total tokens, and cost while marking its cache metric unavailable

### Requirement: Cache enrichment load is bounded
The sidecar MUST bound concurrent per-account statistics requests and SHALL reuse successful observations for a short time during overview polling.

#### Scenario: Overview is refreshed within the cache lifetime
- **WHEN** a successful account cache observation is still fresh
- **THEN** the sidecar reuses the observation without issuing another per-account statistics request

### Requirement: Administrator UI displays the metric compactly
The sidecar administrator UI SHALL display today's cache hit rate in the existing usage summary without introducing a separate card or account-row column.

#### Scenario: Cache statistics are available
- **WHEN** an account includes a cache projection
- **THEN** the usage summary displays `缓存命中 N%` and exposes the cache-read and prompt-token values as explanatory text

#### Scenario: Cache statistics are unavailable
- **WHEN** an account does not include a cache projection
- **THEN** the usage summary displays `缓存命中 —` without showing a misleading zero
