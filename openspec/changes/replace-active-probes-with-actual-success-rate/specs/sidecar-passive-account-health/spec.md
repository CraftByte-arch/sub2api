## ADDED Requirements

### Requirement: Sidecar active probes are disabled
The sidecar SHALL NOT schedule, authorize, or manually execute synthetic account probes after passive account health is enabled.

#### Scenario: Sidecar starts with enabled stored policies
- **WHEN** the sidecar starts and stored accounts contain enabled probe policies or future check times
- **THEN** it SHALL persist those policies disabled, clear their future check times, and SHALL NOT start the active-probe due loop

#### Scenario: Administrator calls a legacy probe endpoint
- **WHEN** an authenticated administrator calls a probe configuration mutation, run-now, authorization, or revocation endpoint
- **THEN** the sidecar SHALL reject the operation as removed without sending a Sub2API account-test or direct-upstream request

#### Scenario: Administrator manually changes scheduling
- **WHEN** an authenticated administrator enables or stops an API-key account through the manual scheduling control
- **THEN** the sidecar SHALL preserve the existing manual scheduling behavior without invoking an account probe

### Requirement: Probe-owned suspensions are relinquished safely
The sidecar SHALL distinguish probe-owned scheduling suspension from administrator-owned scheduling state while entering passive mode.

#### Scenario: Probe state machine owns a suspension
- **WHEN** a stored account has `ManagedSuspended=true` during passive-mode transition
- **THEN** the sidecar SHALL attempt to restore that account's scheduling and clear probe ownership and transient streak counters

#### Scenario: Administrator stopped an account
- **WHEN** an account is unschedulable without `ManagedSuspended=true`
- **THEN** the passive-mode transition SHALL leave the administrator-owned stop unchanged

#### Scenario: One restoration fails
- **WHEN** restoring one probe-owned suspension fails
- **THEN** the sidecar SHALL log the account-specific failure, continue starting unrelated features, and leave no enabled probe policy capable of retrying automatically

### Requirement: Actual success rate uses real account attempts
The sidecar SHALL calculate group-account actual success rate from existing account-performance aggregates and SHALL NOT read raw usage logs.

#### Scenario: Successful and failed attempts exist
- **WHEN** a group-account pair has account-performance attempts in the selected window
- **THEN** its rate SHALL equal `success_count / (attempt_count - client_canceled_count)` and the response SHALL include the numerator and denominator

#### Scenario: Client cancellations exist
- **WHEN** the aggregate contains client-canceled attempts
- **THEN** those attempts SHALL be reported separately and excluded from the success-rate denominator

#### Scenario: One user request fails over between accounts
- **WHEN** one real request fails on one selected account and succeeds on another
- **THEN** the first account SHALL receive a failed attempt and the second account SHALL receive a successful attempt

#### Scenario: Synthetic detection activity occurs outside the gateway recorder
- **WHEN** historical sidecar checks, administrator account tests, balance synchronization, or upstream login operations exist
- **THEN** they SHALL NOT contribute to actual success-rate counts

### Requirement: Recent success reads are incrementally cached
The sidecar SHALL maintain a bounded, demand-driven rolling one-hour cache of minute aggregates.

#### Scenario: Cache is cold or older than the full window
- **WHEN** an authenticated overview request needs actual success data and no usable minute cache exists
- **THEN** the sidecar SHALL perform at most one bounded latest-hour bootstrap query shared by concurrent requests

#### Scenario: Cache has a recent boundary
- **WHEN** the two-minute cache TTL expires while the previous boundary remains within the rolling hour
- **THEN** the sidecar SHALL query only the missing tail plus an overlap, replace overlapping minute buckets, and prune expired buckets

#### Scenario: Administrator page is not requesting overview data
- **WHEN** no authenticated browser or caller requests the overview
- **THEN** the sidecar SHALL perform no background success-rate database polling

#### Scenario: Aggregate query fails
- **WHEN** a bounded aggregate query times out or fails
- **THEN** the sidecar SHALL retain a recent last-good snapshot with an explicit stale/partial state and SHALL NOT fall back to raw logs

### Requirement: A cached 24-hour reference is available
The sidecar SHALL provide a group-account reference rate from the latest 24 completed hourly buckets.

#### Scenario: Closed hourly data exists
- **WHEN** the hourly aggregate contains attempts during the latest 24 completed UTC hours
- **THEN** the sidecar SHALL return its success numerator, effective denominator, cancellation count, failover count, and exclusive completion boundary

#### Scenario: Current hour is incomplete
- **WHEN** the current UTC hour contains live traffic
- **THEN** the 24-hour reference SHALL exclude that incomplete hour and label its completed-hour data boundary

### Requirement: Account rows communicate rate quality and freshness
The administrator UI SHALL show actual success-rate information without relying on color alone.

#### Scenario: Sufficient one-hour samples exist
- **WHEN** a group-account pair has at least 20 effective attempts in the latest hour
- **THEN** the row SHALL display percentage, success/effective-attempt counts, and the one-hour window

#### Scenario: Sample size is low
- **WHEN** a group-account pair has between 1 and 19 effective attempts
- **THEN** the row SHALL display the measured percentage and counts together with a visible `样本较少` label

#### Scenario: No effective attempts exist
- **WHEN** a group-account pair has no effective attempts in the latest hour
- **THEN** the row SHALL display `暂无真实调用` rather than `0%`

#### Scenario: Data is stale or unavailable
- **WHEN** aggregate access is stale, degraded, unconfigured, or unavailable
- **THEN** the row SHALL display a textual freshness state and SHALL NOT present old or missing data as a current zero

#### Scenario: Administrator inspects the metric
- **WHEN** the administrator focuses or hovers the success-rate metric
- **THEN** the UI SHALL expose the 24-hour reference, sample counts, completed-hour boundary, cancellations, failovers, and any collection or freshness notice

### Requirement: Unrelated sidecar and Sub2API behavior remains unchanged
Passive account health SHALL be isolated from all unrelated runtime functions.

#### Scenario: Aggregate database configuration is missing
- **WHEN** the optional sidecar database URL or required table grants are absent
- **THEN** only actual-success display SHALL be unavailable while manual scheduling, upstream management, protection, balance, notification, and authentication features remain operational

#### Scenario: Sidecar is not deployed
- **WHEN** Sub2API is deployed without this sidecar
- **THEN** its billing, scheduling, gateway requests, account-performance collection, and administrator behavior SHALL remain unchanged
