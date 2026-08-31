## ADDED Requirements

### Requirement: Aggregation reads preserve exact existing behavior
The system SHALL preserve all existing API fields, filtering semantics, timezone boundaries, pagination behavior, export behavior, and exact statistics while changing the aggregation maintenance strategy.

#### Scenario: Query spans closed history and the current open bucket
- **WHEN** a supported statistics query spans one or more closed buckets and the current open bucket
- **THEN** the system SHALL combine aggregate rows for safe closed buckets with raw `usage_logs` rows for the open tail and return the same result as the legacy raw query

#### Scenario: A request completes after a date boundary
- **WHEN** a request starts before midnight but its usage log is created after midnight on completion
- **THEN** the system SHALL assign the complete usage record to the completion date and include it through the raw open tail without splitting it across dates

#### Scenario: Coverage cannot be proven
- **WHEN** readiness, coverage, bucket alignment, or state metadata cannot prove that the aggregate path is equivalent
- **THEN** the system SHALL execute the existing legacy query

### Requirement: Normal usage writes avoid synchronous aggregate calculation
After a statistic is switched to closed-bucket maintenance, the system SHALL NOT expand dimensions or upsert its aggregate metric rows in the normal `usage_logs` INSERT transaction.

#### Scenario: Current-bucket usage is inserted
- **WHEN** a normal usage record is inserted into the current open bucket
- **THEN** the write transaction SHALL NOT update that statistic's aggregate metric table and SHALL NOT enqueue the open bucket as dirty

#### Scenario: Unrelated main aggregation remains installed
- **WHEN** a custom statistic is switched off the write path
- **THEN** triggers and tables supplied by upstream main, including group usage rollups, SHALL remain unchanged

### Requirement: Historical changes remain immediately exact
The system SHALL record affected closed buckets for historical INSERT, UPDATE, and DELETE statements and SHALL treat those dirty buckets as raw-data ranges until repair commits.

#### Scenario: Historical usage is inserted
- **WHEN** a usage record is inserted into a bucket earlier than the statistic's `closed_before` watermark
- **THEN** the same transaction SHALL mark that bucket dirty and subsequent reads SHALL use `usage_logs` for that bucket

#### Scenario: Historical usage moves between buckets
- **WHEN** an UPDATE changes fields or timestamps that affect one or more aggregate buckets
- **THEN** the system SHALL mark every affected old and new closed bucket dirty

#### Scenario: Dirty repair races with a read
- **WHEN** a background repair commits while a statistics query is executing
- **THEN** the query SHALL observe either the dirty raw bucket or the repaired aggregate bucket from a consistent database snapshot, without omission or double counting

### Requirement: Closed-bucket maintenance is bounded and serialized
The system SHALL rebuild only closed buckets under bounded, non-parallel background work and SHALL serialize custom usage aggregation scans across application instances and statistic types.

#### Scenario: A daily bucket becomes closed
- **WHEN** the daily watermark is behind the current application date
- **THEN** one maintenance cycle SHALL rebuild and verify at most one next daily bucket before advancing the watermark

#### Scenario: An hourly bucket becomes closed
- **WHEN** an hourly watermark is behind the current application hour
- **THEN** one maintenance cycle SHALL rebuild and verify no more than two next hourly buckets

#### Scenario: Another maintainer owns the lock
- **WHEN** another application instance or custom statistic already owns the shared maintenance lock
- **THEN** the current worker SHALL skip the cycle without starting a concurrent source-table scan

#### Scenario: Maintenance SQL executes
- **WHEN** a maintenance transaction rebuilds or verifies a bucket
- **THEN** it SHALL set `max_parallel_workers_per_gather` to zero, apply a bounded statement timeout, and retain the bucket for retry on failure

### Requirement: Write-path removal uses staged compatibility
The system SHALL deploy hybrid-read compatibility before removing any synchronous custom aggregation write path, and each statistic SHALL be switched independently with forward-only migrations.

#### Scenario: First-stage Account deployment
- **WHEN** the Account hybrid-read version is first deployed
- **THEN** the existing Account synchronous triggers SHALL remain installed while hybrid-read correctness is verified

#### Scenario: Second-stage Account deployment
- **WHEN** HC2 verification has passed and the Account write-path removal migration is deployed
- **THEN** only the Account custom delta triggers SHALL be replaced by dirty-bucket marking and the other three custom statistics SHALL retain their current maintenance behavior

#### Scenario: Application rollback after write-path removal
- **WHEN** an application rollback is required after synchronous Account aggregation has been removed
- **THEN** operators SHALL roll back only to a version that supports aggregate history plus raw-tail reads

### Requirement: Upstream merge conflict surface stays minimal
The implementation SHALL keep aggregation-specific lifecycle and query logic in independent files and SHALL avoid adding statistic-specific methods to the shared Service repository interface.

#### Scenario: Production repository is constructed
- **WHEN** the production usage-log repository is created
- **THEN** a single composition point SHALL wrap the unchanged base repository with independent aggregation Stores

#### Scenario: Account statistics are requested
- **WHEN** Account Service asks for usage statistics
- **THEN** it SHALL continue calling the original `GetAccountUsageStats` interface method while the decorator selects the optimized implementation transparently
