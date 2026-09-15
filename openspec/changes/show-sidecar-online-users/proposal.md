## Why

管理员目前只能看到账号和汇总用量，无法快速判断当前是否有人正在使用服务，也无法在不离开侧车页面的情况下定位最近活跃用户。增加在线用户概览可以把“最近十分钟有调用”的实时信号与用户今日消耗放在同一处，便于运维和容量判断。

## What Changes

- 在侧车总览中增加在线使用人数指标，在线窗口固定为最近 10 分钟。
- 在每个分组标题中显示该分组最近 10 分钟的去重在线人数，同时保留顶部全站在线人数。
- 点击在线人数后打开管理员弹窗，列出窗口内活跃用户、最后调用时间和今日消耗。
- 通过现有 Sub2API 管理员 HTTP 接口读取用量数据并在侧车聚合，不修改 Sub2API 主服务或数据库结构。
- 今日消耗同时保留金额和 token 数，用户名称优先显示用户名，否则显示邮箱；无法识别身份的记录不展示为用户。
- 在线数据读取失败时不影响既有账号/分组总览，页面显示可重试的不可用状态。

## Capabilities

### New Capabilities

- `sidecar-online-users`: 侧车按最近十分钟调用记录聚合在线用户，并提供管理员查看详情的交互。

### Modified Capabilities

<!-- No existing main capability requirements are changed. -->

## Impact

- `account-auto-scheduler/internal/core/online_users.go`：读取近期请求的 `group_id`，按“用户 + 分组”去重汇总，并保留未分组与部分数据状态。
- `account-auto-scheduler/internal/model/online_users.go`：增加在线用户及今日消耗的数据模型。
- `account-auto-scheduler/internal/web/server.go`：在受管理员鉴权保护的总览响应和在线用户详情接口中暴露数据。
- `account-auto-scheduler/internal/web/static/index.html`、`app.js`、`app.css`：增加指标卡、分组在线人数徽标、弹窗、加载/错误/空状态和响应式样式。
- 仅调用现有 `/api/v1/admin/usage`、`/api/v1/admin/dashboard/users-usage` 等管理员接口；不改变 Sub2API 生产代码、数据表或其他容器。
