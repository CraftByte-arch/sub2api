## Context

普通用户首页的 `/api/v1/usage/dashboard/stats` 当前直接从 `usage_logs` 计算用户累计、今日、平均耗时和平台拆分。累计与平台查询均没有历史范围上限，高用量用户会反复扫描大量明细数据。项目已经使用 `api_key_usage_daily` 和 `account_usage_stats_daily` 的“独立 store + 后台回填 + ready 水位 + 原查询回退”模式，因此本变更沿用该模式，减少新的架构概念和上游合并冲突。

约束包括：不改变 HTTP 契约；不修改 Handler、Service、前端或依赖注入；不把统计逻辑散落到多个 usage-log 写入 SQL；上线期间必须允许旧查询继续工作；统计口径必须与现有 Dashboard 一致。

## Goals / Non-Goals

**Goals:**

- 将普通用户首页的全历史与平台聚合查询从 `usage_logs` 转移到紧凑的日聚合表。
- 将新增实现集中在独立 Repository store 和独立 migration 中。
- 在回填完成前透明使用现有查询，避免迁移窗口造成错误数据。
- 保持累计、今日、平均耗时和有效平台拆分的现有统计口径。
- 对增量插入、删除、更新和分区保留清理保持可恢复的一致性。

**Non-Goals:**

- 不优化 `/api/v1/usage` 最近使用记录列表。
- 不优化 Dashboard trend、models 或 groups 接口。
- 不改变 API Key 数量及最近五分钟 RPM/TPM 的实时计算方式。
- 不新增前端字段、配置项或外部依赖。

## Decisions

### 使用独立的用户日路由聚合表

新增 `user_dashboard_route_daily`，主键为 `user_id + bucket_date + group_id + account_id`。表中保存请求、Token、费用、耗时，以及与现有 `actual_cost > 0` 成功口径对应的平台统计字段。

保留 `group_id/account_id` 而不是写死平台字符串，使读取时仍可使用现有“组合分组取账号平台，否则分组平台优先”的表达式。这样分组或账号平台配置改变后，历史展示不会永久固化为旧平台。

备选方案是只按 `user_id + bucket_date` 聚合，但它无法生成现有 `by_platform` 响应；直接存平台字符串虽然更紧凑，但会固化可变配置，因此不采用。

### 使用数据库 statement-level transition trigger 维护增量

migration 在 `usage_logs` 上安装 INSERT、DELETE 和 UPDATE statement trigger。每个 SQL statement 先对 transition table 分组，再对日聚合表执行一次批量 upsert。

该方式覆盖已有和未来新增的日志写入路径，不需要修改多个 `usage_log_repo_insert.go` SQL。DELETE 维护与原始日志一致的保留语义；UPDATE 使用旧值扣减、新值增加，避免未来字段修正造成漂移。

### 使用独立 store 完成读取、回填和回退

新增 `userDashboardStatsStore`：

- `Get` 先检查 `user_dashboard_route_daily_state.ready`。
- 未就绪时返回“未处理”，由现有 `GetUserDashboardStats` 原实现继续查询。
- 就绪后从聚合表构造完整的 `UserDashboardStats`，API Key 数量和五分钟性能指标仍执行小范围实时 SQL。
- 生产 Repository 构造时启动单例后台回填；测试构造器不自动启动，保持测试确定性。

现有 `GetUserDashboardStats` 只增加旁路调用，并将原函数体保留为 legacy helper，避免重写或删除上游代码。

### 逐日回填并在最终切换前校验

状态表记录 `ready`、`coverage_start` 和 `cursor`。后台任务使用 PostgreSQL advisory transaction lock 保证多实例单写，按自然日重建并校验。最终重建当前日时锁定聚合表，使并发 usage-log trigger 在提交后继续追加，避免切换窗口丢数。

历史起点从 `MIN(usage_logs.created_at)` 获取，不硬编码 30 或 90 天，以兼容自定义日志保留期。回填未完成或校验失败时不会启用聚合读取。

### 保留分区清理兼容性

普通 DELETE 会触发扣减；直接 DROP 旧分区不会触发 DELETE trigger。聚合读取使用原始表最早保留日期作为下界，避免后台维护尚未运行时返回已删除分区的陈旧桶。store 完成回填后还会周期性删除下界之前的聚合桶。

## Risks / Trade-offs

- [高并发用户会竞争当天同一路由聚合行] → statement trigger 先分组，每条原始日志只影响一个日路由桶，不增加模型或 endpoint 维度。
- [首次部署回填期间旧查询仍可能超时] → 回填逐日运行且保持旧查询回退；完成后自动切换，无需停机。
- [聚合 trigger 增加日志写入成本] → 使用 statement-level 批量 upsert，并通过写入路径测试和集成测试验证。
- [分区 DROP 不触发删除 trigger] → 读取时应用原始数据保留下界，并由后台维护最终清除陈旧桶。
- [新增 migration 编号可能与未来上游编号接近] → 使用独立、描述唯一的 migration 文件；不修改任何已存在 migration。

## Migration Plan

1. 应用新 migration，创建表、状态行、索引和 trigger；此时 `ready=false`。
2. 应用启动后后台逐日回填并校验，线上请求继续使用旧 SQL。
3. 当前日最终校验成功后原子设置 `ready=true`，随后请求自动使用聚合读取。
4. 若聚合异常，可将状态行 `ready` 设为 `false`，立即恢复旧查询；不需要回滚 API 或代码契约。
5. 后续 forward migration 可移除聚合对象，但本次不提供破坏性 down migration。

## Open Questions

无。
