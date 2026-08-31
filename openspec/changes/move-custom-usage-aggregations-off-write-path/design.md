## Context

当前分支在 `usage_logs` 上维护四类自定义聚合：API Key 日费用、Account 日维度统计、普通用户首页日路由统计和普通用户使用记录小时统计。除 API Key 由 INSERT CTE 维护外，其余均由 statement-level trigger 同步 upsert；Account 每条日志会展开总计、模型、入口 Endpoint、上游 Endpoint 四个维度，HC2 采样中是写放大最大的对象。

这些聚合的读取优化是有效的，但同步维护把计算、索引更新和 WAL 都放进请求结算事务，对 1C/2G PostgreSQL 造成明显的脉冲式 CPU 满载。项目还要持续合并 `origin/main`，因此不能把新的生命周期逻辑继续塞进基础 Repository、Service 或多条写入 SQL。

约束如下：HTTP、页面、精确统计、分页、导出和请求完成时间分桶语义均不能改变；已发布的 `195/196/231/232` migration 不能修改；main 自带的分组汇总触发器不能移除；任何写路径卸载必须先部署能同时读取聚合历史和原始尾部的兼容版本。

## Goals / Non-Goals

**Goals:**

- 从正常 `usage_logs` 写事务移除自定义聚合 upsert，优先消除 Account 的四倍行更新。
- 已关闭桶使用聚合表，尚未关闭尾部和脏桶使用 `usage_logs`，在一个数据库快照内合并为与旧 SQL 等价的精确结果。
- 后台每次只处理一个关闭日桶或有限数量的关闭小时桶，禁用查询并行并限制 SQL 超时。
- 历史 INSERT、UPDATE、DELETE 能立即被读取路径识别为脏桶，并可由后台重建修复。
- 将四个 Store、维护协调和装饰器放在新增/自定义文件中，把 `origin/main` 来源文件的最终差异降到稳定的一行 Repository 包装和现有的一行 user usage middleware。
- 支持按 Account → 最近使用小时统计 → 首页日统计 → API Key 日统计的顺序逐项发布和观察。

**Non-Goals:**

- 不优化使用记录的深度 `OFFSET` 分页。
- 不改变任何统计字段、筛选维度、成功判定、时区边界或 API 响应结构。
- 不移除 `usage_logs`，不引入近似计数，也不接受正常读取的最终一致性误差。
- 不改动 Account Performance 的 minute/hour rollup；它继续使用已经实现的增量关闭小时方案。
- 第一轮生产切换不卸载另外三类统计的同步维护。

## Decisions

### 使用一个组合装饰器承载四个独立 Store

新增 `usageAggregationRepository`，嵌入原 `*usageLogRepository`，并持有 `apiKeyUsageDailyStore`、`accountUsageStatsStore`、`userDashboardStatsStore` 和 `userUsageAnalyticsStore`。装饰器只覆盖已经存在于 `service.UsageLogRepository` 的查询方法，其余方法通过嵌入自动透传。

生产构造器只负责把基础 Repository 包装一次。Account Service 恢复调用原有 `GetAccountUsageStats`，不再扩展接口；首页和 API Key 的快路径也从基础 Repository 方法移到装饰器。这样 `usage_log_repo.go` 的结构体和测试构造器可恢复到 `origin/main` 形态，四类功能仍保持相互独立。

备选方案是在 Service/Handler 增加四组专用接口，但会增加多个上游高冲突调用点，因此不采用。

### 使用 `closed_before` 表示聚合历史水位

每个状态表增加 `closed_before`：所有严格早于该值的日桶/小时桶可由聚合表服务；从该值开始的开放尾部必须查询 `usage_logs`。水位只在桶重建、校验和提交成功的同一事务内单调推进。

Account 第一阶段读取按请求区间拆分：

- `[start, min(end, closed_before))` 从 `account_usage_stats_daily` 读取；
- `[max(start, closed_before), end)` 从 `usage_logs` 读取；
- 位于历史区间但出现在 dirty 表中的日期不读聚合表，改读对应原始日志；
- 两部分按原有四个维度求和后复用同一个响应构造器。

这个规则会正确处理跨零点长请求。usage log 的 `created_at` 在请求完成/结算时写入，因此 23:50 发起、次日 01:00 完成的请求完整归入次日开放尾部，不会漏记或拆分。

备选方案是在当前桶继续同步更新聚合表，但仍会让高峰写入竞争热点行，因此不采用。

### 脏桶是精确读取的一部分，而不只是后台队列

新增独立 dirty 表。历史 INSERT、UPDATE、DELETE 的 statement trigger 只记录受影响的关闭桶，不计算聚合指标。正常当前时间 INSERT 因不早于 `closed_before` 而不会产生 dirty 行。

