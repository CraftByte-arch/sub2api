# Brainstorm Summary

- Change: usage-card-affiliate-rebate
- Date: 2026-07-02

## 确认的技术方案

- 余额卡返利基数按订单实际支付金额 `pay_amount`。
- 当前 `payment_orders.amount` 对 usage card 订单存的是发放额度 `AmountUSD`。
- 当前 `payment_orders.pay_amount` 存的是含支付手续费后的实付金额。
- 当前 schema 已有 `payment_orders.pay_amount`，可直接作为余额卡返利基数。
- 余额卡履约已经通过 `source_order_id` / audit log 实现发卡幂等；返利已有 `AFFILIATE_REBATE_APPLIED` / `AFFILIATE_REBATE_SKIPPED` 审计幂等。
- 采用方案 A：在 `affiliateRebateBaseAmount` 中让 `usage_card` 订单返回 `o.PayAmount`，并在 usage card 成功发卡后调用现有 `applyAffiliateRebateForOrder`。

## 关键取舍与风险

- 不新增 schema，不读取当前 plan price，不从 `pay_amount / (1 + fee_rate)` 反推。
- `pay_amount` 如果包含手续费，返利也包含手续费部分；这是已确认的“按实际支付金额返利”规则。
- 余额卡返利失败会像余额/订阅返利一样让履约保持可重试；现有发卡幂等和返利审计幂等避免重复发卡/重复返利。

## 测试策略

- 单元测试覆盖 usage card 订单返利基数来自 `pay_amount`，且与 `AmountUSD` 不同。
- 履约测试覆盖成功发卡后返利、无资格时 skipped、重复履约不重复发卡/返利。
- 回归测试覆盖 balance/subscription 返利基数不变。

## Spec Patch

回写：delta spec 已改为 usage card 返利使用订单 `pay_amount`，而不是 issued card credit amount。
