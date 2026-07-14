## Context

Sub2API 已在 handler 层实现基于 `UpstreamFailoverError` 的账号排除、重新选号和最大换号次数控制，也在各流式解析器中记录 `first_token_ms`。但是当前流式实现会在真实内容 token 前写入响应头、生命周期事件或 keepalive；一旦底层 writer 已提交，handler 就不能安全地把后续输出切换到另一个账号。

现有 `StreamTimeoutSettings` 只控制“流数据间隔超时后的账号处置”，实际超时阈值来自进程配置，且触发时响应通常已经开始。首 token 超时需要独立设置和不同语义：它只在客户端尚未收到语义响应时生效，超时后必须取消本次上游 attempt，并允许现有 failover 循环重放完整请求。

首期涉及的入口是 OpenAI Responses、OpenAI Chat Completions 和 Anthropic Messages 的 HTTP 文本流式请求。它们可能被路由到不同上游平台或兼容转换管线，因此能力以入站协议和语义事件为边界，而不是以账号平台为边界。

## Goals / Non-Goals

**Goals:**

- 提供默认关闭、1-300 秒、保存后对新请求实时生效的管理员配置。
- 在三个目标 HTTP 流式协议中统一实现“首个客户端可见语义 token”超时。
- 在首 token 前把响应头和响应体视为一个可提交或回滚的 attempt 事务。
- 超时后立即中止上游 I/O，排除当前账号并复用现有 failover 上限换号。
- 保持客户端取消、账号并发槽释放、调度统计、错误响应和既有流间隔超时语义正确。
- 在不依赖现有 usage 成功日志作为分母的前提下，准确统计受控 attempt 与最终 request 的 TTFT/其他失败率，并提供每账号视图。

**Non-Goals:**

- 不覆盖 WebSocket、非流式、图片、视频、批处理或后台任务。
- 不因首 token 超时直接把账号设置为 error 或 temp unschedulable。
- 不在没有其他候选账号时追加同账号重试。
- 不支持已经向客户端提交语义 token 后的跨账号续流。
- 不改变现有账号选择权重、最大换号次数和 stream data interval timeout 配置。

## Decisions

### 1. 使用独立的首 token 超时设置

新增 `FirstTokenTimeoutSettings`，包含 `enabled` 和 `timeout_seconds`，使用独立 settings key 和管理员 API。默认关闭；读取异常或非法历史值时回退默认关闭；写入时严格校验 1-300 秒。

不扩展 `StreamTimeoutSettings`：两者触发阶段、失败处理和账号影响不同，合并会让现有“累计超时后临时下线”的配置语义变得含糊。

### 2. 每个账号 attempt 独立计时

TTFT 定时器在账号已选定、请求即将发往上游时启动，不包含认证、计费检查、用户并发排队或选号时间。每次 failover 选择新账号后创建新的 attempt context 和定时器。

attempt 使用 `context.WithCancelCause`，定时器获胜时以专用 `ErrFirstTokenTimeout` 取消 context。上游 HTTP 请求必须绑定该 context，使等待响应头和读取响应体都能被立即中断。首 token 到达、attempt 正常结束或客户端取消时必须停止定时器，避免 goroutine 和 timer 泄漏。

### 3. 使用事务化流式 writer 门控首 token 前输出

为 eligible attempt 包装 `gin.ResponseWriter`：

- `Header()` 操作写入 attempt 本地 header 副本，不提前污染底层响应头。
- `Write`、`WriteString` 和 `Flush` 在提交前只写入有界内存缓冲区，`Written()` 对外仍表示底层客户端是否已收到内容。
- 协议解析器识别首个语义 token 后原子调用 `Commit`：停止 TTFT 定时器，把本地 headers 和缓冲事件按原顺序写到底层 writer，再继续直写。
- 超时或首 token 前 failover 时调用 `Rollback`：丢弃缓冲和本地 headers，恢复原 writer，使下一账号从干净响应状态开始。

