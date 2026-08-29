## ADDED Requirements

### Requirement: Ready-gated user Dashboard aggregation
系统 SHALL 仅在用户日聚合历史完成回填并通过校验后，使用聚合数据响应普通用户首页统计；否则 MUST 透明使用原有明细日志查询。

#### Scenario: Aggregation is not ready
- **WHEN** 用户日聚合状态尚未标记为 ready
- **THEN** `/api/v1/usage/dashboard/stats` 使用原有 `usage_logs` 查询并保持响应契约不变

#### Scenario: Aggregation is ready
- **WHEN** 用户日聚合状态已标记为 ready
- **THEN** `/api/v1/usage/dashboard/stats` 从日聚合表读取累计、今日、平均耗时和平台统计

### Requirement: Dashboard statistic parity
聚合读取 SHALL 保持现有普通用户首页的统计字段和口径，包括失败占位记录的总计口径、`actual_cost > 0` 的平台口径、应用时区自然日以及最近五分钟 RPM/TPM。

#### Scenario: Aggregated response matches raw response
- **WHEN** 同一用户的聚合数据与 `usage_logs` 覆盖相同记录
- **THEN** 两条读取路径返回相同的累计、今日、平均耗时和平台统计值

#### Scenario: Null duration values exist
- **WHEN** 用户的部分使用记录没有 `duration_ms`
- **THEN** 聚合平均耗时与原查询 `AVG(duration_ms)` 的非空计数口径一致

### Requirement: Incremental consistency
系统 MUST 对聚合启用后的 usage-log 插入、删除和更新执行事务内增量维护，使已就绪聚合不会因正常写入路径产生漂移。

#### Scenario: Usage logs are inserted
- **WHEN** 一个 SQL statement 插入一条或多条使用记录
- **THEN** 系统按用户、自然日和路由汇总增量并原子更新对应聚合桶

#### Scenario: Usage logs are deleted or updated
- **WHEN** 使用记录被删除或其聚合相关字段被更新
- **THEN** 系统扣减旧贡献，并在更新场景增加新贡献

### Requirement: Resumable verified backfill
系统 MUST 在后台逐日、可恢复地回填现有使用记录，并 MUST 在发布聚合读取前验证聚合结果。

#### Scenario: Multiple instances start together
- **WHEN** 多个应用实例同时尝试执行历史回填
- **THEN** PostgreSQL advisory lock 保证同一时间只有一个实例推进回填水位

#### Scenario: A daily verification fails
- **WHEN** 某日聚合结果与原始记录校验不一致
- **THEN** 系统不设置 ready，并在后续周期重试回填

### Requirement: Retention-aware aggregation
聚合读取 MUST 排除已经从 `usage_logs` 保留窗口中移除的数据，包括直接删除旧分区而未触发逐行删除的情况。

#### Scenario: An old usage partition is dropped
- **WHEN** 原始日志最早保留日期向后推进
- **THEN** 用户首页不再统计该日期之前的聚合桶，并由后台维护清理陈旧桶

### Requirement: Minimal integration surface
实现 SHALL 保持现有 HTTP、Service 和前端契约不变，并将新增统计实现集中在独立 migration、store 和测试文件中。

#### Scenario: Upstream code is merged later
- **WHEN** 上游修改普通用户 Dashboard 的 Handler、Service 或前端
- **THEN** 本变更不要求这些层存在定制接口或定制响应字段
