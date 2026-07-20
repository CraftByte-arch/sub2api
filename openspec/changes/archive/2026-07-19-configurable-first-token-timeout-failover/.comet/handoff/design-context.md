# Comet Design Handoff

- Change: configurable-first-token-timeout-failover
- Phase: design
- Mode: compact
- Context hash: dac2efaeb60dc5e004000fcb9affc2806e5b442086f2adc0dbfbb0a31a7185fe

Generated-by: comet-handoff.sh

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## openspec/changes/configurable-first-token-timeout-failover/proposal.md

- Source: openspec/changes/configurable-first-token-timeout-failover/proposal.md
- Lines: 1-34
- SHA256: 9201df56126dd756f8bbc84ae0864992ce5b098f245409d32fe5d582e84c01cb

```md
## Why

当前网关只能在上游报错或流数据间隔超时后切换账号，无法处理“连接已建立但长时间没有首个内容 token”的慢请求。管理员需要一个可热更新的首 token 超时阈值，在客户端尚未收到语义响应时取消慢上游并安全换号，以降低长尾等待时间。

## What Changes

- 在管理员后台新增首 token 超时总开关和超时秒数配置，默认关闭，允许范围为 1-300 秒，保存后对新请求实时生效。
- 为 HTTP 文本流式请求定义统一的“首 token”语义：第一个非空文本、reasoning/thinking 或工具/函数参数增量结束计时，响应头、metadata、role-only、usage、ping、空 delta 和终止事件均不算首 token。
- 在首 token 前暂存准备发送给客户端的协议事件；首 token 到达时按原顺序提交，超时时丢弃，保证失败尝试不会污染后续账号的响应流。
- 首 token 超时后立即取消当前上游尝试，将当前账号排除在本请求后续选择之外，并复用现有账号 failover 次数上限选择其他账号重试。
- 所有候选账号或换号次数耗尽时返回明确的 `504 first_token_timeout`；不在同一账号上额外重试。
- 超时仅影响当前请求并上报调度失败、Ops 事件和指标，不直接将账号标记为错误或临时不可调度。
- 首 token 已发送后关闭该超时控制，后续流中断继续由现有 stream data interval timeout 处理。
- 首期仅覆盖 OpenAI Responses、OpenAI Chat Completions 和 Anthropic Messages 的 HTTP 文本流式请求；WebSocket、非流式请求、图片、视频和批处理不在本次范围内。
- 新增独立的小时聚合统计，只采集开关开启且实际受控的 attempt/request，分别展示 TTFT attempt 超时率、换号恢复率、最终 TTFT 失败率、其他失败率和每账号超时率；数据保留 90 天，不回填历史日志。
- 新增管理员独立页面 `/admin/ttft`，页面顶部直接配置开关与阈值，下方展示汇总、趋势、失败分类和账号明细。

## Capabilities

### New Capabilities

- `first-token-timeout-failover`: 定义管理员可配置的首 token 超时、跨协议首 token 判定、首 token 前响应门控、上游取消、账号 failover、终止错误和可观测性要求。

### Modified Capabilities

无。

## Impact

- 后端系统设置模型、设置存储、管理员设置 API、DTO 和校验逻辑。
- 管理员独立 TTFT 页面、前端 API 类型、路由、侧边栏、国际化文案、图表和交互测试。
- OpenAI Responses、OpenAI Chat Completions、Anthropic Messages 的 HTTP 流式响应处理与现有 handler failover 循环。
- 上游 attempt context 生命周期、首 token 前事件缓冲、客户端断开处理、账号调度结果和 Ops 指标。
- 新增 `first_token_timeout_stats_hourly` PostgreSQL 小时聚合表、独立 repository/recorder/query API 和 90 天清理；配置继续存储在现有 settings 存储中，不新增第三方依赖。
```

## openspec/changes/configurable-first-token-timeout-failover/design.md