首 token 前缓冲上限固定为 256 KiB。超过上限视为异常的 prelude 响应并走可观测的 failover 错误，避免不受控内存增长；该上限首期不开放后台配置。

相比只包裹 `context.WithTimeout`，事务化 writer 能保证 keepalive、metadata 和失败账号响应头不会让 `c.Writer.Written()` 提前变为 true。相比在每个解析器手写 byte slice，它能统一处理 headers、Flush 和 handler 的 stream-started 判断。

### 4. 首 token 由协议解析器报告语义事件

共享 attempt controller 只负责状态机，不解析协议。各解析器在已有 TTFT 计时点附近调用统一的 `MarkFirstToken(ctx)`：

- OpenAI Responses：首个非空、客户端可见的文本、reasoning 或工具调用参数 delta。
- OpenAI Chat Completions：首个包含非空 content、reasoning content 或工具/函数调用增量的 choice delta；role-only chunk 不计入。
- Anthropic Messages：首个包含非空 text、thinking 或 input-json 增量的 content block 事件；纯 message lifecycle、usage 和 ping 不计入。

工具调用和 reasoning 输出属于客户端可消费的语义进展，应结束 TTFT 计时，避免模型正在正常输出非文本内容时被误杀。上游在首 token 前返回终止、拒绝或协议错误时，停止定时器并继续使用现有空响应、silent refusal 或上游错误分类，不伪装成 TTFT 超时。

### 5. 超时转换为不可同账号重试的 failover 错误

当 `context.Cause(attemptCtx)` 为 `ErrFirstTokenTimeout` 且 writer 尚未提交时，service 返回：

- `StatusCode: 504`
- 稳定错误类型 `first_token_timeout`
- `RetryableOnSameAccount: false`
- 当前 attempt 的账号、协议、模型、阈值和耗时仅进入内部日志/Ops 字段，不进入对外错误正文。

handler 复用现有逻辑把当前账号加入 `failedAccountIDs`，释放账号槽，重新选号。所有候选账号或 `maxAccountSwitches` 耗尽后，把最后一个 TTFT 错误作为 504 返回客户端。客户端 context 已取消时不再 failover。

### 6. 调度、计费与失败分类

每个 TTFT 超时 attempt 调用现有账号调度结果上报，记为失败，但不写正常 usage log、不扣除 Sub2API 用户余额，也不直接修改账号持久状态。最终成功 attempt 按现有流程记录 usage 和计费。

新增结构化事件和计数指标，至少包含 endpoint/protocol、platform、account_id、model、timeout_seconds、attempt_index 和 switch_count。不得记录请求正文、凭据或失败上游的内部地址。

上游服务可能仍对已经开始计算但被本地取消的 attempt 计费，这是主动延迟保护不可消除的外部成本，管理员通过显式启用该功能接受此权衡。

失败分类在 TTFT 组件内统一归一化为 `rate_limit`、`auth`、`upstream_4xx`、`upstream_5xx`、`transport`、`stream_idle_timeout`、`protocol` 或 `other`。客户端取消单独记录为 `client_canceled`，但不进入失败率分母。

### 7. 使用独立小时聚合表记录受控样本

新增 `first_token_timeout_stats_hourly`，只记录开关开启且实际进入 TTFT controller 的流量，不从现有 Ops/usage 日志回填。表使用 `attempt` 和 `request` 两种 scope：

- attempt outcome 为 `success`、`ttft_timeout`、`other_failure` 或 `client_canceled`，账号维度使用实际账号。
- request outcome 为 `success`、`recovered_after_ttft`、`ttft_exhausted`、`other_failure` 或 `client_canceled`，使用零值账号/平台哨兵，避免把多账号 request 错误归因到单一账号。
- 聚合维度为小时、scope、账号、入站协议、平台、模型、阈值快照、outcome 和 failure kind；计数器保存样本数、包含语义首 token 的样本数、TTFT 总毫秒、TTFT 最大毫秒和受 TTFT timeout 影响的 request 数。

