# Comet Design Handoff

- Change: global-usage-card-available-summary
- Phase: design
- Mode: compact
- Context hash: 8609bdef0c1d595ccf20e82bb77f6caa314b15f4ae9c541b64e2316d5e618ce2

Generated-by: comet-handoff.sh

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## openspec/changes/global-usage-card-available-summary/proposal.md

- Source: openspec/changes/global-usage-card-available-summary/proposal.md
- Lines: 1-31
- SHA256: 8b68d02820e34b53d3cef85d3159be00711d50c5420a91d45cc9bdd749c45fd2

```md
## Why

当前全局顶栏已经显示用户长期余额，同时余额卡入口显示的是余额卡总数，包含已过期、已耗尽、暂停或尚未生效的卡。用户真正需要的是在不改变原长期余额展示的前提下，额外看到“当前可用未过期余额卡”的数量和剩余可用总量，以便在任何页面快速判断还能通过余额卡消费多少额度。

API 密钥页已有刷新按钮，但它只刷新 API key 列表和相关用量数据；当用户在该页查看密钥并刷新时，顶栏余额卡信息和长期余额信息也应同步刷新。

## What Changes

- 将全局顶栏余额卡组件的数字从余额卡总数改为当前可用未过期余额卡数量。
- 保留原全局顶栏长期余额展示不变。
- 在全局顶栏新增余额卡剩余可用总量展示，直接显示当前可用未过期余额卡余额总和。
- 余额卡悬浮层/摘要使用同一套“当前可用未过期”统计口径。
- API 密钥页刷新按钮除刷新 API key 数据外，也触发全局顶栏余额卡信息和长期余额信息刷新。
- “可用未过期”按服务端口径统计：状态 active、未删除、已开始、未过期、未耗尽。

## Capabilities

### New Capabilities

- `usage-cards`: 用户侧余额卡可用额度摘要与全局顶栏展示。

### Modified Capabilities

- None.

## Impact

- 影响后端用户余额卡查询/摘要接口，优先复用现有可用余额卡判断逻辑。
- 影响前端全局顶栏 `UsageCardMini` 展示和刷新状态管理，但不改变原长期余额组件的展示口径。
- 影响 API 密钥页刷新按钮，使其联动刷新余额卡信息和长期余额信息。
- 不涉及数据库结构变更、不改变余额卡购买/兑换/扣费规则、不改变后台余额卡管理流程。
```

## openspec/changes/global-usage-card-available-summary/design.md

- Source: openspec/changes/global-usage-card-available-summary/design.md
- Lines: 1-46
- SHA256: 8c20565377b4062420735c6643f7287e19f5f77257a454d2d7a804a9132bc359

