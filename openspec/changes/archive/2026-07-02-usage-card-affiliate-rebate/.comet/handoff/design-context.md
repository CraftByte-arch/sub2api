# Comet Design Handoff

- Change: usage-card-affiliate-rebate
- Phase: design
- Mode: compact
- Context hash: d706edd3d0deab97070942f2c215242855ce93ce903185e2b64306d259937735

Generated-by: comet-handoff.sh

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## openspec/changes/usage-card-affiliate-rebate/proposal.md

- Source: openspec/changes/usage-card-affiliate-rebate/proposal.md
- Lines: 1-28
- SHA256: 867dcabd3f1c3fe08bd89e84bb6380a535b4117a2af5ae457ad091f8023a4ea9

```md
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
```

## openspec/changes/usage-card-affiliate-rebate/design.md

- Source: openspec/changes/usage-card-affiliate-rebate/design.md
- Lines: 1-44
- SHA256: b424bdba24e43f548c6d815e0aa47f18fb5828b8ad361c79db626633b704a921

```md
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
```

## openspec/changes/usage-card-affiliate-rebate/tasks.md

- Source: openspec/changes/usage-card-affiliate-rebate/tasks.md
- Lines: 1-22
- SHA256: 2b0404c7316295e3630f2879bc6082ee6b22134e05d4a71b3eaf92bc4bc1b2b4

```md
## 1. Rebate Base Amount

- [ ] 1.1 Extend affiliate rebate base amount resolution to support `usage_card` payment orders.
- [ ] 1.2 Ensure the usage card rebate base uses the order `pay_amount`, not the issued usage card credit amount.
- [ ] 1.3 Add focused unit coverage for balance, subscription, usage card, and unsupported order types.

## 2. Usage Card Fulfillment

- [ ] 2.1 Invoke affiliate rebate processing during successful usage card payment fulfillment after idempotent card issuance.
- [ ] 2.2 Preserve retry behavior so previously issued usage cards and previously applied or skipped affiliate rebates are not duplicated.
- [ ] 2.3 Preserve existing behavior for paid, failed, completed, and refund-related usage card order statuses.

## 3. Affiliate Rule Coverage

- [ ] 3.1 Add backend tests proving invited usage card buyers accrue inviter rebate from the usage card order `pay_amount`.
- [ ] 3.2 Add backend tests proving disabled affiliate settings, no inviter, zero rebate, expired duration, or reached per-invitee cap skip rebate without blocking usage card fulfillment.
- [ ] 3.3 Add backend tests proving repeated usage card fulfillment does not duplicate affiliate quota or audit results.

## 4. Verification

- [ ] 4.1 Run targeted payment fulfillment and affiliate service tests.
- [ ] 4.2 Run the relevant OpenSpec validation for `usage-card-affiliate-rebate`.
```

## openspec/changes/usage-card-affiliate-rebate/specs/payment/spec.md

- Source: openspec/changes/usage-card-affiliate-rebate/specs/payment/spec.md
- Lines: 1-30
- SHA256: e2bf790897bd79778a42777dfe55a55b1d9f246178ae8ca071d51be3a8863d7f

```md
## ADDED Requirements

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
```
