# Comet Design Handoff

- Change: configurable-payment-order-prefix
- Phase: design
- Mode: compact
- Context hash: cbde4eff2afc686c65468f8ffc8eb2527eae79677c73ccd5ca5d8f4aa4bede05

Generated-by: comet-handoff.sh

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## openspec/changes/configurable-payment-order-prefix/proposal.md

- Source: openspec/changes/configurable-payment-order-prefix/proposal.md
- Lines: 1-30
- SHA256: 32d92814d6351d6f81320b918dd36e5caff2d9a829ca6b46605beb1784875aaf

```md
## Why

Sub2API currently generates payment provider merchant order numbers with a fixed `sub2_` prefix. Operators who run multiple deployments, brands, or merchant accounts may need a recognizable merchant order prefix for reconciliation, provider dashboards, and collision avoidance across systems.

The prefix should be configurable from the existing admin payment settings while preserving the current default behavior for existing deployments.

## What Changes

- Add a payment configuration field for the merchant order number prefix.
- Use the configured prefix when generating new payment order `out_trade_no` values.
- Keep the existing suffix format unchanged: date in `yyyyMMdd` format plus an 8-character random alphanumeric string.
- Expose the prefix in the admin payment settings UI and persist it through the existing settings update flow.
- Validate the prefix so generated merchant order numbers remain compatible with supported payment providers.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `payment`: Payment configuration and order creation support a configurable merchant order number prefix.

## Impact

- Affects backend payment configuration parsing, updating, validation, and order creation.
- Affects the admin payment settings page and related TypeScript API types.
- Does not migrate or rewrite historical `out_trade_no` values.
- Does not change webhook lookup semantics; callbacks continue to locate orders by the full stored `out_trade_no`.
```

## openspec/changes/configurable-payment-order-prefix/design.md

- Source: openspec/changes/configurable-payment-order-prefix/design.md
- Lines: 1-35
- SHA256: 8e4e05ba32b93cfd21dabccc18200960b51721e9b813e10b4298b8f51a9c9744

```md
## Overview

The merchant order number prefix will become a normal payment configuration setting, similar to the existing product name prefix and suffix. New orders will read the current payment configuration and generate `out_trade_no` as:

```text
<configured-prefix><yyyyMMdd><8-char-random>
```

The default prefix remains `sub2_`, preserving existing behavior when operators do not configure the new field.

## Decisions

- Store the prefix through the existing settings repository using a new payment setting key.
- Add the field to `PaymentConfig` and `UpdatePaymentConfigRequest`, and include it in admin settings DTOs and admin payment config DTOs.
- Pass the loaded `PaymentConfig` into order number allocation so order creation uses one consistent configuration snapshot.
- Restrict prefixes to payment-provider-safe characters: ASCII letters, digits, underscore, and hyphen.
- Use length bounds of 1 to 16 characters after trimming whitespace.
- Treat an empty saved value as default `sub2_` rather than generating prefixless order numbers.

## Data Flow

1. Admin opens payment settings.
2. Backend returns the configured merchant order prefix, defaulting to `sub2_`.
3. Admin updates the prefix from the existing payment settings form.
4. Backend validates and stores the prefix in settings.
5. During order creation, `PaymentService` loads `PaymentConfig`, allocates a unique `out_trade_no` with the configured prefix, and stores it on `payment_orders`.
6. Payment providers receive the stored `out_trade_no` through the existing provider request path.

## Compatibility

Existing orders keep their original `out_trade_no`. Verification, callbacks, refunds, and admin searches already operate on the stored full order number, so changing the configured prefix only affects future orders.

## Validation

Invalid prefixes are rejected during configuration updates. This avoids creating orders that might be rejected by Alipay, WeChat Pay, EasyPay, Airwallex, or Stripe metadata/idempotency paths.
```

## openspec/changes/configurable-payment-order-prefix/tasks.md

- Source: openspec/changes/configurable-payment-order-prefix/tasks.md
- Lines: 1-23
- SHA256: 336f812f44ecaa8d24095f57ae28ddbba69c3e2fb2c43660b229d618c877d9c4

```md
## 1. Backend Configuration

- [ ] 1.1 Add a merchant order prefix payment setting with default `sub2_`.
- [ ] 1.2 Add parsing, update, and validation coverage in `PaymentConfigService`.
- [ ] 1.3 Include the field in admin settings and admin payment config request/response DTOs.

## 2. Order Number Generation

- [ ] 2.1 Change `out_trade_no` allocation to use the configured prefix.
- [ ] 2.2 Preserve existing date plus 8-character random suffix behavior.
- [ ] 2.3 Add tests for default prefix, custom prefix, and uniqueness retry behavior.

## 3. Admin UI

- [ ] 3.1 Add the merchant order prefix field to the payment settings page.
- [ ] 3.2 Update frontend API types, form defaults, payload mapping, and i18n strings.
- [ ] 3.3 Add or update frontend tests for loading and saving the new setting.

## 4. Verification

- [ ] 4.1 Run targeted backend payment configuration and order tests.
- [ ] 4.2 Run targeted frontend settings tests or type checks.
- [ ] 4.3 Validate the OpenSpec change artifacts.
```

## openspec/changes/configurable-payment-order-prefix/specs/payment/spec.md

- Source: openspec/changes/configurable-payment-order-prefix/specs/payment/spec.md
- Lines: 1-24
- SHA256: 5b0db4d798e2a1440f1db9a512043050a418cc3e049811cd8fd54e0a979c024c

```md
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
```

