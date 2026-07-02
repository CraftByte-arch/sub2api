# payment Specification

## Purpose
TBD - created by archiving change configurable-payment-order-prefix. Update Purpose after archive.
## Requirements
### Requirement: Merchant order number prefix is configurable
The system SHALL allow administrators to configure the prefix used for newly generated payment provider merchant order numbers while preserving `sub2_` as the default prefix.

#### Scenario: Default prefix is used when unset
- **WHEN** no merchant order prefix has been configured
- **THEN** newly created payment orders use merchant order numbers beginning with `sub2_`
- **AND** the remaining merchant order number format remains `yyyyMMdd` plus an 8-character random alphanumeric suffix

#### Scenario: Custom prefix is used for new orders
- **WHEN** an administrator configures the merchant order prefix to `myshop_`
- **THEN** newly created payment orders use merchant order numbers beginning with `myshop_`
- **AND** existing payment orders keep their stored merchant order numbers unchanged

#### Scenario: Invalid prefix is rejected
- **WHEN** an administrator submits a merchant order prefix containing unsupported characters or an unsupported length
- **THEN** the system rejects the payment settings update
- **AND** no invalid merchant order prefix is saved

#### Scenario: Current prefix changes do not affect callbacks for existing orders
- **WHEN** a payment provider callback arrives for an order created with a previous merchant order prefix
- **THEN** the system locates the payment order by the full stored merchant order number from the callback
- **AND** the current merchant order prefix setting is not used to reinterpret that existing order number

### Requirement: Usage card payment orders generate affiliate rebates
The system SHALL apply affiliate rebate accrual to successfully fulfilled usage card payment orders when the purchasing user is bound to an inviter and affiliate rebates are enabled.

#### Scenario: Usage card purchase accrues inviter rebate
- **WHEN** an invited user completes a usage card payment order and the purchased usage card is issued successfully
- **THEN** the system records affiliate rebate accrual for the inviter using the usage card order `pay_amount` as the rebate base
- **AND** the rebate source order references the completed usage card payment order
- **AND** the payment audit log records the affiliate rebate result for that order

#### Scenario: Usage card purchase uses actual payment amount instead of issued card credit
- **WHEN** a usage card payment order has a `pay_amount` that differs from the issued card credit amount
- **THEN** affiliate rebate accrual for a successful usage card payment order MUST use `pay_amount` as the rebate base
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

