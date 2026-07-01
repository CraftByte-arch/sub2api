## Why

Users who register through an invitation link can already generate affiliate rebates when they complete balance recharge or subscription payment orders. Usage card purchases are also paid orders, but successful usage card fulfillment currently issues the card without applying the same affiliate rebate path, so inviters miss rebates for invited users who buy usage cards.

## What Changes

- Apply affiliate rebate processing after a usage card payment order successfully issues the purchased usage card.
- Use the usage card order `pay_amount` as the affiliate rebate base amount for usage card orders, not the issued card credit amount.
- Preserve existing affiliate feature controls: global enablement, inviter binding, inviter-specific rebate rate, freeze period, rebate duration, and per-invitee cap.
- Preserve existing affiliate audit idempotency so retrying usage card fulfillment does not duplicate rebates.
- Keep existing balance recharge and subscription rebate behavior unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `payment`: usage card payment fulfillment also participates in affiliate rebate accrual, with the usage card order `pay_amount` as the rebate base.

## Impact

- Backend payment fulfillment for `usage_card` orders.
- Affiliate rebate base amount calculation and payment audit logging for usage card orders.
- Backend tests covering successful rebate accrual, skipped rebate cases, and idempotent retries for usage card fulfillment.
- No database schema changes, API contract changes, or frontend changes are expected.
