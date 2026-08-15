## ADDED Requirements

### Requirement: Per-group-account protection intent
The sidecar SHALL persist an optional protection record for each `(group_id, account_id)` relationship, including the configured threshold and the upstream identity/key source needed to restore the relationship.

#### Scenario: Protection is configured for a bound account
- **WHEN** an administrator saves a finite non-negative protection multiplier for a physically bound API Key account in a group
- **THEN** the sidecar SHALL persist the logical relationship and its source tuple without changing the account billing multiplier or account schedulable state

#### Scenario: Protection is not configured
- **WHEN** an API Key account has no protection record for a group
- **THEN** the sidecar SHALL preserve the existing physical binding behavior and SHALL NOT create a shadow relationship during ordinary synchronization

### Requirement: Automatic protection state transitions
The sidecar SHALL compare the saved final multiplier with the protection multiplier and SHALL physically unbind the account when `final_multiplier > protection_multiplier`; it SHALL physically bind the account when a fresh, unambiguous final multiplier is `<= protection_multiplier`.

#### Scenario: Final multiplier exceeds protection
- **WHEN** a protected account's final multiplier is strictly greater than its configured protection multiplier
- **THEN** the sidecar SHALL remove the actual Sub2API group binding, retain the logical relationship, and expose the relationship as `超过保护倍率`

#### Scenario: Final multiplier returns within protection
- **WHEN** a protected logical member has a fresh final multiplier less than or equal to its protection multiplier
- **THEN** the sidecar SHALL restore the actual Sub2API group binding and clear the exceeded state while retaining the protection setting

#### Scenario: Multiplier is unavailable
- **WHEN** the saved source key is stale, ambiguous, missing, or has no valid final multiplier
- **THEN** the sidecar SHALL retain the last physical binding state, SHALL NOT guess a transition, and SHALL expose the unavailable reason

### Requirement: Protected members remain visible in administrator projections
The sidecar SHALL merge physical Sub2API memberships with persisted protection records in overview and group-binding responses.

#### Scenario: Protected account is physically unbound
- **WHEN** Sub2API reports an account as unbound because the protection guard removed it
- **THEN** the sidecar SHALL still list the account under the group with its final multiplier, protection multiplier, and `超过保护倍率` state

#### Scenario: Binding dialog opens
- **WHEN** an administrator opens a group's account-binding dialog
- **THEN** a protected logical member SHALL appear selected until the administrator explicitly deselects it

### Requirement: Safe manual protection actions
The administrator UI and API SHALL provide explicit actions to release protection and to remove a group-account relationship, and SHALL require confirmation for both destructive actions.

#### Scenario: Administrator releases protection
- **WHEN** an administrator confirms “解除倍率保护” for an exceeded account
- **THEN** the sidecar SHALL bind the account back first, then delete the protection record; a failed bind SHALL leave the record intact and report the error

#### Scenario: Administrator removes a relationship
- **WHEN** an administrator confirms “移除绑定” for any group-account row
- **THEN** the sidecar SHALL delete the protection record before unbinding the physical account, so a later multiplier refresh cannot automatically rebind the removed account

#### Scenario: Manual remove is retried
- **WHEN** physical unbinding fails after the protection record was deleted
- **THEN** the relationship SHALL remain logically removed and a later ordinary reconciliation SHALL NOT rebind it

### Requirement: Protection value validation and backward compatibility
Protection values SHALL be finite, non-negative numbers, and missing values SHALL retain legacy behavior.

#### Scenario: Invalid protection value
- **WHEN** an administrator submits a negative, NaN, infinite, or non-numeric protection value
- **THEN** the sidecar SHALL reject the request before changing state or Sub2API membership

#### Scenario: Existing state is upgraded
- **WHEN** the sidecar loads a state file created before protection support
- **THEN** it SHALL preserve all existing accounts, upstreams, bindings, and history with an empty protection map
