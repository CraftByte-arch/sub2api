## ADDED Requirements

### Requirement: Durable hourly group usage aggregation
The system SHALL persist each newly inserted usage log with a non-null group ID into a PostgreSQL UTC-hour group usage bucket. Retried or idempotently rejected usage-log inserts MUST NOT add usage to a bucket.

#### Scenario: Newly inserted grouped usage log
- **WHEN** a usage log with a group ID is newly persisted
- **THEN** the corresponding UTC-hour bucket includes its actual cost exactly once

#### Scenario: Idempotent usage-log retry
- **WHEN** a usage-log insert is rejected as an existing request
- **THEN** no group usage bucket is incremented

### Requirement: Compatible group usage summary
The system SHALL retain the `GetAllGroupUsageSummary(ctx, todayStart)` contract and return total and today actual cost for every non-deleted group.

#### Scenario: Aggregate ready
- **WHEN** hourly aggregation backfill is marked ready
- **THEN** the summary reads historical and today cost from durable group-hour buckets without scanning all retained usage logs

#### Scenario: Aggregate initializing
- **WHEN** initial hourly aggregation backfill is incomplete or unverified
- **THEN** the summary uses the legacy raw-log calculation so that historical totals remain complete

#### Scenario: Soft-deleted group
- **WHEN** a group is soft-deleted
- **THEN** the summary does not return that group

### Requirement: Automatic resumable historical backfill
The system SHALL automatically backfill retained grouped usage history after deployment in bounded, resumable chunks coordinated through PostgreSQL.

#### Scenario: Worker interruption
- **WHEN** the backfill worker stops after completing one or more chunks
- **THEN** a later worker resumes from persisted progress without reprocessing completed progress as an additive update

#### Scenario: Multiple application replicas
- **WHEN** multiple application instances are running
- **THEN** at most one instance performs group usage backfill at a time

#### Scenario: Backfill verification
- **WHEN** a historical backfill chunk is complete
- **THEN** the system compares active-group aggregate values with the matching raw-log range before advancing the persistent cursor

### Requirement: Retention-consistent group history
The system SHALL remove group-hour buckets older than the raw usage-log retention cutoff.

#### Scenario: Usage-log retention cleanup
- **WHEN** retained usage logs older than the configured cutoff are removed
- **THEN** group-hour buckets older than that cutoff are removed in the same cleanup cycle
