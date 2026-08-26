## ADDED Requirements

### Requirement: Administrator can manually control API Key account scheduling
The sidecar SHALL provide an administrator-authenticated operation that sets the global schedulable state of an API Key account through the existing Sub2API management API.

#### Scenario: Administrator stops a schedulable API Key account
- **WHEN** an authenticated administrator sets an active API Key account to not schedulable
- **THEN** the sidecar SHALL disable that account's global scheduling eligibility and return the updated account state

#### Scenario: Administrator enables an active API Key account
- **WHEN** an authenticated administrator sets an active API Key account to schedulable
- **THEN** the sidecar SHALL enable that account's global scheduling eligibility and return the updated account state

#### Scenario: Operation targets an OAuth account
- **WHEN** an administrator attempts to change scheduling eligibility for a non-API-Key account
- **THEN** the sidecar SHALL reject the operation without changing the account

#### Scenario: Administrator enables a non-active account
- **WHEN** an administrator attempts to enable scheduling for an API Key account whose status is not active
- **THEN** the sidecar SHALL reject the operation without changing the account

#### Scenario: Unauthenticated request attempts scheduling control
- **WHEN** a request without a valid administrator session calls the manual scheduling endpoint
- **THEN** the sidecar SHALL reject the request before changing the account

### Requirement: Manual scheduling control and automatic detection preserve ownership
The sidecar SHALL distinguish an administrator's manual scheduling decision from a suspension owned by the automatic detection state machine.

#### Scenario: Administrator manually stops an account with detection enabled
- **WHEN** an administrator manually stops an account that has an enabled detection policy
- **THEN** the sidecar SHALL continue scheduled detection while clearing automatic-suspension ownership so healthy checks do not automatically restore the account

#### Scenario: Administrator restores an automatically suspended account
- **WHEN** an administrator manually enables an account currently marked as automatically suspended
- **THEN** the sidecar SHALL clear automatic-suspension ownership and failure/success streaks while retaining the enabled detection policy and history

#### Scenario: Account has no detection configuration
- **WHEN** an administrator manually stops or enables an API Key account without a sidecar detection configuration
- **THEN** the sidecar SHALL update the Sub2API scheduling state without creating a detection configuration

#### Scenario: Upstream scheduling update fails
- **WHEN** Sub2API rejects or fails the scheduling update
- **THEN** the sidecar SHALL report the failure and SHALL NOT mutate its stored detection snapshot

### Requirement: Manual scheduling updates are serialized with account checks
The sidecar SHALL prevent a manual scheduling update and a detection run for the same account from executing concurrently.

#### Scenario: Detection is already running
- **WHEN** an administrator requests a scheduling update while the same account is being checked
- **THEN** the sidecar SHALL return a conflict response and leave the scheduling state unchanged

#### Scenario: Manual scheduling update starts first
- **WHEN** a detection trigger occurs while a manual scheduling update owns the account operation lock
- **THEN** the detection trigger SHALL be rejected or deferred until the manual operation has finished

### Requirement: Group account UI exposes the global scheduling state
The sidecar's group account list SHALL display a separate global scheduling control for each API Key account and SHALL distinguish it from automatic detection control.

#### Scenario: Administrator stops an account from a group row
- **WHEN** an administrator turns off the global scheduling switch
- **THEN** the UI SHALL require confirmation that the action affects the account in all groups before sending the update

#### Scenario: Administrator restores an automatically suspended account
- **WHEN** an administrator turns on the global scheduling switch for an automatically suspended account
- **THEN** the UI SHALL require confirmation that automatic detection remains enabled and may suspend the account again

#### Scenario: Update is in progress
- **WHEN** a global scheduling update is pending
- **THEN** every rendered row for that account SHALL show a saving state and prevent duplicate updates

#### Scenario: Account check is in progress
- **WHEN** an account's detection check is running
- **THEN** the global scheduling switch SHALL be disabled with an explanation that the update can be retried after the check

#### Scenario: Non-active account is displayed
- **WHEN** an API Key account is not active
- **THEN** the UI SHALL prevent enabling scheduling while still allowing an already schedulable account to be stopped