独立 recorder 用有界 channel 接收热路径事件，在单 goroutine 内按维度合并，每 5 秒或达到批量阈值后通过 PostgreSQL UPSERT 原子累加。队列满或 flush 失败时只增加 dropped count，不影响请求结果；API 返回当前实例 dropped count、最后成功 flush 时间和 completeness 状态。多实例分别聚合并依靠 UPSERT 加法合并。

数据固定保留 90 天，由同一独立服务每日执行幂等清理。统计不保存请求正文、响应正文、凭据或内部地址。

### 8. 配置与统计放在独立管理员页面

新增 `/admin/ttft` 和侧边栏“首 Token 监控”入口。页面顶部使用 toggle、1-300 秒数输入、保存按钮和生效状态；关闭时秒数保留但不生效。设置读写继续走独立 admin settings API，统计查询走独立 admin TTFT API。

页面默认查看 24 小时，另支持 7/30/90 天；汇总卡展示受控请求数、attempt TTFT 超时率、换号恢复率、最终 TTFT 失败率和其他最终失败率，所有比例同时显示分子和分母。趋势图和失败分类图下方提供账号表，包含搜索、平台筛选、排序、分页、平均 TTFT 和低样本标记。request 级筛选只支持时间、入站协议和模型，账号/平台筛选只作用于账号 attempt 表。

## Risks / Trade-offs

- **[误判语义 token]** 不同协议事件结构持续演进 → 将 detector 保持为小型纯函数，并用真实协议片段覆盖文本、reasoning、工具调用、metadata 和空 delta。
- **[首 token 前缓冲增加内存]** 大量并发慢请求会占用缓冲 → 固定 256 KiB 上限，按 attempt 及时回收，测试 rollback 后不可再访问缓冲。
- **[超时与首 token 同时发生的竞态]** 可能既提交又 failover → controller 使用单一原子状态机，只有 `pending -> committed` 或 `pending -> timed_out` 一个转换成功。
- **[取消上游仍产生供应商费用]** 无法保证上游停止计费 → 默认关闭、后台明确风险、仅对管理员主动启用的请求生效。
- **[兼容 writer 接口不完整]** Gin/中间件可能依赖 `Flusher`、`Hijacker`、`Pusher` 等 → wrapper 完整实现 `gin.ResponseWriter` 契约，并增加接口断言和现有 stream failover 回归测试。
- **[失败账号 headers 泄漏]** 下个 attempt 可能继承 request-id 或 content-type → headers 使用本地副本，只在 commit 时覆盖到底层。
- **[多次 attempt 放大延迟]** 最坏耗时约为阈值乘以尝试数 → 复用并受现有 `maxAccountSwitches` 硬限制，不额外同账号重试。
- **[统计写入反压请求]** DB 故障可能把延迟传播到网关 → recorder 使用有界非阻塞队列和异步 flush，丢弃时暴露 completeness，不在请求线程重试数据库。
- **[多实例重复写]** 多个实例可能同时更新相同小时桶 → UPSERT 只做计数加法和最大值合并，天然支持并行 flush。
- **[低样本比例误导]** 新启用或小账号比例波动大 → 页面始终显示分子/分母，并对低于 20 个非取消 attempt 的账号标记低样本。

## Migration Plan

1. 先应用向前兼容的小时聚合表迁移，再发布默认关闭的配置、API、recorder、页面、writer/controller 和协议集成。
2. 验证关闭状态与当前行为一致；关闭时页面可查询历史但不新增受控样本。
3. 在测试环境使用短阈值验证换号、统计分子/分母、异步 flush 和 90 天清理。
4. 生产环境由管理员以保守阈值启用，观察 TTFT timeout 次数、换号成功率、504、数据完整性和供应商用量。
5. 如需回滚，先关闭设置即可立即停止新 attempt 使用门控和产生新样本；旧版本忽略新增表，迁移无需回滚即可保留历史。

## Open Questions

无。首期范围、候选耗尽行为、账号状态影响和 change 名称均已确认。
