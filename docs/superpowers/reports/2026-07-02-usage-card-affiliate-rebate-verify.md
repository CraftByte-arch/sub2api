# Usage Card Affiliate Rebate 验证报告

Change: `usage-card-affiliate-rebate`
分支: `feature/20260702/usage-card-affiliate-rebate`
验证模式: `full`
日期: 2026-07-02

## 结论

PASS。

本次实现满足 proposal、OpenSpec delta spec 和技术设计要求：邀请链接注册用户购买余额卡后，成功履约会进入既有 affiliate rebate 流程；余额卡返利基数使用订单记录的实际支付金额 `PaymentOrder.PayAmount` / `payment_orders.pay_amount`，不是余额卡发放额度 `Amount`。

按用户要求，分支处理方式为保持当前分支并提交验证状态，不合并到 `main`。`main` 保持作为同步上游源仓库的基线。

## 验证项

| 检查项 | 结果 | 证据 |
| --- | --- | --- |
| Comet verify 入口检查 | PASS | `.comet.yaml` 处于 `phase=verify`，`verify_result=pending` |
| 规模评估 | PASS | 11 个任务、1 个 delta capability、14 个变更文件，使用 full verify |
| tasks.md 完成度 | PASS | 11 项任务均已勾选 |
| OpenSpec artifact 完整性 | PASS | proposal、design、spec、tasks 均存在且 complete |
| OpenSpec 严格校验 | PASS | `openspec validate --type change usage-card-affiliate-rebate --json` 返回 1 passed / 0 failed |
| proposal 目标覆盖 | PASS | usage card 成功履约调用返利、pay_amount 作为基数、保留既有规则和幂等性 |
| delta spec 场景覆盖 | PASS | 覆盖成功返利、实际支付金额基数、跳过条件、重试幂等、非成功订单不返利 |
| Design Doc 一致性 | PASS | 实现路径符合 `docs/superpowers/specs/2026-07-02-usage-card-affiliate-rebate-design.md` |
| 服务单测 | PASS | `cd backend && go test -tags=unit ./internal/service -count=1` |
| 后端构建 | PASS | `make -C backend build` |
| Diff hygiene | PASS | `git diff --check 8a24d5a7c4b435dea7e38f72aecced711ae7dd1d...HEAD` |
| 基础密钥扫描 | PASS | 针对本 change diff 扫描常见 access key、private key、token、明文 password/api key 模式，无命中 |

## 关键实现核对

- `affiliateRebateBaseAmount` 对 `balance` 和 `subscription` 继续返回 `Amount`。
- `affiliateRebateBaseAmount` 对 `usage_card` 返回 `PayAmount`。
- `doUsageCard` 在余额卡幂等发放后、订单完成前调用 `applyAffiliateRebateForOrder`。
- 余额卡已发放或返利已应用/跳过后的重复履约不会重复发卡或重复累计返利。
- `FAILED` 余额卡订单只有在 `PaidAt` 已存在时才允许走履约重试，避免付款前失败订单被错误发卡或返利。
- 既有余额充值、订阅返利行为保持不变。

## Review 处理

代码评审指出 `FAILED` 状态语义过载风险：付款前失败与付款后履约失败都可能表现为 failed。已修复为 `FAILED` 余额卡订单必须存在 `PaidAt` 才可走履约重试，并补充对应单测。

## 分支处理

用户明确选择：提交到当前分支，不合并到 `main`。本次 verify 将 `branch_status` 记录为 `handled`，当前分支保留用于后续处理。
