## ADDED Requirements

### Requirement: Group access visibility
The sidecar SHALL distinguish public and exclusive groups and load explicitly authorized users only when an administrator opens an exclusive group.

#### Scenario: Exact membership
- **WHEN** the upstream name filter also returns users of a similarly named group
- **THEN** only users whose allowed_groups includes the exact group ID are shown.

#### Scenario: Paged list interaction
- **WHEN** the administrator searches or changes pages
- **THEN** results are filtered locally, selection persists across pages, and already-authorized target users cannot be selected.

### Requirement: Additive synchronization
The sidecar SHALL support synchronizing all loaded eligible users or selected users from a different exclusive group, after confirmation, without removing permissions or copying balance, rates, subscriptions or API keys.

#### Scenario: Safe merge
- **WHEN** a selected user still has source permission and lacks target permission
- **THEN** their latest allowed_groups is merged with the target ID and no unrelated field is submitted.

#### Scenario: Membership changed
- **WHEN** a selected user loses source permission or already has target permission
- **THEN** the sidecar skips the user without writing.

#### Scenario: Partial failure
- **WHEN** some users fail or a write has an ambiguous outcome
- **THEN** individual results are shown, confirmed changes remain, unknown outcomes are not automatically retried, and pending batches stop when necessary.

### Requirement: Admin-only bounded HTTP integration
The sidecar MUST authenticate administrators, validate IDs and exclusive groups, bound paging, batches and execution time, and use only existing main-service HTTP APIs.

#### Scenario: Unauthorized request
- **WHEN** an unauthenticated or non-admin caller requests users or submits synchronization
- **THEN** the request is rejected before membership reads or writes.

#### Scenario: Invalid destination or incomplete source
- **WHEN** a group is public, missing, identical to source, or the source cannot be fully loaded
- **THEN** no synchronization is started and an actionable error is shown.
