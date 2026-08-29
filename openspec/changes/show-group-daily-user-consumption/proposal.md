## Why

管理员可以查看最近十分钟的在线用户，但还无法快速判断某个分组当天的用户消耗构成。为便于识别高消耗用户、核对分组流量和处理异常用量，需要在每个分组提供按今日实际扣费排序的用户 Top 20 视图。

## What Changes

- 在每个分组的操作区增加“今日用户 Top 20”入口。
- 点击入口后打开管理员弹窗，展示当前分组当天消耗最高的前 20 位用户。
- 每行显示用户身份、今日实际消耗、请求数和 Token 数，并显示统计日期与更新时间。
- 使用现有 Sub2API 管理员分组用户统计接口，保持分组倍率和实际扣费口径，不修改 Sub2API 主服务、数据库或计费逻辑。
- 无数据、统计失败和部分数据场景均提供明确状态与重试操作。

## Capabilities

### New Capabilities

- `sidecar-group-user-consumption`: 侧车按分组查看当天用户消耗 Top 20。

### Modified Capabilities

<!-- No existing spec-level requirements change. -->

## Impact

- 侧车 `internal/core` 增加按分组读取用户统计的 HTTP 客户端方法和数据模型。
- 侧车 `internal/web` 增加受管理员鉴权保护的分组统计接口。
- 侧车总览页面的分组操作区和响应式弹窗增加入口与展示逻辑。
- 仅调用现有 `/api/v1/admin/dashboard/user-breakdown` 管理员接口；HC2 部署另行进行，本次实现不触碰 Sub2API 主服务。
