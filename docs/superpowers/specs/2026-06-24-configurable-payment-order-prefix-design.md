---
comet_change: configurable-payment-order-prefix
role: technical-design
canonical_spec: openspec
---

# Configurable Payment Order Prefix Design

## Context

Payment order creation currently generates provider-facing merchant order numbers with a fixed `sub2_` prefix. Operators need the prefix to be configurable for reconciliation and multi-deployment identification, while preserving the current default behavior and avoiding any impact on historical orders.

OpenSpec remains the canonical requirement source:

- `openspec/changes/configurable-payment-order-prefix/specs/payment/spec.md`
- `openspec/changes/configurable-payment-order-prefix/proposal.md`
- `openspec/changes/configurable-payment-order-prefix/design.md`

## Architecture

Use the existing payment configuration path end to end.

Add a payment setting key for the merchant order prefix in `PaymentConfigService`. Parse it into `PaymentConfig`, expose it through `UpdatePaymentConfigRequest`, and wire it through both admin payment config endpoints and the integrated admin settings endpoint. The admin settings page will render a text input in the payment settings section and submit the value with the rest of the payment form.

Order creation already loads `PaymentConfig` before validation and allocation. Change order number allocation to accept the loaded prefix from that same config snapshot:

```text
<merchant_order_prefix><yyyyMMdd><8-char-random>
```

The date and random suffix remain unchanged.

## Validation

The backend is the source of truth for validation. The prefix is normalized by trimming whitespace. Empty values fall back to the default `sub2_`.

Valid custom prefixes:

- Length: 1-16 characters after trimming
- Characters: ASCII letters, ASCII digits, `_`, `-`

Invalid values are rejected during payment settings update. The frontend can show helper text, but it must not be the only validation layer.

## Data Flow

1. Admin loads settings.
2. Backend returns `payment_merchant_order_prefix` and `merchant_order_prefix`, defaulting to `sub2_` where applicable.
3. Admin edits and saves the field from the payment settings page.
4. Backend validates and persists the prefix through the existing settings repository.
5. User creates a payment order.
6. `PaymentService.CreateOrder` uses the loaded config to allocate a unique `out_trade_no`.
7. Provider adapters receive the stored `out_trade_no` through the existing `CreatePaymentRequest.OrderID` path.
8. Callbacks and verification continue to look up orders by the full stored `out_trade_no`.

## Compatibility

Existing orders keep their stored `out_trade_no`; there is no migration and no rewrite. Changing the prefix affects only future payment orders. The unique partial index on `payment_orders.out_trade_no` remains the collision guard.

Refunds, callbacks, manual verification, and admin searches already use the stored full order number, so they do not need to interpret the current prefix setting.

## Implementation Notes

- Prefer a small helper such as `normalizeMerchantOrderPrefix` for defaulting and validation.
- Prefer changing `generateOutTradeNo(prefix string)` rather than introducing a second generator.
- Keep `allocateOutTradeNo` responsible for uniqueness retry; pass the selected prefix into it.
- Keep admin DTO names consistent with existing payment settings naming:
  - service/admin payment config: `merchant_order_prefix`
  - integrated settings form: `payment_merchant_order_prefix`

## Testing

Backend tests:

- `PaymentConfigService` parses default `sub2_`.
- `UpdatePaymentConfig` persists a valid prefix.
- Invalid prefixes are rejected and not saved.
- Order creation uses a custom prefix.
- Uniqueness retry still works with the configured prefix.
- Admin settings response and update DTOs include the field.

Frontend tests:

- Admin settings form initializes `payment_merchant_order_prefix`.
- Save payload includes `payment_merchant_order_prefix`.
- i18n labels and helper text render for the new field.

Verification commands should include targeted Go tests for payment config/order creation and frontend settings tests or type checks.
