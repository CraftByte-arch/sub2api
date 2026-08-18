## Why

管理员在“分组与账号”页面点击“管理账号”时，目前只能看到账号、绑定状态和分组归属，无法在做绑定决策前快速比较账号的最终计算倍率与可用余额。把这两项关键运行数据放进同一列表，可以减少来回切换页面和误绑定风险。

## What Changes

- 在“管理分组账号”弹窗的每个 API Key 账号行中增加“最终倍率”和“可用余额”信息。
- 复用总览接口已经提供的最终倍率与管理员余额投影，不新增上游请求、不改变绑定保存逻辑。
- 对未绑定、倍率未计算、余额未知/不限/不足等状态提供明确的文字状态和悬浮说明。
- 保持现有平台兼容筛选、搜索、批量勾选和保存行为不变，并优化弹窗列表在小屏下的可读性。

## Capabilities

### New Capabilities

- `binding-account-metrics`: 在分组账号管理列表中展示每个 API Key 账号的最终倍率和可用余额及其状态。

### Modified Capabilities

<!-- No existing product requirement is changed; this is an additive view capability. -->

## Impact

- 侧车前端：`account-auto-scheduler/internal/web/static/app.js`、`app.css`，必要时调整 `index.html` 列表语义。
- 侧车已有 `/api/overview` 数据模型作为数据来源；不修改 Sub2API 代码、账号绑定 API 或上游同步流程。
- 增加前端静态渲染测试/检查，覆盖可用、未知和不足状态。
