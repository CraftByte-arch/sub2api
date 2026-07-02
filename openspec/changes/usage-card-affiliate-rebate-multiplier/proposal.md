## Why

余额卡邀请返现当前只按订单实际支付金额 `pay_amount` 乘邀请返现比例入账，漏掉了当前余额充值倍率。邀请返现最终进入长期余额，因此余额卡返现数量应与余额充值到账规则一致，按当前余额充值倍率折算。

## What Changes

- 修正余额卡订单的邀请返现基数：使用 `pay_amount * 当前余额充值倍率`。
- 保持余额充值和订阅订单的现有返现逻辑不变。
- 保持现有邀请绑定、返现比例、冻结期、返现有效期、单人上限和审计幂等逻辑不变。
- 在余额卡返现审计详情中记录实付金额、余额充值倍率和最终基数，便于排查。

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `payment`: 余额卡支付订单邀请返现基数从单独 `pay_amount` 调整为 `pay_amount * 当前余额充值倍率`。

## Impact

- Backend payment fulfillment for `usage_card` orders.
- Affiliate rebate base amount calculation and payment audit detail for usage card orders.
- Backend tests covering multiplier-adjusted usage card affiliate rebates.