```md
## Context

全局顶栏通过 `AppHeader` 渲染长期余额展示和 `UsageCardMini`，因此这些信息会出现在所有使用主布局的页面。当前长期余额展示的是用户账户余额，应保持原样；当前 `UsageCardMini` 调用用户余额卡列表接口并显示 `cards.length`，这会把历史卡、过期卡、耗尽卡和不可用卡都计入数字。

后端已有 `UserUsageCard.IsAvailableAt(now)` 和 `ListAvailableCards(ctx, userID, now)`，可用口径已经和扣费侧保持一致：active、未删除、已开始、未过期、未耗尽。此次变更应复用这套服务端口径，避免前端重复实现并产生差异。

## Goals / Non-Goals

**Goals:**

- 全局顶栏余额卡组件直接展示可用余额卡余额总和。
- 全局顶栏余额卡组件的数量数字展示可用未过期余额卡数量。
- 原全局顶栏长期余额展示保持不变。
- 悬浮层/摘要和顶栏直接展示使用同一个 summary 数据来源。
- API 密钥页刷新按钮可触发全局顶栏余额卡 summary 和长期余额信息重新加载。

**Non-Goals:**

- 不修改余额卡扣费优先级和实际扣费逻辑。
- 不修改余额卡购买、兑换、发放、暂停、取消等业务流程。
- 不修改数据库 schema。
- 不把展示限制在 API 密钥页；顶栏仍是全局组件。
- 不把余额卡剩余可用总量合并进原长期余额数字。

## Decisions

- 新增或复用一个用户侧余额卡 summary 获取能力，由后端返回 `available_count` 和 `available_remaining_usd`。理由：服务端已有可用判断，summary 响应比完整列表更适合顶栏；替代方案是在前端根据 `/usage-cards` 列表计算，但会复制 starts/expires/status/remaining 判断并增加漂移风险。
- 顶栏余额卡组件从 summary 状态读取数量和余额，并继续在悬浮层展示最近余额卡列表或可用卡明细；原长期余额组件仍读取用户账户余额。理由：长期余额和余额卡余额是两个不同资金口径，必须并列展示而不是合并；替代方案是改原长期余额数字，会让用户分不清账户余额与余额卡额度。
- 前端提供共享刷新入口，例如 store/composable 或组件暴露的全局事件，使 API 密钥页刷新按钮能触发顶栏 summary 刷新。理由：`UsageCardMini` 位于 `AppHeader`，API 密钥页不是其父组件，直接 props 传递不合适；替代方案是在 API key 页重复请求并显示本地摘要，会破坏全局顶栏单一来源。
- API 密钥页刷新按钮应同时调用 API key 数据刷新、余额卡 summary 刷新和长期余额刷新。理由：用户在密钥页刷新时预期顶栏资金信息也跟随更新；替代方案是只刷新余额卡 summary，会留下原长期余额可能过期的问题。
- 余额卡 summary 或长期余额加载失败不应阻塞 API 密钥页列表刷新。理由：API key 数据和顶栏资金信息是独立读路径，顶栏刷新失败只影响顶栏展示。

## Risks / Trade-offs

- [Risk] 顶栏和悬浮层数据可能来自 summary 与列表两个接口，短时间内不一致。→ Mitigation：刷新入口同时刷新 summary 和必要列表数据，展示文案以 summary 为准。
- [Risk] API 密钥页刷新联动全局状态，测试需要覆盖跨组件行为。→ Mitigation：把刷新逻辑集中在可测试的 store/composable，并在页面测试中断言刷新函数被调用。
- [Risk] 余额显示精度不一致。→ Mitigation：沿用余额卡现有金额精度或明确统一为顶栏两位小数、明细四位小数。
- [Risk] 用户误以为长期余额包含余额卡额度。→ Mitigation：原长期余额展示保持不变，新增余额卡剩余可用总量在余额卡组件内呈现，并使用明确文案区分。

## Migration Plan

无需数据迁移。发布后顶栏长期余额继续按原口径显示，余额卡组件即时从余额卡总数切换为可用未过期余额卡数量，并新增余额卡剩余可用总量。回滚时移除 summary 调用并恢复原 `cards.length` 展示即可。

## Open Questions

- 顶栏余额总和最终显示精度采用 `$0.00` 还是 `$0.0000` 需要在实现时按现有视觉空间确认；建议顶栏使用两位小数，悬浮层明细保留四位小数。
```

## openspec/changes/global-usage-card-available-summary/tasks.md

- Source: openspec/changes/global-usage-card-available-summary/tasks.md
- Lines: 1-31
- SHA256: 85eacd09f11e0c755da15fe8a99a15e6916b66f14c9f1e8f659f35deb2241b84

