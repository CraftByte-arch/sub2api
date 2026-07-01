## Context

Affiliate rebate accrual is currently centralized in payment fulfillment through `applyAffiliateRebateForOrder`. Balance recharge and subscription fulfillment call that path after their paid order has been fulfilled. Usage card fulfillment currently issues the purchased card and marks the order completed without applying affiliate rebate processing.

Usage card orders have two relevant monetary values: `amount`, which currently stores the issued usage card credit amount (`AmountUSD`), and `pay_amount`, which stores the actual payment amount recorded for the order. For this change, the desired rebate base is `pay_amount`, not the issued card credit amount.

## Goals / Non-Goals

**Goals:**

- Apply the existing affiliate rebate pipeline to successfully fulfilled usage card payment orders.
- Calculate usage card affiliate rebates from the usage card order `pay_amount`.
- Preserve existing affiliate settings, inviter binding rules, cap checks, freeze behavior, and audit idempotency.
- Keep existing balance recharge and subscription rebate behavior unchanged.

**Non-Goals:**

- Do not change invitation binding, affiliate rate configuration, quota transfer, refund handling, or usage card billing.
- Do not change frontend APIs or admin UI behavior.
- Do not introduce a database migration; the required rebate base already exists on the payment order as `pay_amount`.

## Decisions

- Extend the existing payment-level affiliate rebate path to include `usage_card` orders. Rationale: the existing path already handles feature enablement, no-inviter skips, caps, freeze periods, source order IDs, and audit idempotency. Alternative: add a separate usage-card-specific rebate path, but that would duplicate business rules and increase drift risk.

- Use the usage card order `pay_amount` as the rebate base for `usage_card` orders. Rationale: inviters should be rebated on the actual recorded payment amount for the order, and `pay_amount` is already persisted on the order. Alternative: use `amount`, which matches the issued usage card credit but does not represent the user's actual payment amount.

- Treat affiliate rebate application as part of successful usage card fulfillment, after card issuance is idempotently completed and before marking the payment order completed. Rationale: this matches balance and subscription fulfillment ordering, so a failed rebate attempt can leave the order retryable instead of silently losing rebate state. Alternative: mark the order complete first and apply rebates asynchronously, but that would require a new retry/reconciliation mechanism.

- Preserve payment audit idempotency for usage card rebate attempts. Rationale: usage card fulfillment can be retried after partial failure, so the same order must not accrue duplicate affiliate quota. Alternative: rely only on affiliate ledger uniqueness, but the existing payment audit claim already provides order-level idempotency and diagnostics.

## Risks / Trade-offs

- [Risk] `pay_amount` can include payment fee configuration, so usage card rebates may include that fee component. -> Mitigation: this matches the confirmed "actual payment amount" rule and avoids mutable plan-price lookups.
- [Risk] Applying rebate before marking order complete can keep an otherwise issued usage card order retryable if rebate processing fails. -> Mitigation: this matches existing balance/subscription semantics and preserves eventual correctness; idempotent issuance and rebate audit claims prevent duplicate work on retry.
- [Risk] Existing admin rebate records may not clearly distinguish balance, subscription, and usage card rebates. -> Mitigation: retain source order linkage and existing payment type/order metadata so records remain traceable without adding UI changes in this change.

## Migration Plan

No schema migration is expected. On deploy, newly fulfilled usage card payment orders should start producing affiliate rebate audit entries when affiliate rules allow it. Existing completed usage card orders are not backfilled. Rollback removes the usage card order type from affiliate rebate calculation and fulfillment invocation.

## Open Questions

None.
