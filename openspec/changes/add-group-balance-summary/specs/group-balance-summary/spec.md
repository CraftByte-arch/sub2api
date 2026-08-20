## ADDED Requirements

### Requirement: Group headers show enabled and disabled available balances

The sidecar group overview SHALL show separate available-balance summaries for currently enabled and non-enabled accounts in each group, using the same account balance projection already shown on account rows.

#### Scenario: Both categories have finite balances

- **WHEN** a group has enabled accounts and non-enabled accounts with finite `admin_balance.remaining` values
- **THEN** the group header displays the sum for enabled accounts and the sum for non-enabled accounts separately
- **AND** it displays the contributing account counts

#### Scenario: A category contains unavailable, unlimited, or insufficient balances

- **WHEN** a category contains accounts whose balance is unavailable, unlimited, or insufficient
- **THEN** the summary does not treat unavailable values as zero or include unlimited accounts in the numeric sum
- **AND** the group header communicates the corresponding state/count in text or an accessible description

#### Scenario: No accounts in one category

- **WHEN** a group has no enabled or no non-enabled accounts
- **THEN** that category displays `—` (or an equivalent zero-member label) without affecting the other category

### Requirement: Group balance summaries do not change scheduling or billing

The summary SHALL be read-only and SHALL NOT change account binding, scheduling state, account billing, quota accounting, or upstream synchronization behavior.

#### Scenario: Overview refresh

- **WHEN** the existing overview polling refreshes account data
- **THEN** the group summaries are recomputed from the same response without an additional upstream request

#### Scenario: Legacy response compatibility

- **WHEN** an older sidecar response does not contain `group_balance_summaries`
- **THEN** the page remains usable and simply omits the new summary fields
