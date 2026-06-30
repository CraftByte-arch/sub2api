## ADDED Requirements

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