```md
## 1. Backend Summary API

- [ ] 1.1 Add a user usage-card summary response containing available card count and available remaining USD.
- [ ] 1.2 Reuse the existing server-side available-card criteria for active, started, unexpired, undeleted, non-exhausted cards.
- [ ] 1.3 Register the authenticated user route and add backend tests for active, expired, exhausted, suspended, cancelled, and future cards.

## 2. Frontend Shared State

- [ ] 2.1 Add a frontend API method and type for loading the usage-card summary.
- [ ] 2.2 Add a shared store/composable refresh entry point for topbar usage-card summary data.
- [ ] 2.3 Add or reuse a refresh entry point for the existing topbar long-term account balance.
- [ ] 2.4 Ensure usage-card summary and long-term balance refresh failures do not block callers that also refresh unrelated page data.

## 3. Global Topbar UI

- [ ] 3.1 Change `UsageCardMini` to show available card count instead of total card count.
- [ ] 3.2 Show the available remaining USD total directly in the global topbar component.
- [ ] 3.3 Keep the existing long-term balance display unchanged and visually separate from the usage-card remaining total.
- [ ] 3.4 Align the hover/summary copy with the available-card summary while preserving useful card details.
- [ ] 3.5 Add or update frontend tests for zero cards, mixed unavailable cards, separated long-term balance display, and formatted summary display.

## 4. API Key Page Integration

- [ ] 4.1 Update the API key page refresh action to call the shared usage-card summary refresh and long-term balance refresh.
- [ ] 4.2 Add or update tests proving the API key refresh triggers both topbar usage-card information refresh and long-term balance refresh.

## 5. Verification

- [ ] 5.1 Run targeted backend usage-card tests.
- [ ] 5.2 Run targeted frontend tests or type checks for the topbar and API key page.
- [ ] 5.3 Validate the OpenSpec change artifacts.
```

## openspec/changes/global-usage-card-available-summary/specs/usage-cards/spec.md

- Source: openspec/changes/global-usage-card-available-summary/specs/usage-cards/spec.md
- Lines: 1-41
- SHA256: 1c3f1967384997af1c6d64398052418de37304cd42a652144845213382b20dc5

```md
## ADDED Requirements

### Requirement: 全局顶栏展示可用余额卡摘要
系统 SHALL 在不改变原长期余额展示的前提下，在全局顶栏余额卡组件中展示当前用户可用未过期余额卡的数量和余额总和。

#### Scenario: 只统计可用未过期余额卡
- **WHEN** 用户同时拥有 active 未过期未耗尽余额卡、已过期余额卡、已耗尽余额卡、暂停余额卡、取消余额卡或尚未开始的余额卡
- **THEN** 顶栏余额卡数量只统计 active、未删除、已开始、未过期、未耗尽的余额卡
- **AND** 顶栏余额卡余额总和只累加这些可用余额卡的剩余额度

#### Scenario: 顶栏直接显示余额总和
- **WHEN** 用户打开任何使用全局顶栏的页面
- **THEN** 原顶栏长期余额按原有账户余额口径继续显示
- **AND** 顶栏余额卡组件直接显示当前可用未过期余额卡余额总和
- **AND** 顶栏余额卡组件的数字显示当前可用未过期余额卡数量

#### Scenario: 长期余额和余额卡余额分开展示
- **WHEN** 用户同时拥有长期余额和可用未过期余额卡余额
- **THEN** 顶栏长期余额不合并余额卡剩余额度
- **AND** 顶栏余额卡组件单独展示余额卡剩余可用总量

#### Scenario: 没有可用余额卡
- **WHEN** 用户没有任何可用未过期余额卡
- **THEN** 顶栏余额卡数量显示为 0
- **AND** 顶栏余额卡余额总和显示为 0

### Requirement: API 密钥页刷新联动顶栏资金信息
系统 SHALL 在 API 密钥页刷新数据时同步刷新全局顶栏余额卡信息和长期余额信息。

#### Scenario: 点击 API 密钥页刷新按钮
- **WHEN** 用户在 API 密钥页点击刷新按钮
- **THEN** 系统刷新 API key 列表和相关用量数据
- **AND** 系统重新获取全局顶栏余额卡摘要
- **AND** 系统重新获取全局顶栏长期余额信息
- **AND** 顶栏余额卡数量和余额总和更新为最新值
- **AND** 顶栏长期余额更新为最新值

#### Scenario: 顶栏资金信息刷新失败不阻塞 API key 刷新
- **WHEN** 用户在 API 密钥页点击刷新按钮且余额卡摘要或长期余额获取失败
- **THEN** API key 列表刷新流程不因顶栏资金信息失败而中断
- **AND** 系统保留合理的顶栏余额卡或长期余额加载/错误状态
```

