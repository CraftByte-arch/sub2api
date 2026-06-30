# global-usage-card-available-summary 验证报告

日期：2026-06-30

## 结论

PASS。实现满足 OpenSpec change、Design Doc 和用户确认需求：

- 原全局顶栏长期余额仍由 `authStore.user.balance` 展示，未合并余额卡额度。
- 全局顶栏余额卡组件新增并直接显示可用未过期余额卡剩余总量，格式为 `$0.00`。
- 全局顶栏余额卡数量改为服务端统计的可用未过期余额卡数量。
- API 密钥页刷新按钮会联动刷新余额卡 summary 和长期余额信息，且顶栏资金刷新失败不阻塞 API key 数据刷新。

## 验证矩阵

| 检查项 | 结果 | 证据 |
| --- | --- | --- |
| OpenSpec artifacts 完整性 | PASS | `openspec status --change "global-usage-card-available-summary" --json` 返回 proposal/design/specs/tasks 均为 done，`isComplete: true` |
| OpenSpec change 校验 | PASS | `openspec validate --type change "global-usage-card-available-summary" --json` 返回 1 passed, 0 failed |
| 后端余额卡 summary 与可用口径 | PASS | `go test ./internal/service ./internal/repository -run 'UsageCard' -count=1` 在 `backend/` 下通过 |
| 前端顶栏、store、API Key 页联动 | PASS | `pnpm test:run src/components/common/__tests__/UsageCardMini.spec.ts src/views/user/__tests__/KeysView.spec.ts src/stores/__tests__/usageCardSummary.spec.ts` 在 `frontend/` 下通过，7 tests passed |
| TypeScript 类型检查 | PASS | `pnpm typecheck` 在 `frontend/` 下通过 |
| 前后端构建 | PASS | `make build` 在仓库根目录通过 |
| 安全检查 | PASS | 变更文件中未新增硬编码密钥、token、私钥或凭据；未跟踪的 `api_key` 未纳入提交 |

## Artifact 对照

- `proposal.md` 的目标已实现：顶栏余额卡摘要、原长期余额不变、API Key 页刷新联动。
- `design.md` 和技术设计文档一致：新增 `GET /usage-cards/summary`，复用 `ListAvailableCards` 服务端可用口径，前端使用共享 Pinia store。
- delta spec 的两个 requirement 均已覆盖：
  - 全局顶栏展示可用余额卡摘要。
  - API 密钥页刷新联动顶栏资金信息。

## 分支处理

按用户要求保留当前功能分支，不合并、不切换、不修改 `main`。`main` 继续作为同步源仓库代码的基线。
