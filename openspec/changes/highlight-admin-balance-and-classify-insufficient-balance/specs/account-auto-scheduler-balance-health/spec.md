## ADDED Requirements

### Requirement: Administrator balance projection
The sidecar SHALL expose a safe administrator-balance projection for each overview account without exposing the account's private `Extra` metadata.

#### Scenario: Managed upstream balance is available
- **WHEN** an API Key account has sidecar-managed upstream quota metadata with a finite remaining value
- **THEN** the overview SHALL mark the balance as managed and configured and SHALL return that remaining value

#### Scenario: Managed upstream balance is exhausted
- **WHEN** an API Key account has sidecar-managed upstream quota metadata marked exhausted
- **THEN** the overview SHALL mark the administrator balance as insufficient even if Sub2API represents exhaustion with a small positive sentinel quota

#### Scenario: Ordinary configured quota is exhausted
- **WHEN** an account has a finite positive administrator quota limit and its used value reaches or exceeds that limit
- **THEN** the overview SHALL mark the relevant quota dimension as exhausted and the administrator balance as insufficient

#### Scenario: Quota is not configured
- **WHEN** an account has no finite positive administrator quota limit and no managed upstream quota metadata
- **THEN** the overview SHALL not mark the account as insufficient

### Requirement: Prominent administrator balance display
The account list SHALL present administrator balance as a prominent, compact status block while retaining quota usage details.

#### Scenario: Finite balance remains
- **WHEN** the overview returns a finite remaining administrator balance
- **THEN** the account row SHALL display the remaining amount with stronger visual emphasis than the used/limit detail

#### Scenario: Administrator balance is insufficient
- **WHEN** the overview marks the administrator balance as insufficient
- **THEN** the account row SHALL display the literal text `余额不足` using the danger state and SHALL also identify the exhausted quota dimension in text

#### Scenario: Balance is unlimited or unconfigured
- **WHEN** the balance is explicitly unlimited or no administrator quota is configured
- **THEN** the account row SHALL display a non-danger explanatory state and SHALL not claim that the balance is insufficient

#### Scenario: Narrow viewport
- **WHEN** the account list is rendered at a 375px viewport width
- **THEN** the balance block and quota details SHALL wrap within the account row without horizontal overflow or hiding the textual state

### Requirement: Insufficient-balance probe classification
The sidecar SHALL classify a direct probe failure as `balance_insufficient` only when the upstream response is HTTP 403 and its sanitized error text contains recognized insufficient-balance or exhausted-quota semantics.

#### Scenario: Chinese insufficient quota response
- **WHEN** a direct upstream probe returns HTTP 403 with text containing `用户额度不足`, `额度不足`, `余额不足`, or `余额耗尽`
- **THEN** the resulting failed check SHALL have failure kind `balance_insufficient`

#### Scenario: English insufficient quota response
- **WHEN** a direct upstream probe returns HTTP 403 with text containing `insufficient balance`, `insufficient quota`, or `quota exhausted` without regard to letter case
- **THEN** the resulting failed check SHALL have failure kind `balance_insufficient`

#### Scenario: Unrelated forbidden response
- **WHEN** a direct upstream probe returns HTTP 403 without any recognized insufficient-balance phrase
- **THEN** the check SHALL remain a generic failure with no balance-insufficient failure kind

#### Scenario: Similar text from another HTTP status
- **WHEN** a direct upstream probe returns a status other than HTTP 403 even if its text mentions insufficient balance
- **THEN** the check SHALL not receive the balance-insufficient failure kind

### Requirement: Balance failure state and history
The sidecar SHALL persist and display the structured balance failure reason without discarding the original sanitized probe error.

#### Scenario: Latest check fails for insufficient balance
- **WHEN** a counted check has failure kind `balance_insufficient`
- **THEN** the managed account SHALL persist `last_failure_kind` as `balance_insufficient`, the account list SHALL show `余额不足导致检测失败`, and the original failure message SHALL remain available as the detailed reason and history text

#### Scenario: A later check is healthy
- **WHEN** a later counted check completes successfully
- **THEN** the managed account SHALL clear `last_failure_kind` and the account list SHALL no longer show the balance-failure state

#### Scenario: State is reloaded
- **WHEN** the sidecar restarts after persisting a balance-insufficient result
- **THEN** the result failure kind and latest failure kind SHALL remain available from the version-4 state file

### Requirement: Existing scheduler thresholds remain authoritative
An insufficient-balance failure SHALL participate in the same failure and recovery state machine as other counted unhealthy checks.

#### Scenario: Failure threshold is reached
- **WHEN** consecutive insufficient-balance failures reach the configured failure threshold
- **THEN** the sidecar SHALL run the existing automatic suspension behavior without a balance-specific bypass

#### Scenario: Recovery threshold is reached
- **WHEN** a previously suspended account later produces enough consecutive healthy checks to reach the configured recovery threshold
- **THEN** the sidecar SHALL run the existing automatic recovery behavior and clear the balance failure classification
