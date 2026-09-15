## ADDED Requirements

### Requirement: Manual multi-model probing
The sidecar SHALL provide an administrator-only manual probe action for API Key accounts. The action SHALL accept multiple model IDs, a custom prompt, and a reasoning-effort setting, and SHALL execute each selected model using streaming direct-upstream probing.

#### Scenario: Probe multiple models
- **WHEN** an administrator selects two or more models and submits a valid prompt
- **THEN** the sidecar returns one independent result per model with success/failure, latency, response summary, usage when available, and sanitized error details

#### Scenario: Manual probe does not affect scheduling
- **WHEN** a manual probe succeeds or fails
- **THEN** the sidecar does not change the account schedulable state, automatic probe counters, automatic probe history, or protection status

#### Scenario: Probe an account without sidecar detection configuration
- **WHEN** an API Key account exists in Sub2API but has no managed-account configuration in the sidecar
- **THEN** an authenticated administrator can still run a manual probe without creating a managed-account configuration

#### Scenario: Invalid manual probe request
- **WHEN** no model is selected, too many models are selected, or prompt/effort values exceed validation limits
- **THEN** the sidecar rejects the request with a clear validation error before making upstream calls

### Requirement: Safe administrator interaction
The manual probe SHALL be available only to an authenticated administrator from both the account detail actions and the account-row overflow menu, SHALL never return account secrets, SHALL show per-model progress/results, and SHALL remain usable on narrow screens.

#### Scenario: Load the account's real configured models
- **WHEN** an administrator opens manual probe for an account
- **THEN** the sidecar loads models from Sub2API's existing account-model administrator endpoint and does not insert hard-coded model IDs

#### Scenario: Model loading fails
- **WHEN** the account-model request fails or returns no usable model IDs
- **THEN** the dialog shows an explicit error or empty state with a retry action while retaining the custom model fallback

### Requirement: Efficient exclusive-group member reads
The sidecar SHALL read exclusive-group membership through its configured read-only PostgreSQL connection using the exact source and target group IDs. The read SHALL not call the general administrator user-list endpoint or scan usage records. Permission mutations SHALL continue to use Sub2API's administrator HTTP API.

#### Scenario: View current or source-group users
- **WHEN** an administrator opens an exclusive group or selects a source group
- **THEN** the sidecar returns source-group users and their target authorization state from one bounded indexed database query

#### Scenario: Read-only database is unavailable
- **WHEN** the sidecar has no configured read-only database or the query fails
- **THEN** it returns a clear retryable failure without silently falling back to the expensive general user-list endpoint
