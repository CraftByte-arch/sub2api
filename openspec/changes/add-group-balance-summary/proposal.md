## Why

分组标题目前只显示账号数量，管理员还需要逐个查看账号才能判断一个分组中启用和未启用账号分别还剩多少可用余额。将两类余额在分组汇总处展示，可以快速判断分组的实际可用容量和停用账号的潜在储备。

## What Changes

- 在侧车分组标题的汇总区域增加“启用余额”和“未启用余额”。
- 汇总沿用 `/api/overview` 中每个账号已有的管理员可用余额投影，不新增上游请求或修改 Sub2API。
- 仅对当前分组成员聚合；启用/未启用分类沿用现有分组账号状态判断。
- 对余额不足、无限额度和余额暂不可用等情况保留明确文字状态，不把未知余额当作 0。

## Capabilities

### New Capabilities

- `group-balance-summary`: 在每个分组标题显示启用与未启用账号的可用余额汇总。

### Modified Capabilities

<!-- No existing product capability is changed; this is an additive overview projection. -->

## Impact

- 侧车前端：扩展分组汇总渲染和样式。
- 侧车后端：在 overview 响应中增加按分组聚合的余额摘要（兼容旧字段）。
- Sub2API：不修改代码、数据库或计费逻辑。
