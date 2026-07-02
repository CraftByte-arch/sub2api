## 1. Usage Card Rebate Multiplier

- [x] 1.1 Add a failing usage-card fulfillment test proving rebate uses `pay_amount * current balance recharge multiplier`.
- [x] 1.2 Update payment fulfillment rebate base calculation for usage-card orders to use the current multiplier.
- [x] 1.3 Update audit detail assertions for multiplier-adjusted usage-card rebate records.

## 2. Verification

- [x] 2.1 Run targeted payment fulfillment tests.
- [x] 2.2 Run OpenSpec validation for `usage-card-affiliate-rebate-multiplier`.
