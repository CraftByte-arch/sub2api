## ADDED Requirements

### Requirement: Bark destination SHALL be administrator-configurable and encrypted at rest

The scheduler SHALL allow an authenticated administrator to configure one Bark endpoint, device key, 16-byte AES encryption key, and optional Bark Basic Auth credentials. Secret fields SHALL be encrypted with the deployment credential key before persistence, and API responses SHALL never return their plaintext values.

#### Scenario: Save a valid encrypted Bark configuration

- **WHEN** an administrator submits a valid HTTPS Bark endpoint, device key, 16-byte encryption key, and optional Basic Auth credentials while `AUTO_SCHEDULER_CREDENTIAL_KEY` is configured
- **THEN** the scheduler persists an encrypted secret envelope, returns only configuration metadata, and marks the destination as configured

#### Scenario: Reject notification configuration without the deployment key

- **WHEN** an administrator attempts to save or test Bark notifications while credential encryption is disabled
- **THEN** the scheduler rejects the operation with an actionable configuration error and leaves existing scheduling behavior unchanged

#### Scenario: Test delivery uses Bark encrypted payloads

- **WHEN** an administrator requests a test notification for a configured destination
- **THEN** the scheduler sends a Bark request containing AES-128-CBC `ciphertext` and `iv`, uses HTTPS and configured Basic Auth, and reports success or a bounded delivery error without exposing secrets

### Requirement: Account and group balance thresholds SHALL use account precedence

The scheduler SHALL allow an optional available-balance threshold on an account and an optional default threshold on a group. For a group-account relation, the account threshold SHALL take precedence over the group threshold; if neither is configured, no balance alert SHALL be evaluated.

#### Scenario: Account threshold overrides group threshold

- **WHEN** a group threshold is 50 and one account in that group has an account threshold of 10
- **THEN** that account is evaluated against 10 while other accounts without an account threshold are evaluated against 50

#### Scenario: Unset thresholds disable balance alerts

- **WHEN** neither the account nor its group has a threshold
- **THEN** the scheduler SHALL not create or send a balance alert for that relation

### Requirement: Balance alerts SHALL be edge-triggered and use projected available balance

The scheduler SHALL calculate an API-key account's available balance from the existing upstream balance projection (upstream balance divided by the observed group multiplier), persist whether each threshold relation is currently below threshold, and send at most one alert while it remains below. A recovery alert SHALL clear the active edge so a later transition below can alert again.

#### Scenario: Entering below-threshold state sends one alert

- **WHEN** a configured relation changes from balance at-or-above its effective threshold to balance below it
- **THEN** the scheduler sends one message containing group, account, current available balance, and threshold, and persists the below-threshold state

#### Scenario: Repeated low observations do not resend

- **WHEN** later polling cycles still observe the same relation below its effective threshold
- **THEN** the scheduler SHALL not send another balance alert

#### Scenario: Replenishment enables a future alert

- **WHEN** a relation previously below threshold becomes at-or-above threshold and later falls below again
- **THEN** the scheduler sends one recovery message at the safe transition and one new low-balance message at the later falling transition

#### Scenario: Unobservable balance does not create a false alert

- **WHEN** the bound upstream balance is unavailable, stale, unsupported, or its multiplier is unknown
- **THEN** the scheduler SHALL not classify the relation as below threshold

### Requirement: Group zero-capacity alerts SHALL recover only above one usable account

The scheduler SHALL count physically bound API-key accounts that are active and schedulable for each group. It SHALL send one alert when the count enters zero, remain quiet while the count is zero or one, and send one recovery message only when the count becomes greater than one.

#### Scenario: Group becomes empty

- **WHEN** a group's usable API-key count changes from nonzero to zero
- **THEN** the scheduler sends one group capacity alert and persists the zero-capacity state

#### Scenario: One account is not sufficient for recovery

- **WHEN** a zero-capacity group later has exactly one usable account
- **THEN** the scheduler SHALL not send a recovery message and SHALL keep the capacity alert active

#### Scenario: Group recovers above one

- **WHEN** a zero-capacity group later has more than one usable account
- **THEN** the scheduler sends one recovery message and clears the active capacity alert

### Requirement: Multiplier changes SHALL be deduplicated and include protection state

The scheduler SHALL persist the last observed final multiplier for each bound group-account relation. The first observation SHALL initialize silently; later changes SHALL send one message containing old value, new value, group, account, and whether the current relation triggered multiplier protection.

#### Scenario: Initial multiplier observation is silent

- **WHEN** a relation has no persisted multiplier observation and the evaluator sees its first valid final multiplier
- **THEN** the scheduler stores the value without sending a multiplier-change message

#### Scenario: Changed final multiplier sends one message

- **WHEN** the persisted final multiplier changes from one finite value to another
- **THEN** the scheduler sends one multiplier-change message containing both values and the current protection-triggered status

#### Scenario: Unavailable multiplier is ignored

- **WHEN** a relation has no finite final multiplier in the current projection
- **THEN** the scheduler SHALL not send a multiplier-change message or overwrite the last valid observation

### Requirement: Notification delivery failures SHALL not affect scheduling

The scheduler SHALL isolate Bark HTTP failures, timeout, malformed configuration, and encryption errors from account probing, upstream synchronization, and multiplier protection. It SHALL log a bounded diagnostic and expose the latest delivery error through the administrator notification status API.

#### Scenario: Bark is unavailable during a transition

- **WHEN** a transition is detected but the Bark request fails
- **THEN** the scheduler records the delivery error, keeps the scheduler loops running, and does not retry the same active transition on every poll

#### Scenario: State survives restart

- **WHEN** the scheduler restarts after recording an active low-balance or zero-capacity condition
- **THEN** the same condition SHALL not produce a duplicate alert until its recovery transition occurs
