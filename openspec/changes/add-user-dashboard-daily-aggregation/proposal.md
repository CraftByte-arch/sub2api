## Why

普通用户首页统计当前会按用户重复扫描 `usage_logs` 全部历史数据，并执行累计、今日和平台维度聚合。高用量用户的数据增长后，请求容易超过接口超时时间，也会持续放大数据库负载。

## What Changes

- 新增按用户、自然日和实际路由维度维护的首页统计聚合数据。
- 新增独立的聚合读取与历史回填组件，聚合未就绪时透明回退现有原始日志查询。
- 保持现有 `/api/v1/usage/dashboard/stats` 请求和响应契约不变。
- 保留最近五分钟 RPM/TPM 和 API Key 数量的实时查询口径。
- 维护日志插入、删除及历史保留清理后的聚合一致性。
- 不改变最近使用记录列表、趋势图和模型统计查询。

## Capabilities

### New Capabilities

- `user-dashboard-daily-aggregation`: 为普通用户首页提供可回填、可校验、可回退的日聚合统计读取能力。

### Modified Capabilities

无。

## Impact

- 数据库新增用户首页日聚合表、状态表和增量维护触发器。
- Repository 新增独立统计 store，并在现有用户 Dashboard Repository 方法中增加一个旁路调用。
- API、Service、Handler、前端和依赖注入契约保持不变。
- 上线后后台逐日回填历史数据；回填完成前沿用现有查询。
