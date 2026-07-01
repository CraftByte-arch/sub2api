---
comet_change: usage-card-affiliate-rebate
role: technical-design
canonical_spec: openspec
---

# Usage Card Affiliate Rebate Design

## Confirmed Behavior

Usage card payment fulfillment must participate in the existing affiliate rebate pipeline. When an invited user successfully buys a usage card, the inviter rebate is calculated from the payment order's recorded `pay_amount`.

This intentionally differs from `payment_orders.amount` for usage card orders. Existing order creation stores usage card issued credit in `amount` via `UsageCardPlan.AmountUSD`, while the actual payment value is stored in `pay_amount`. The rebate base for usage card orders is therefore `PaymentOrder.PayAmount`.

No database migration, frontend API change, admin UI change, or backfill is part of this change.

## Implementation Plan

Update `backend/internal/service/payment_fulfillment.go`.

Extend `affiliateRebateBaseAmount(o)`:

- Keep `balance` and `subscription` returning `o.Amount`.
- Add `payment.OrderTypeUsageCard` returning `o.PayAmount`.
- Keep unsupported or nil orders returning `0`.

Update `doUsageCard(ctx, o)`:

- Preserve existing disabled service, missing plan, plan lookup, and card issue behavior.
- Keep usage card issuance idempotent through the existing `USAGE_CARD_SUCCESS` audit check and `IssueFromPayment` source-order semantics.
- After successful card issuance, call `applyAffiliateRebateForOrder(ctx, o)` before `markCompleted(ctx, o, "USAGE_CARD_SUCCESS")`.
- On a retry where `USAGE_CARD_SUCCESS` already exists, call `applyAffiliateRebateForOrder(ctx, o)` before marking completed, matching balance fulfillment's retry path.

The implementation should not add a usage-card-specific rebate service path. The existing `applyAffiliateRebateForOrder` already handles feature enablement, inviter binding, custom/global rebate rate, freeze period, rebate duration, per-invitee cap, source order ID, and audit idempotency.

## Failure And Retry Semantics

Affiliate rebate processing remains part of fulfillment. If usage card issuance succeeds but rebate processing returns an error, fulfillment should fail and leave the order retryable. Retry must not issue another usage card and must not duplicate rebate because:

- Usage card issuance is keyed by payment order/source order.
- Affiliate rebate audit claim uses the existing `AFFILIATE_REBATE_APPLIED` / `AFFILIATE_REBATE_SKIPPED` actions for idempotency.

Skipped rebate cases, such as disabled affiliate settings, no inviter, zero rate, expired duration, or cap reached, should still let usage card fulfillment complete.

## Tests

Add focused backend tests in `backend/internal/service/payment_fulfillment_test.go`.

Required coverage:

- `affiliateRebateBaseAmount` returns `Amount` for balance and subscription, `PayAmount` for usage card, and `0` for unsupported/nil orders.
- An invited usage card buyer accrues inviter rebate from `pay_amount`, with a test order where `amount` and `pay_amount` differ.
- The affiliate audit log records the usage card rebate result with `baseAmount` equal to `pay_amount`.
- Ineligible affiliate cases skip rebate without blocking usage card fulfillment.
- Repeated usage card fulfillment does not duplicate usage card issuance, affiliate quota, or affiliate audit result.
- Existing balance and subscription rebate behavior remains unchanged.

Targeted verification should run payment fulfillment and affiliate-related backend tests, plus OpenSpec validation for `usage-card-affiliate-rebate`.

## Risks

`pay_amount` can include fee configuration. This is accepted because the confirmed rule is to rebate from actual recorded payment amount, not mutable plan price or issued card credit.

Applying rebate before marking a usage card order complete can keep an already issued order retryable if rebate processing fails. This matches existing balance/subscription semantics and favors eventual correctness over silently losing affiliate rebate state.
