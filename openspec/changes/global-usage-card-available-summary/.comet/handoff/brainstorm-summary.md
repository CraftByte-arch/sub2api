# Brainstorm Summary

- Change: global-usage-card-available-summary
- Date: 2026-06-30

## 确认的技术方案

新增用户侧余额卡 summary 能力，由服务端按现有可用余额卡口径返回可用卡数量和剩余可用总量；前端新增共享的余额卡 summary 状态，`UsageCardMini` 从该状态读取展示；API 密钥页刷新按钮同时触发 API key 数据刷新、余额卡 summary 刷新和 `authStore.refreshUser()` 长期余额刷新。顶栏余额卡剩余可用总量采用 `$0.00` 展示，悬浮层明细继续保留更高精度。

## 关键取舍与风险

- 候选取舍：服务端计算 summary，避免前端复制可用卡口径。
- 候选取舍：长期余额保持 `authStore.user.balance` 原口径，余额卡剩余可用总量在 `UsageCardMini` 内单独展示。
- 风险：顶栏空间有限。已确认顶栏余额卡总量使用 `$0.00`，降低空间压力；悬浮层明细可保留 `$0.0000`。

## 测试策略

- 后端覆盖 active、expired、exhausted、suspended、cancelled、future、deleted 卡的 summary 统计。
- 前端覆盖顶栏余额卡数量和金额展示、长期余额不变、API key 页刷新联动两个顶栏刷新路径。

## Spec Patch

候选：无。当前 OpenSpec delta spec 已覆盖长期余额不变、余额卡分开展示、API key 页刷新联动两类顶栏资金信息。

## 剩余问题

无。用户已确认整体技术设计方案。
