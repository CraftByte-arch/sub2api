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
