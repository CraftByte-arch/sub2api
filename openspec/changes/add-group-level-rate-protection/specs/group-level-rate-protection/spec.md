## ADDED Requirements

### Requirement: Group default protection configuration
管理员 SHALL 能为分组保存、读取和删除一个可选的非负有限倍率保护默认值，且所有操作必须通过现有管理员鉴权。

#### Scenario: Save a valid group default
- **WHEN** authenticated administrator submits a finite non-negative protection multiplier for an existing group
- **THEN** sidecar SHALL persist the value and return the saved group-level setting

#### Scenario: Reject an invalid group default
- **WHEN** administrator submits a negative, non-numeric, NaN, or infinite value
- **THEN** sidecar SHALL reject the request without changing the stored setting

#### Scenario: Delete a group default
- **WHEN** authenticated administrator deletes a group's default protection
- **THEN** sidecar SHALL remove the setting idempotently and reconcile any group-derived protected relationships

#### Scenario: Non-administrator access
- **WHEN** a request without a valid administrator session calls group-default endpoints
- **THEN** sidecar SHALL reject the request before reading or mutating the setting

### Requirement: Protection precedence
For a `(group, account)` relationship, the sidecar SHALL use an explicitly configured account-level protection multiplier first; only when no account-level protection exists SHALL it use the group's default multiplier.

#### Scenario: Account-level value overrides group default
- **WHEN** both account-level and group-level values exist for a bound API Key account
- **THEN** sidecar SHALL compare the final multiplier only with the account-level value and SHALL not apply the group default

#### Scenario: Group default is inherited
- **WHEN** no account-level value exists and a group default is configured for a physically bound API Key account
- **THEN** sidecar SHALL expose the group value as the effective protection with source `group` and apply the protection state machine

#### Scenario: No protection configured
- **WHEN** neither account-level nor group-level protection exists
- **THEN** sidecar SHALL preserve the existing binding and scheduling behavior without creating a logical protection record

### Requirement: Safe group-level automatic transitions
The sidecar SHALL apply group-level protection only to a physically bound account or an existing logical protection relationship, SHALL unbind when the final multiplier is strictly greater than the effective threshold, and SHALL rebind when a fresh final multiplier is less than or equal to the threshold.

#### Scenario: Inherited threshold is exceeded
- **WHEN** a bound API Key account inherits a group default and its final multiplier exceeds that default
- **THEN** sidecar SHALL remove the physical group binding, retain a logical group-derived relationship, and expose status `rate_protected`

#### Scenario: Inherited threshold recovers
- **WHEN** a group-derived logical relationship has a fresh final multiplier at or below the group default
- **THEN** sidecar SHALL restore the physical binding and clear the derived relationship while keeping the group default configured

#### Scenario: Final multiplier unavailable
- **WHEN** an account has an effective protection threshold but its final multiplier is missing, invalid, stale, or ambiguous
- **THEN** sidecar SHALL not bind or unbind the account and SHALL expose an unavailable reason

#### Scenario: Equality is safe
- **WHEN** final multiplier equals the effective protection threshold
- **THEN** sidecar SHALL treat the account as within protection and SHALL keep or restore the physical binding

### Requirement: Manual binding safety with group defaults
Manual removal SHALL take precedence over the group default for that relationship until the administrator explicitly binds or configures protection again.

#### Scenario: Remove a physically bound account
- **WHEN** administrator confirms removal of an account from a group while a group default exists
- **THEN** sidecar SHALL remove any account-level or derived logical protection intent before unbinding and SHALL not rebind it during later default reconciliation

#### Scenario: Remove an already protected account
- **WHEN** administrator removes an account whose physical binding was already removed by group protection
- **THEN** sidecar SHALL remove the derived relationship and leave the account physically unbound without future automatic rebinding

#### Scenario: Explicitly bind again
- **WHEN** administrator explicitly selects an unbound account in the group binding dialog
- **THEN** sidecar SHALL create/retain the physical binding and the group default MAY apply on a later reconciliation; a currently exceeded effective multiplier SHALL require the existing release-protection flow instead of bypassing the guard

### Requirement: Administrator projections and UI
The administrator overview SHALL expose group defaults and the effective protection source/threshold for account rows without exposing credentials.

#### Scenario: Group header displays default
- **WHEN** authenticated administrator opens the groups tab
- **THEN** each configured group SHALL show its default protection multiplier and controls to edit or clear it

#### Scenario: Account row displays inherited source
- **WHEN** an account has no account-level value but inherits a group default
- **THEN** account row SHALL show the effective threshold and label it as group default; when final multiplier is unavailable it SHALL state that protection is not active

#### Scenario: Account-level source is visible
- **WHEN** an account-level value overrides a group default
- **THEN** account row SHALL show the account-level threshold and identify it as account-level protection

#### Scenario: Binding dialog honors logical membership
- **WHEN** an inherited group-protected account is physically unbound
- **THEN** binding dialog SHALL show it as logically selected until administrator explicitly deselects it

### Requirement: Backward compatibility and sidecar isolation
The feature SHALL preserve old sidecar state and leave Sub2API behavior unchanged when the sidecar is absent or no protection is configured.

#### Scenario: Load old state
- **WHEN** sidecar loads a state file without group defaults
- **THEN** it SHALL initialize an empty group-default map and preserve all existing accounts, upstream snapshots, protections, and history

#### Scenario: Run Sub2API without sidecar
- **WHEN** only Sub2API is deployed or the sidecar is stopped
- **THEN** Sub2API SHALL continue using its original account binding, billing, and scheduling logic without requiring the new field
