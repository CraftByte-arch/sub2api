# Usage Card Affiliate Rebate Multiplier 验证报告

Change: `usage-card-affiliate-rebate-multiplier`
分支: `feature/20260702/usage-card-affiliate-rebate`
验证模式: `full`
日期: 2026-07-02

## 结论

PASS。

余额卡邀请返现已修正为按履约时当前余额充值倍率折算长期余额数量：

```text
返现长期余额 = PayAmount * AffiliateRatePercent / 100 * 当前 BalanceRechargeMultiplier
```

实现上先将余额卡订单 `PayAmount` 用当前 `BalanceRechargeMultiplier` 转成返利基数，再复用既有 `AccrueInviteRebateForOrder` 计算返利比例、冻结期、有效期、单人上限和审计幂等。

## 验证项

| 检查项 | 结果 | 证据 |
| --- | --- | --- |
| Comet verify 入口检查 | PASS | `.comet.yaml` 处于 `phase=verify`，`verify_result=pending` |
| 规模评估 | PASS | 5 个任务、1 个 delta capability、8 个变更文件，使用 full verify |
| tasks.md 完成度 | PASS | 5 项任务均已勾选 |
| OpenSpec artifact 完整性 | PASS | proposal、design、spec、tasks 均存在且 complete |
| OpenSpec change 校验 | PASS | `openspec validate --type change usage-card-affiliate-rebate-multiplier --json` 返回 1 passed / 0 failed |
| proposal/design/spec 一致性 | PASS | 均要求余额卡使用 `pay_amount * 当前余额充值倍率` 作为返利基数 |
| 相关单测 | PASS | `cd backend && go test -tags=unit ./internal/service -run 'TestExecuteUsageCardFulfillment(AccruesAffiliateRebateFromPayAmountWithCurrentBalanceMultiplier\|SkipsIneligibleAffiliateRebate\|NonSuccessfulStatusesDoNotAccrueAffiliateRebate\|FailedStatusRetriesAndAccruesAffiliateRebate\|DoesNotDuplicateCardOrAffiliateRebate)\|TestAffiliateRebateBaseAmountByOrderType' -count=1` |
| 后端构建 | PASS | `make -C backend build` |
| Diff hygiene | PASS | `git diff --check feed4a28e97ba0883084e5851e001b56bb8ae4c8...HEAD` |
| 基础密钥扫描 | PASS | 针对 hotfix diff 扫描常见 access key、private key、token、明文 password/api key 模式，无命中 |

## 关键实现核对

- 余额卡返利 context 在支付履约层计算，未修改通用 `AffiliateService` 公式。
- `usage_card` 订单使用 `calculateCreditedBalance(PayAmount, 当前 BalanceRechargeMultiplier)` 作为传入 `AccrueInviteRebateForOrder` 的基数。
- 余额充值和订阅订单仍沿用原有 `Amount` 基数。
- audit detail 记录 `payAmount`、`balanceRechargeMultiplier`、`baseAmount` 和 `rebateAmount`。
- 无 affiliate service 时继续直接跳过，不读取支付配置。

## 分支处理

用户明确要求：提交到当前分支，不合并到 `main`。本次 verify 将 `branch_status` 记录为 `handled`，当前分支保留。
