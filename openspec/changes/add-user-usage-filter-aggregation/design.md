## Context

普通用户 `UsageView` 在一次筛选操作中并发请求 `/usage`、`/usage/stats`、`/usage/dashboard/models` 和 `/usage/dashboard/snapshot-v2`。HC2 上用户 ID 1 在 2026-08-01 至 2026-08-31 的执行计划显示：第一页 20 条明细约 2.9ms，但精确总数约 4.0s、统计卡片约 7.9s、模型约 5.5s、趋势约 1.6s、分组约 1.2s；这些请求重复扫描约 18.5 万条 `usage_logs`。

页面需要保留精确总数、页码、筛选、排序、图表和导出，且项目后续会频繁合并上游。实现必须把冲突面压缩到极少的稳定接入点，不能重写前端或现有 Handler/Service 契约。

HC2 的实际维度基数表明，一张包含当前普通用户筛选维度和入口 Endpoint 的小时事实聚合表，在 30 天内可把 184,890 条原始记录压缩到约 5,075 条，适合用作透明加速层。

## Goals / Non-Goals

**Goals:**

- 保持普通用户使用记录页的页面、请求路径、响应字段、精确总数和数据结果不变。
- 让第一页精确总数及统计卡片、模型、趋势、分组和入口 Endpoint 主要读取紧凑聚合表。
- 让管理员、后台任务和未标记调用继续使用原 Repository 行为。
- 聚合未就绪、覆盖不足、时间边界不安全或筛选不支持时无条件回退旧 SQL。
- 将迁移、触发器、回填、聚合查询和差分测试放在新增文件中；现有生产文件只保留路由标记和 Repository 包装两个薄接入点。

**Non-Goals:**

- 不解决深度 `OFFSET` 的扫描成本。
- 不改变分页 UI、任意页跳转、CSV 导出或管理员统计。
- 不提供近似总数或最终一致的异步统计。
- 不替换已有 `user_dashboard_route_daily` 首页聚合表。

## Decisions

### 使用请求上下文标记限定普通用户路径

在用户 `/usage` 路由组增加一个轻量 middleware，仅向 `c.Request.Context()` 写入包私有标记。聚合装饰器只在该标记存在时启用；管理员路由、后台服务和测试调用不会被改变。

备选方案是在 Handler 中改用一组新 Service 方法，但需要修改多个现有调用点，增加上游合并冲突，因此不采用。

### 使用嵌入原 Repository 的独立装饰器

新增 `userUsageAnalyticsRepository`，嵌入 `*usageLogRepository` 并只覆盖：

- `ListWithFilters`
- `GetStatsWithFilters`
- `GetUsageTrendWithUsageFilters`
- `GetModelStatsWithUsageFiltersBySource`
- `GetGroupStatsWithUsageFilters`

未覆盖方法由嵌入的原 Repository 自动提供。`NewUsageLogRepository` 只把最终返回值包裹为装饰器，不扩展 `service.UsageLogRepository` 接口，也不修改 Handler/Service。

### 使用一张完整维度小时聚合表

新增 `user_usage_analytics_hourly`，键维度为：

- `user_id`
- `bucket_start`
- `api_key_id`
- `group_id`
- 规范化 requested model
- 规范化 request type
- `stream`
- `openai_ws_mode`（用于精确复现历史 `request_type=0` 的兼容筛选）
- `billing_type`
- 规范化 billing mode
- 规范化 inbound endpoint

指标包含请求数、输入/输出/缓存 Token、标准费用、实际费用、账号成本、时延总和和时延样本数。账号成本虽然不会由普通用户接口输出，但保留它可使装饰器覆盖的方法在内部结构上也与旧 Repository 完全一致。完整维度表可以用同一份数据回答精确总数和全部普通用户统计，避免维护多张重复宽表。生产基数验证显示加入入口 Endpoint 几乎不增加行数。

### 聚合只服务完全可证明等价的查询

Store 的 `CanServe` 必须同时验证：

- 请求来自普通用户 usage 上下文；
- 状态表 `ready=true`；
- 查询范围不早于 `coverage_start`；
- 起止时间与小时桶边界对齐；
- 仅使用普通用户页面现有筛选维度；
- 模型来源为 requested 或与普通用户默认语义等价。

任何条件不满足都调用嵌入的旧 Repository。这样半小时/45 分钟时区边界或未来新增筛选不会产生错误统计，只是暂时失去加速。

### 明细仍查原表，精确总数查聚合表

装饰器的 `ListWithFilters` 在快路径中独立构造与旧方法等价的过滤条件：

- `SUM(requests)` 从聚合表取得精确总数；
- 当前页明细仍按原排序、`LIMIT/OFFSET` 从 `usage_logs` 读取；
- 继续使用原有扫描和关联对象批量 hydration。

因此第一页避免昂贵 `COUNT(*)`，但深页行为和成本保持不变。

### 使用同步 statement-level 触发器保持精确

新增 INSERT、DELETE 及 UPDATE old/new statement-level 触发器，利用 transition table 先按聚合键分组，再执行 upsert/subtract。它不修改应用写入代码，并能正确处理批量写入、清理和管理修正。

异步队列会引入短暂不一致，无法维持现有精确总数和统计语义，因此不采用。

### 使用状态表和可恢复逐日回填

新增状态表记录 `ready`、`coverage_start`、`cursor` 和更新时间。部署后触发器立即维护新增数据，但装饰器继续回退旧 SQL；后台按日重建并推进 cursor，完成校验后才原子设置 `ready=true`。

回填沿用现有聚合 Store 的限速、可恢复和当前日重建模式。应用回滚时即使聚合表保留，旧版本也不会读取它。

## Risks / Trade-offs

- [完整维度键包含文本，可能增加索引体积] → 对所有维度做非空规范化，只保留普通用户页面需要的 requested model 和 inbound endpoint，并以 HC2 基数及写入基准验证容量。
- [新增同步触发器增加 usage-log 写入延迟] → 使用 statement-level transition table 批量聚合，每批只 upsert 唯一维度组合；上线前进行批量写入基准。
- [迁移后回填造成生产 IO] → `ready=false` 时功能走旧路径，按日期分段、可暂停、可恢复并在低峰限速执行。
- [聚合表达式与旧过滤语义漂移] → 使用差分集成测试覆盖 API Key、分组、模型、请求类型、stream、计费类型和计费模式组合；不支持的条件自动回退。
- [小时桶不能精确表示部分时区边界] → 只在起止时间整点对齐时启用；否则回退旧 SQL，保持正确性。
- [装饰器方法集影响可选接口类型断言] → 嵌入具体 `*usageLogRepository` 以保留全部现有方法，并对生产构造类型断言增加测试。

## Migration Plan

1. 创建聚合表、状态表、索引和 statement-level 触发器，状态保持 `ready=false`。
2. 部署包含装饰器的新版本；所有请求仍自动回退旧 SQL。
3. 后台按日回填保留范围，重建当前日并执行差分校验。
4. 原子设置 `ready=true`，普通用户 usage 请求自动切换聚合快路径。
5. 观察查询耗时、回退率、聚合差异和 usage-log 写入延迟。
6. 应用回滚时无需回滚 API 或前端；旧版本忽略聚合表。若触发器写开销异常，使用后续非破坏性迁移停用触发器并把状态设回未就绪。

## Open Questions

无。