混合读取 SQL 在同一 statement snapshot 中：聚合部分排除 dirty 桶，原始部分包含开放尾部和 dirty 桶。这样历史改写提交后，下一次读取立即精确；后台修复只负责把脏桶重新变成紧凑聚合数据，不承担正确性窗口。

备选方案是让读取继续使用脏聚合直到后台修复，但会短暂改变精确统计语义，因此不采用。

### 后台维护只重建关闭桶

维护循环使用跨实例 PostgreSQL advisory transaction lock，并使用一个所有自定义 usage 聚合共享的维护锁避免多个 Store 同时扫描 `usage_logs`。事务内执行：

- `SET LOCAL max_parallel_workers_per_gather = 0`；
- `SET LOCAL statement_timeout = '20s'`；
- 优先修复一个 dirty 桶，否则关闭一个水位之后的日桶；小时 Store 每轮最多关闭两个小时桶；
- 重建后执行指标校验，通过后删除 dirty 标记或推进 `closed_before`。

循环之间保留短暂停顿并在错误后退避。它不会周期性扫描完整聚合表或完整 `usage_logs` 保留范围。首次安装或原状态未就绪时仍按受限桶逐步回填，查询在覆盖不足时回退旧 SQL。

### 使用两阶段 forward migration

阶段一 migration 只增加状态水位和 dirty 元数据，保留现有同步维护。代码先部署混合读取能力；此时即使新读取出现问题，也能把状态切回未就绪并使用旧 SQL。

阶段二在 HC2 验证后，以新的 migration 删除 Account 同步聚合 trigger/function，安装轻量 dirty trigger，并启用持续关闭日维护。后续三个 Store 各自使用独立 forward migration 切换，不能把四个写路径一次性同时改变。

两个阶段不得放进同一个首次部署。阶段二之后允许回滚到阶段一兼容版本，但不能直接回滚到当前只读整段聚合表、依赖同步 trigger 的版本。

### Account 查询 SQL 使用单次 UNION 聚合

Account 混合读取使用一个 SQL statement 将“非脏关闭日聚合行”和“开放尾部/脏日原始展开行”`UNION ALL`，再按维度统一 `GROUP BY`。这既保证同一 snapshot 的脏桶切换一致性，也避免分别查询后在修复事务边界发生重计或漏计。

模型、入口 Endpoint、上游 Endpoint 和总计仍使用现有规范化表达式，费用与时延字段也沿用原表口径。结果继续交给现有 `buildAccountUsageStatsResponse`，不重新实现 API 结构。

## Risks / Trade-offs

- [dirty INSERT trigger 仍会经过每个写 statement] → 只做日期去重和条件插入，不展开四个维度、不更新聚合索引；用写入测试确认正常当前桶不会新增 dirty 行。
- [阶段二部署后回滚到过旧版本会读到不再实时维护的当前聚合桶] → 强制两阶段发布，并保留阶段一镜像作为唯一直接回滚目标。
- [维护任务在 1C 数据库上与在线查询竞争] → 共享维护锁、单桶上限、禁并行、20 秒超时、错误退避，并优先从最重的 Account 单独发布观察。
- [状态水位或 dirty 表异常可能导致错误切分] → 覆盖不足、状态缺失或非法水位一律回退 legacy SQL；维护推进前做差分校验。
- [历史分区直接 DROP 不触发 DELETE trigger] → 维护任务根据原始日志保留下界回收超界聚合桶并收紧可服务覆盖；未能证明覆盖时回退旧 SQL。
- [组合装饰器覆盖方法可能遗漏可选接口] → 嵌入具体基础 Repository，并增加编译期接口断言和构造器类型断言回归测试。

## Migration Plan

1. 新增组合装饰器并把现有四个 Store 移入装饰器，恢复基础 Repository 和 Account Service 的 main 形态；先跑差分测试确认行为不变。
2. 新增 Account 阶段一 migration：增加 `closed_before` 和 `account_usage_stats_dirty_days`，从已就绪状态安全初始化水位；部署混合读取版本，但保留原同步 trigger。
3. 在 HC2 对 Account 统计做原始/混合差分，观察查询耗时、CPU、WAL 和写延迟；异常时将状态 `ready=false` 或回滚到部署前版本。
4. 新增并部署 Account 阶段二 migration：删除 `trg_account_usage_stats_daily_insert/delete` 与旧 delta function，安装历史变更 dirty trigger；后台开始逐日关闭和修复。
5. 观察至少一个完整业务周期；确认 Account 表更新次数、数据库 CPU 峰值和统计差异符合预期后，再按独立 migration 依次迁移另外三类统计。
6. 每次后续切换都保留上一阶段兼容镜像；不修改或回滚已经应用的旧 migration 文件。

## Open Questions

无。第一轮实现和发布范围固定为 Account；另外三类只完成装饰器收口，不在本轮卸载同步写入。
