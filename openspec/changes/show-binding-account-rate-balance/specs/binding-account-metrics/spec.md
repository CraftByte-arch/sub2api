## ADDED Requirements

### Requirement: Binding account rows show final multiplier
The group account management dialog SHALL show each API Key account's final calculated multiplier using the current overview projection, without making a new upstream request.

#### Scenario: Final multiplier is available
- **WHEN** an account has `upstream_final_multiplier.status` equal to `available` and a finite `final_multiplier`
- **THEN** its row displays the value in multiplier form such as `0.16x`
- **AND** the row provides a non-color-only explanation of the multiplier source on hover or accessible description

#### Scenario: Final multiplier is unavailable
- **WHEN** the account has no usable final multiplier projection
- **THEN** its row displays `未计算` or an equivalent unavailable label
- **AND** it displays a human-readable reason such as unbound, stale, recharge rate unset, or group multiplier unknown

### Requirement: Binding account rows show available balance
The group account management dialog SHALL show each API Key account's available balance projection when one is available, while distinguishing unavailable, unlimited, and insufficient states.

#### Scenario: Managed balance is available
- **WHEN** the account overview contains a finite managed balance remaining value
- **THEN** its row displays that value as the account's available balance
- **AND** the display identifies it as a projected/synchronized balance rather than a new billing value

#### Scenario: Balance is insufficient
- **WHEN** the account overview marks the managed balance as insufficient or exhausted
- **THEN** its row displays `余额不足`
- **AND** the row includes a textual or accessible explanation of the exhausted state

#### Scenario: Balance is unlimited or unavailable
- **WHEN** the account is marked unlimited
- **THEN** its row displays `不限额度`
- **WHEN** no managed balance projection is available and the account is not unlimited
- **THEN** its row displays `暂不可用` or an equivalent state instead of displaying zero

### Requirement: Metrics do not change binding behavior
Adding multiplier and balance information SHALL NOT change candidate filtering, checkbox selection, pending-change calculation, save requests, or platform compatibility rules.

#### Scenario: Selecting an account
- **WHEN** an administrator checks or unchecks an account row
- **THEN** only the existing draft binding set changes
- **AND** the multiplier and balance cells remain read-only

#### Scenario: Saving bindings
- **WHEN** the administrator saves pending changes
- **THEN** the dialog sends the same group account IDs payload as before
- **AND** the new metric cells do not cause an additional upstream or Sub2API mutation

### Requirement: Binding metrics remain readable on small screens
The dialog SHALL keep the new metric labels and values readable without horizontal page overflow on narrow viewports.

#### Scenario: Narrow viewport
- **WHEN** the dialog is rendered at the existing mobile breakpoint
- **THEN** multiplier and balance fields stack below the account identity with visible field labels
- **AND** the checkbox and primary account state remain keyboard and touch accessible
