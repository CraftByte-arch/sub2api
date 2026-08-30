## ADDED Requirements

### Requirement: Transparent user usage analytics acceleration
系统 SHALL 在不改变普通用户使用记录页、现有 API 路径、请求参数、响应字段和精确数值语义的前提下，通过聚合数据加速筛选后的总数和统计查询。

#### Scenario: Existing page applies filters
- **WHEN** 普通用户在使用记录页修改日期、API Key、分组、模型、请求类型、计费类型或计费模式筛选
- **THEN** 页面继续使用现有接口并得到与旧明细 SQL 一致的列表、精确总数、统计卡片、模型、趋势、分组和入口 Endpoint 数据

#### Scenario: Existing export and pagination
- **WHEN** 用户翻页、排序或执行 CSV 导出
- **THEN** 系统保持现有页码、排序、精确总数和导出行为，且明细记录仍从 `usage_logs` 读取

### Requirement: Exact synchronous aggregation
系统 MUST 按普通用户现有筛选维度同步维护时间聚合数据，并 MUST 在 INSERT、UPDATE 和 DELETE 后保持请求数、Token、费用及平均时延所需指标与 `usage_logs` 一致。

#### Scenario: Usage logs are inserted in a batch
- **WHEN** 一个 statement 写入多条 usage logs
- **THEN** 系统先按聚合维度合并增量并只对对应聚合行执行 upsert

#### Scenario: Usage logs are deleted or corrected
- **WHEN** usage logs 被清理、删除或更新
- **THEN** 系统从旧维度扣除贡献并向新维度增加贡献，聚合结果继续与原始明细一致

### Requirement: Safe readiness and fallback
系统 MUST 仅在聚合回填完成、覆盖查询范围且查询可被精确表示时使用聚合快路径；其他情况 MUST 自动执行变更前的 Repository SQL。

#### Scenario: Aggregation is not ready
- **WHEN** 状态表尚未标记 ready、回填失败或查询早于 coverage_start
- **THEN** 系统无感回退旧 SQL，现有页面功能和数据结果不受影响

#### Scenario: Unsupported or unsafe filter
- **WHEN** 查询包含聚合表未覆盖的筛选条件、模型来源或非整点时间边界
- **THEN** 系统回退旧 SQL，不返回近似或截断统计

### Requirement: Resumable production backfill
系统 SHALL 提供可恢复、可限速的分段回填，并 MUST 在完成差分校验前保持聚合读取关闭。

#### Scenario: Backfill is interrupted
- **WHEN** 应用重启或单个日期回填失败
- **THEN** 后续运行从状态表 cursor 继续，不需要清空已完成日期或阻塞线上请求

#### Scenario: Backfill completes
- **WHEN** 保留范围、当前日期和增量触发数据均完成校验
- **THEN** 系统原子设置 ready=true，后续符合条件的普通用户查询自动使用聚合表

### Requirement: Isolated integration boundary
系统 SHALL 将聚合实现放在新增迁移和新增代码文件中，并 MUST 限定聚合加速仅作用于普通用户 usage 请求上下文。

#### Scenario: Admin or background service queries usage
- **WHEN** 管理员接口、后台任务或未标记上下文调用相同 Repository 方法
- **THEN** 系统继续执行原 Repository 实现，不改变其字段、统计维度或缓存行为

#### Scenario: Upstream code is merged
- **WHEN** 项目同步上游前端、Handler 或 Service 代码
- **THEN** 聚合功能的接入冲突面仅限路由上下文标记和 Repository 构造包装，不要求修改页面或公共 Repository 接口
