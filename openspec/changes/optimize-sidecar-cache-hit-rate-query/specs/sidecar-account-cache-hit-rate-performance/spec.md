## ADDED Requirements

### Requirement: Sidecar bulk-loads today's cache statistics
When its existing read-only aggregate database connection is configured, the sidecar SHALL obtain today's cache statistics for all requested overview account IDs through one bounded database aggregation rather than one main-service statistics request per account.

#### Scenario: Overview has multiple accounts
- **WHEN** an administrator loads an overview containing multiple account IDs and the database connection is configured
- **THEN** the sidecar issues one grouped cache-stat query for the normalized ID set and attaches an individual cache projection to each requested account's today-usage data

#### Scenario: Account has no usage today
- **WHEN** a requested account has no matching current-day usage rows
- **THEN** the sidecar returns a present zero-valued cache projection with a 0.0 percent hit rate for that account

### Requirement: Sidecar preserves cache-hit semantics and response compatibility
The sidecar SHALL calculate the cache-hit rate as cache-read tokens divided by input, cache-creation, and cache-read tokens, and SHALL retain the existing nested `today_usage.cache` response shape.

#### Scenario: Account has prompt token data
- **WHEN** the grouped query returns input, cache-creation, or cache-read tokens for an account
- **THEN** the sidecar exposes their non-negative sums, their prompt-token denominator, and the calculated percentage in that account's existing cache projection

### Requirement: Bulk cache statistics are resilient and bounded
The sidecar MUST cache a successful bulk result for a short period, share concurrent refreshes for the same account-ID set, and preserve the base overview when the cache-stat query is unavailable.

#### Scenario: Same overview is refreshed within the cache lifetime
- **WHEN** the normalized account-ID set is unchanged and its bulk result remains fresh
- **THEN** the sidecar reuses the stored projection without issuing a new database query

#### Scenario: Configured database query fails
- **WHEN** the direct database connection is configured but the bulk cache-stat query fails
- **THEN** the sidecar uses the latest successful projection for that ID set when available, otherwise leaves cache statistics unavailable, and does not issue per-account main-service statistics requests

#### Scenario: Direct database is not configured
- **WHEN** the optional direct database connection is not configured
- **THEN** the sidecar retains the existing bounded HTTP cache-enrichment behavior as a compatibility fallback

### Requirement: Sub2API production service remains unchanged
The cache-stat performance improvement SHALL be implemented entirely in the sidecar and SHALL not require a Sub2API code, route, schema, configuration, image, or container change.

#### Scenario: HC2 release
- **WHEN** the optimized sidecar is deployed to HC2
- **THEN** only the `account-auto-scheduler` container image is replaced and the running `sub2api-canary` container remains unchanged
