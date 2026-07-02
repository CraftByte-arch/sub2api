## MODIFIED Requirements

### Requirement: Usage card payment orders generate affiliate rebates
The system SHALL apply affiliate rebate accrual to successfully fulfilled usage card payment orders when the purchasing user is bound to an inviter and affiliate rebates are enabled.

#### Scenario: Usage card purchase accrues inviter rebate
- **WHEN** an invited user completes a usage card payment order and the purchased usage card is issued successfully
- **THEN** the system records affiliate rebate accrual for the inviter using the usage card order `pay_amount` multiplied by the current balance recharge multiplier as the rebate base
- **AND** the rebate source order references the completed usage card payment order
- **AND** the payment audit log records the affiliate rebate result for that order

#### Scenario: Usage card purchase uses actual payment amount adjusted by current balance recharge multiplier
- **WHEN** a usage card payment order has a `pay_amount` that differs from the issued card credit amount
- **THEN** affiliate rebate accrual for a successful usage card payment order MUST use `pay_amount * current balance recharge multiplier` as the rebate base
- **AND** the issued card credit amount MUST NOT be used as the rebate base

#### Scenario: Usage card purchase follows existing affiliate eligibility rules
- **WHEN** a usage card payment order is fulfilled but affiliate rebates are disabled, the buyer has no inviter, the applicable rebate rate is zero, the rebate duration has expired, or the per-invitee cap has already been reached
- **THEN** the usage card fulfillment still completes successfully
- **AND** no affiliate quota is added for that order
- **AND** the payment audit log records that affiliate rebate processing was skipped for that order

#### Scenario: Retried usage card fulfillment does not duplicate rebate
- **WHEN** a usage card payment order fulfillment is retried after the usage card was already issued or after affiliate rebate processing already recorded an applied or skipped result
- **THEN** the system MUST NOT create duplicate usage cards
- **AND** the system MUST NOT accrue duplicate affiliate rebate quota for the same payment order

#### Scenario: Non-successful usage card payment orders do not generate rebate
- **WHEN** a usage card payment order is pending, unpaid, failed before fulfillment, cancelled, expired, or in a refund-related state
- **THEN** the system MUST NOT accrue affiliate rebate quota for that order
