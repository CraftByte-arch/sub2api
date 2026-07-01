## 1. Rebate Base Amount

- [x] 1.1 Extend affiliate rebate base amount resolution to support `usage_card` payment orders.
- [x] 1.2 Ensure the usage card rebate base uses the order `pay_amount`, not the issued usage card credit amount.
- [x] 1.3 Add focused unit coverage for balance, subscription, usage card, and unsupported order types.

## 2. Usage Card Fulfillment

- [x] 2.1 Invoke affiliate rebate processing during successful usage card payment fulfillment after idempotent card issuance.
- [x] 2.2 Preserve retry behavior so previously issued usage cards and previously applied or skipped affiliate rebates are not duplicated.
- [x] 2.3 Preserve existing behavior for paid, failed, completed, and refund-related usage card order statuses.

## 3. Affiliate Rule Coverage

- [x] 3.1 Add backend tests proving invited usage card buyers accrue inviter rebate from the usage card order `pay_amount`.
- [x] 3.2 Add backend tests proving disabled affiliate settings, no inviter, zero rebate, expired duration, or reached per-invitee cap skip rebate without blocking usage card fulfillment.
- [x] 3.3 Add backend tests proving repeated usage card fulfillment does not duplicate affiliate quota or audit results.

## 4. Verification

- [ ] 4.1 Run targeted payment fulfillment and affiliate service tests.
- [ ] 4.2 Run the relevant OpenSpec validation for `usage-card-affiliate-rebate`.