- Source: openspec/changes/configurable-first-token-timeout-failover/design.md
- Lines: 1-127
- SHA256: 57b6bb652d8b2afca902736351b4a17b508f085fc3d41f665a889995b7a14330

[TRUNCATED]

```md
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
```

Full source: openspec/changes/configurable-first-token-timeout-failover/design.md

## openspec/changes/configurable-first-token-timeout-failover/tasks.md

- Source: openspec/changes/configurable-first-token-timeout-failover/tasks.md
- Lines: 1-49
- SHA256: fd5153b1c0c34cfae26732575dd6b039b3fb447b2fd9f4e46af0b5531ec470c0

```md
## 1. 设置与管理 API

- [ ] 1.1 新增 `FirstTokenTimeoutSettings`、独立 setting key、默认值、1-300 秒解析校验和保存逻辑，并覆盖缺失/损坏配置回退测试
- [ ] 1.2 新增管理员读取与更新 API、DTO、路由和设置审计字段，覆盖有效保存、非法阈值和关闭状态测试
- [ ] 1.3 新增只读策略快照供每个 eligible attempt 获取启用状态与阈值，保证保存热更新、并发读取和损坏配置回退不阻塞网关

## 2. Attempt 控制器与事务化响应门

- [ ] 2.1 为 pending、committed、timed_out、canceled 状态编写并发与竞态测试，明确只有一个终态转换能成功
- [ ] 2.2 实现基于 `context.WithCancelCause` 的 attempt controller、timer 生命周期和 context helper，确保 commit、正常结束及客户端取消均释放资源
- [ ] 2.3 实现完整 `gin.ResponseWriter` 契约的事务化 writer，支持本地 headers、Write/WriteString、抑制 Flush、Commit 和 Rollback
- [ ] 2.4 实现 256 KiB prelude 缓冲上限及溢出 failover，并覆盖 header 不泄漏、事件顺序、接口兼容和缓冲回收测试

## 3. 协议语义 token 判定与接入

- [ ] 3.1 为 OpenAI Responses、Chat Completions 和 Anthropic Messages 建立纯函数 token detector 测试，覆盖文本、reasoning、工具调用、metadata、role-only、usage、ping 和空 delta
- [ ] 3.2 接入 OpenAI Responses HTTP 流式路径，在首个语义 token 前门控输出，并保持 compact keepalive、silent refusal 和既有错误分类行为
- [ ] 3.3 接入 OpenAI Chat Completions HTTP 流式路径，确保 role-only chunk 不提交、内容或工具 delta 正确提交
- [ ] 3.4 接入 Anthropic Messages HTTP 流式路径，确保 lifecycle/keepalive 不提交、内容或工具输入增量正确提交
- [ ] 3.5 验证首 token 提交后关闭 TTFT 控制，后续停流仍由现有 stream data interval timeout 处理

## 4. Failover、调度与计费

- [ ] 4.1 将 attempt timeout 和 prelude overflow 转换为稳定的 typed `UpstreamFailoverError`，TTFT 超时使用 504、`first_token_timeout` 且禁止同账号重试
- [ ] 4.2 在目标 handler failover 循环中按 attempt 安装/回收门控，正确释放账号槽、排除超时账号并受现有 `maxAccountSwitches` 限制
- [ ] 4.3 记录安全的 TTFT timeout 结构化事件、Ops 指标和账号调度失败结果，不记录正文、凭据或内部地址
- [ ] 4.4 确保超时 attempt 不写正常 usage log、不扣除 Sub2API 用户余额，最终成功 attempt 继续正常计费

## 5. 独立统计存储与查询

- [ ] 5.1 新增 `first_token_timeout_stats_hourly` migration，定义 attempt/request scope、维度哨兵、outcome/failure kind 约束、加法 UPSERT 唯一键和 90 天查询索引，并增加迁移测试
- [ ] 5.2 新增独立 stats port/repository，实现批量原子 UPSERT、汇总/趋势/失败分类/账号分页查询和 90 天幂等清理，覆盖多实例累加与阈值快照测试
- [ ] 5.3 新增有界异步 recorder，支持非阻塞 Record、5 秒/批量阈值 flush、2 秒停机 flush、dropped count、last successful flush 和每日清理，覆盖 DB 失败不传播及竞态测试
- [ ] 5.4 在 attempt 与 request 生命周期末端各记录一次 outcome，统一其他失败分类，确保 client_canceled 排除率分母、TTFT 后其他失败仍进入受影响 request 分母
- [ ] 5.5 新增管理员 TTFT summary/trend/failure-distribution/account-stats API、DTO、参数校验和 completeness 元数据，覆盖 24h/7d/30d/90d、协议/模型与账号局部筛选

## 6. 独立管理员页面

- [ ] 6.1 新增 `/admin/ttft` 路由、侧边栏“首 Token 监控”入口、独立前端 API/types 和中英文 locale，设置筛选状态同步 URL
- [ ] 6.2 页面顶部实现策略加载、toggle、1-300 秒输入、保存按钮和生效/校验状态，不再修改现有大型 SettingsView 区块
- [ ] 6.3 实现五项汇总指标、失败率趋势折线图、其他失败分类横向条形图和 completeness 提示，所有比例显示分子/分母
- [ ] 6.4 实现账号统计表的搜索、平台/账号筛选、排序、分页、平均 TTFT 与低样本提示，并覆盖 skeleton、空态、错误重试、暗色和响应式状态
- [ ] 6.5 增加页面/API 单元测试，验证全局 request 筛选不受账号局部筛选影响、URL 恢复、保存设置和 degraded 状态

## 7. 端到端验证与发布保护

- [ ] 7.1 增加慢账号超时后第二账号成功、候选耗尽返回 504、客户端取消停止重试、失败账号输出完全不可见且统计 outcome 正确的集成测试
- [ ] 7.2 增加关闭功能、非流式、WebSocket、图片/视频/批处理不受影响且不产生统计样本的回归测试
- [ ] 7.3 运行后端目标包与全量测试、迁移测试、前端类型检查/测试/生产构建，并记录默认关闭发布、短阈值启用、统计完整性和设置开关回滚验证结果
```

