## Context

余额充值订单创建时会将 `PaymentOrder.Amount` 设置为 `实付金额 * BalanceRechargeMultiplier`，因此余额充值邀请返现按 `Amount` 计算时已经自然包含余额充值倍率。余额卡订单不同：`PaymentOrder.Amount` 存储发放卡额度，`PaymentOrder.PayAmount` 存储实际支付金额。当前余额卡邀请返现直接使用 `PayAmount`，导致最终进入长期余额的返现数量少乘了当前余额充值倍率。

用户已确认倍率读取履约时的当前配置，而不是下单时快照。

## Goals / Non-Goals

**Goals:**

- 余额卡邀请返现按 `PayAmount * 当前 BalanceRechargeMultiplier` 作为返利基数。
- 继续复用现有 affiliate rebate pipeline。
- 余额充值和订阅返现行为保持不变。
- 返现审计详情保留可解释字段。

**Non-Goals:**

- 不新增订单字段或数据库迁移。
- 不改支付订单创建 API、前端或管理端。
- 不回填历史余额卡订单。
- 不改变余额卡发放额度或余额充值倍率配置语义。

## Decisions

- 在支付履约层解析 affiliate rebate context，而不是修改 `AffiliateService` 的通用公式。理由：只有 `usage_card` 需要从 `PayAmount` 转换成长期余额基数；余额充值订单已在 `Amount` 中完成倍率折算，订阅不应乘余额充值倍率。
- 余额卡倍率按履约时当前 `PaymentConfig.BalanceRechargeMultiplier` 读取。理由：用户明确要求使用当前配置，且当前订单模型没有保存下单时倍率快照。
- 审计详情记录 `payAmount`、`balanceRechargeMultiplier`、`baseAmount` 和 `rebateAmount`。理由：同一个订单的实付金额、倍率和最终返现金额需要可追踪。

## Risks / Trade-offs

- [Risk] 履约时配置变化会影响已创建但未履约的余额卡订单返现金额。-> Mitigation: 这是确认后的当前配置规则；审计详情记录倍率用于追踪。
- [Risk] 读取支付配置失败会阻塞余额卡履约。-> Mitigation: 这与返现作为履约一部分的现有语义一致，失败后订单可重试，发卡和返现审计保持幂等。