## openspec/changes/configurable-first-token-timeout-failover/specs/first-token-timeout-failover/spec.md

- Source: openspec/changes/configurable-first-token-timeout-failover/specs/first-token-timeout-failover/spec.md
- Lines: 1-188
- SHA256: 6b8d4ae25b9a810e41824221899ba002c953d0c5eb25dc4490fa1a87ea3db46c

[TRUNCATED]

```md
## ADDED Requirements

### Requirement: 管理员可配置首 token 超时
系统 SHALL 提供独立的首 token 超时总开关和超时秒数设置。该设置 SHALL 默认关闭，超时秒数 SHALL 只接受 1-300 的整数，并 SHALL 在保存后对新开始的上游 attempt 生效而无需重启服务。

#### Scenario: 管理员启用有效阈值
- **WHEN** 管理员保存启用状态和 30 秒阈值
- **THEN** 系统持久化设置，并让之后创建的 eligible attempt 使用 30 秒阈值

#### Scenario: 管理员提交非法阈值
- **WHEN** 管理员提交小于 1、大于 300 或非整数的阈值
- **THEN** 系统拒绝更新并返回可理解的校验错误

#### Scenario: 功能保持关闭
- **WHEN** 首 token 超时开关关闭
- **THEN** 所有请求 SHALL 保持现有流式转发和 failover 行为

### Requirement: 首期请求范围受到明确限制
系统 SHALL 只对 OpenAI Responses、OpenAI Chat Completions 和 Anthropic Messages 的 HTTP 文本流式请求启用首 token 超时。WebSocket、非流式、图片、视频、批处理和后台任务 SHALL 不受该设置影响。

#### Scenario: Eligible HTTP 文本流
- **WHEN** 客户端通过目标入口发起流式文本请求且功能已启用
- **THEN** 系统为每个上游账号 attempt 启动独立首 token 计时

#### Scenario: 非目标请求
- **WHEN** 请求是 WebSocket、非流式或媒体/批处理请求
- **THEN** 系统 SHALL 不创建首 token 门控或改变原有超时行为

### Requirement: 首 token 必须代表语义进展
系统 SHALL 将第一个非空、客户端可消费的文本 delta、reasoning/thinking delta 或工具/函数参数 delta 视为首 token。响应头、生命周期 metadata、role-only chunk、usage、ping、空 delta 和终止标记 SHALL 不结束首 token 计时。

#### Scenario: Metadata 先于文本到达
- **WHEN** 上游先发送 response-created 或 message-start，再发送非空文本 delta
- **THEN** 系统只在非空文本 delta 到达时结束首 token 计时

#### Scenario: 工具调用先于文本到达
- **WHEN** 上游首先发送非空工具调用参数增量
- **THEN** 系统将其视为首个语义 token 并提交响应

#### Scenario: 只有 ping 和空 delta
- **WHEN** 上游在阈值内只发送 ping、metadata 或空 delta
- **THEN** 首 token 计时 SHALL 继续运行

### Requirement: 首 token 前响应必须可回滚
系统 SHALL 在首 token 前暂存当前 attempt 的响应头和响应字节，并 SHALL 抑制底层 Flush。只有首 token 状态成功提交后，系统才能按原顺序把暂存内容和后续内容写给客户端。

#### Scenario: 首 token 正常到达
- **WHEN** 首个语义 token 在阈值内到达
- **THEN** 系统原子停止定时器、提交成功账号的响应头和暂存事件，并继续正常直写后续流

#### Scenario: Attempt 在首 token 前失败
- **WHEN** 当前 attempt 在首 token 前超时或返回可 failover 错误
- **THEN** 系统丢弃该 attempt 的响应头和全部暂存字节，下一 attempt 从干净响应状态开始

#### Scenario: Prelude 超过内存上限
- **WHEN** 首 token 前暂存内容超过 256 KiB
- **THEN** 系统 SHALL 中止该 attempt 并通过有界的 failover 错误处理，且 SHALL 不继续增长缓冲区

### Requirement: 首 token 超时必须取消上游并换号
系统 SHALL 从上游 attempt 开始时计时。阈值到期且尚未提交首 token 时，系统 SHALL 以专用原因取消上游 context、释放当前账号并返回不可同账号重试的 `UpstreamFailoverError`，使现有 handler 排除当前账号并重新选号。

#### Scenario: 第二个账号成功
- **WHEN** 第一个账号超过 N 秒没有语义 token且第二个 eligible 账号可用
- **THEN** 系统取消第一个 attempt、排除第一个账号并通过第二个账号重试完整请求，客户端 SHALL 看不到第一个 attempt 的任何输出

#### Scenario: 没有其他候选账号
- **WHEN** 当前账号首 token 超时且没有其他 eligible 账号
- **THEN** 系统 SHALL 不在同一账号追加重试，并返回 `504` 和稳定错误类型 `first_token_timeout`

#### Scenario: 最大换号次数耗尽
- **WHEN** 多个账号连续首 token 超时并达到现有最大换号次数
- **THEN** 系统 SHALL 停止重试并把最后一个首 token 超时作为 `504 first_token_timeout` 返回

### Requirement: 首 token 提交后不得因 TTFT 换号
系统 SHALL 在首 token 提交后永久关闭当前请求的首 token 定时器。后续流数据停顿或断开 SHALL 继续使用现有 stream data interval timeout 和已写流错误处理，且 SHALL 不尝试拼接其他账号的响应。

#### Scenario: 首 token 后发生流停顿
- **WHEN** 客户端已经收到首个语义 token，随后上游超过流数据间隔阈值
- **THEN** 系统按现有 stream timeout 行为结束该流，不进行首 token failover

```

Full source: openspec/changes/configurable-first-token-timeout-failover/specs/first-token-timeout-failover/spec.md
