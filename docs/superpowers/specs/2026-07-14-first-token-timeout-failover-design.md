---
comet_change: configurable-first-token-timeout-failover
role: technical-design
canonical_spec: openspec
---

# 可配置首 Token 超时、换号与统计技术设计

## 1. 设计边界

需求与验收场景以 `openspec/changes/configurable-first-token-timeout-failover/specs/first-token-timeout-failover/spec.md` 为唯一事实源。本文只描述实现方式，不重新定义产品范围。

实现遵循两个约束：

1. 首 token 超时必须在任何失败 attempt 输出到达客户端前完成取消和回滚，否则不能安全复用现有 failover。
2. 新逻辑尽量放在独立文件和独立页面中。现有大型 handler、流解析器、SettingsView 和 Ops 页面只增加小型接入点，不移动代码、不顺带重构、不批量格式化。

首期只控制三个入站 HTTP 流协议：OpenAI Responses、OpenAI Chat Completions、Anthropic Messages。是否 eligible 由入站协议、`stream=true` 和非媒体意图共同决定，不以最终选择的上游平台决定。

## 2. 组件划分

后端新增以下独立组件，名称可在实现时按项目惯例微调，但职责不得合并回大型 handler：

| 组件 | 建议位置 | 职责 |
| --- | --- | --- |
| `FirstTokenTimeoutPolicy` | `backend/internal/service/first_token_timeout_policy.go` | 保存只读策略快照，提供 enabled/threshold，处理启动加载和热更新 |
| `FirstTokenAttemptController` | `backend/internal/service/first_token_timeout_attempt.go` | 管理 attempt context、timer、原子状态和取消原因 |
| `FirstTokenResponseGate` | `backend/internal/handler/first_token_response_gate.go` | 实现事务化 `gin.ResponseWriter`，隔离 headers/body/Flush |
| 协议 detector | `backend/internal/service/first_token_detector.go` | 纯函数识别三种入站协议的客户端可消费语义增量 |
| request/attempt tracker | `backend/internal/service/first_token_timeout_tracking.go` | 保证 attempt 与 request outcome 各记录一次，维护是否曾 TTFT timeout |
| stats port/model | `backend/internal/service/first_token_timeout_stats.go` | 定义聚合 key、event、查询 DTO、repository 接口和失败分类 |
| stats recorder | `backend/internal/service/first_token_timeout_stats_recorder.go` | 有界非阻塞采集、内存聚合、定时/阈值 flush、健康状态、清理 |
| stats repository | `backend/internal/repository/first_token_timeout_stats_repo.go` | PostgreSQL UPSERT、overview、trend、distribution、account page 查询 |
| admin handler | `backend/internal/handler/admin/first_token_timeout_handler.go` | 设置读写与统计查询参数/响应映射 |

前端新增 `frontend/src/api/admin/ttft.ts` 和 `frontend/src/views/admin/ttft/`。现有 `frontend/src/router/index.ts` 与 `frontend/src/components/layout/AppSidebar.vue` 只增加一条路由和一个导航项。

依赖关系保持单向：handler 负责 eligible 判断和 request/attempt 生命周期；controller/gate 负责取消与输出事务；协议流代码只报告语义 token；tracker 只发 stats event；recorder 依赖 stats repository。统计故障不得反向改变 controller 或 handler 的执行结果。

## 3. 设置与热更新

新增 settings key `first_token_timeout_settings`，JSON 结构为：

```go
type FirstTokenTimeoutSettings struct {
    Enabled        bool `json:"enabled"`
    TimeoutSeconds int  `json:"timeout_seconds"`
}
```

默认值为 `{enabled:false, timeout_seconds:30}`。读取缺失、JSON 损坏或阈值越界时返回默认关闭；写入必须拒绝非 1-300 整数。关闭时保留管理员最后保存的合法秒数。

热路径不直接查询数据库。`FirstTokenTimeoutPolicy` 使用 `atomic.Value` 保存不可变快照：

- 进程启动时从 settings 加载，失败则使用默认关闭并记录安全日志。
- 管理 API 成功持久化后同步替换本实例快照，再返回成功。
- 多实例通过现有 Redis 能力发布轻量 invalidation；订阅实例收到通知后从 settings 重载。Redis 不可用时由独立低频轮询兜底，恢复后自动收敛。
- 每个新 attempt 只读取一次快照，timer 期间设置变化不修改已开始 attempt 的 deadline。

invalidation 或兜底轮询失败只会延迟其他实例观察新设置，不得把无效值推入快照。API 的 effective 状态返回当前实例正在使用的快照及加载时间，便于管理员判断保存是否生效。

## 4. Attempt 生命周期

handler 在账号并发槽获得后、调用上游 forward 方法前创建 attempt。计时起点必须位于实际发起上游 HTTP 请求之前，因此覆盖 DNS/连接、TLS、响应头和首个语义增量等待，但不包含认证、计费校验、用户排队、选号和账号并发排队。

```text
selected account
  -> acquire account slot
  -> read policy snapshot
  -> create attempt context + timer
  -> install response gate
  -> call existing Forward...(attemptCtx, ginContext, account, ...)
  -> restore original writer
  -> finalize attempt outcome
  -> release account slot
  -> existing failover decision
```

controller 使用 `context.WithCancelCause`，状态仅允许一次从 `pending` 转为：

- `committed`：detector 识别到首个语义 token，timer 被停止，gate 永久进入直写。
- `timed_out`：deadline 先获胜，以 `ErrFirstTokenTimeout` 取消 attempt context，gate 回滚。
- `canceled`：客户端取消、prelude 溢出或首 token 前其他终止原因，gate 回滚。

状态转换使用 CAS；timer callback 和 `MarkFirstToken` 同时发生时只有一个成功。`Finish`、`Cancel` 和 timer stop 必须幂等，所有退出路径都释放 timer 资源。

客户端请求 context 是 attempt context 的父 context。父 context 取消优先：handler 不进入后续选号，统计记为 `client_canceled`。超时 context 必须传给最终的 `http.NewRequestWithContext` 或等价调用，不能只在外层 select，否则等待响应头的网络 I/O 无法立即中断。

## 5. 事务化 Response Gate

gate 包装当前 `gin.ResponseWriter`，安装范围仅限单个 account attempt。构造时复制底层已有 headers，后续 `Header()` 返回 attempt 本地 header map；回滚时本地更改不会污染下一个账号。

pending 状态行为：

- `WriteHeader` 只记录 status，不调用底层 writer。
- `Write`/`WriteString` 按调用顺序追加到 256 KiB 缓冲并返回正常长度。
- `Flush` 不触发底层 flush。
- `Written`/`Size` 反映客户端尚未收到当前 attempt 内容，避免现有 handler 误判 stream 已开始。
- `Status` 返回本地待提交状态，未设置时沿用底层默认状态。

commit 必须在单一临界区内完成：CAS controller 状态、停止 timer、把本地 headers/status/bytes 按顺序写入底层 writer、清空缓冲、切换到 passthrough。commit 一旦开始，即使底层写失败也不能再换号，因为客户端可能已收到部分数据。

rollback 清空本地 header、status 和 byte slice，恢复 handler 的原始 writer。失败 attempt 的 `Content-Type`、request-id、metadata、ping 和 keepalive 均不得泄漏。

缓冲将超过 256 KiB 时，gate 不再接收新字节，并以 `ErrFirstTokenPreludeTooLarge` 取消 controller。即使现有调用方忽略 `Write` 错误，绑定的 attempt context 也会终止上游读取；最终转换为不可同账号重试的 failover error。该错误属于 `protocol`，不是 `ttft_timeout`。

实现必须满足 `gin.ResponseWriter` 完整契约，并通过编译期接口断言覆盖 `http.Flusher`、`http.Hijacker`、`http.Pusher`、`CloseNotify` 等项目当前依赖的方法。pending 状态不允许 hijack/push 绕过 gate；eligible 路径不包含 WebSocket，因此可返回明确的不支持错误。

## 6. 语义 Token 检测

detector 解析即将写给客户端的协议事件，而不是任意上游原始事件。这样同一个入站协议无论经过 Anthropic、OpenAI、Gemini 或兼容转换，判断口径保持一致。

三组纯函数规则为：

- Responses：非空文本 delta、reasoning/summary delta、function/tool call arguments delta。
- Chat Completions：任一 choice delta 中非空 `content`、reasoning content、function call arguments 或 tool call arguments；只有 role、finish reason 或 usage 不算。
- Anthropic Messages：content block delta 中非空 `text_delta`、`thinking_delta` 或 `input_json_delta.partial_json`；`message_start`、`content_block_start`、usage、ping 和 stop 事件不算。

空白字符串是否算语义进展按“非空 payload”判断，不额外 `TrimSpace` 删除模型真实输出；字段缺失、空字符串和空 JSON argument delta 不算。detector 不修改现有 `first_token_ms` 计算，首期允许两个口径并存以降低冲突。

接入点放在各流路径已经完成协议转换、准备调用 writer 的位置，并在写入该事件前调用 `MarkFirstToken`。如果 commit 失败是因为 timeout 已先获胜，该事件不得写入，forward 返回 TTFT failover error。

上游在首 token 前正常终止、silent refusal、SSE error、JSON 解析失败或 transport error 时，controller 停止 timer 并保留原错误语义，不把它伪装成 TTFT timeout。

## 7. Failover 与错误映射

`UpstreamFailoverError` 增加一个可选稳定 kind/code 字段，空值保持全部现有行为。TTFT timeout 构造值为：

```text
StatusCode: 504
ErrorType: first_token_timeout
RetryableOnSameAccount: false
```

prelude 溢出使用独立内部 kind，状态映射为 502，仍禁止同账号重试。现有 `FailoverState.HandleFailoverError` 继续负责加入 `FailedAccountIDs`、受 `MaxSwitches` 限制并选择下一账号；TTFT error 不触发 temp unschedule，也不进入 pool mode 同账号 retry。

三个入站 handler 的 exhausted 分支只增加专用错误映射：当最后错误 type 为 `first_token_timeout` 且尚未提交流时，按各协议错误 envelope 返回 HTTP 504 和稳定 type。其他错误继续走现有 silent refusal、passthrough 和 upstream mapping。

超时 attempt 在调用正常 usage/billing 之前已经返回 error，因此不得创建 usage log 或扣 Sub2API 余额。账号调度 runtime stats 可以上报失败，但不得调用会持久禁用或临时下线账号的逻辑。

## 8. 统计生命周期与失败分类

eligible 且策略开启时，handler 为整个请求创建一个 `FirstTokenRequestTracker`；每次实际发起上游 HTTP attempt 时创建子 tracker。两者都用 `sync.Once` 或等价原子终结，防止 defer、错误分支和客户端取消重复计数。

Attempt outcome：

- `success`：forward 完整成功。
- `ttft_timeout`：cause 为 `ErrFirstTokenTimeout`。
- `client_canceled`：父 request context 被客户端取消。
- `other_failure`：其余失败，包括首 token 后断流；同时写 failure kind。

Request outcome：

- `success`：最终成功且从未发生 TTFT timeout。
- `recovered_after_ttft`：至少一个 attempt TTFT timeout 后最终成功。
- `ttft_exhausted`：最终对外错误为 `first_token_timeout`。
- `client_canceled`：客户端取消终止整个请求。
- `other_failure`：其他最终失败。若此前有 TTFT timeout，`ttft_affected_count` 仍加一，保证恢复率分母准确。

其他失败分类优先级固定，避免相同错误在不同位置被分到不同桶：

1. 明确 429/限速类型 -> `rate_limit`
2. 401/403 或认证类型 -> `auth`
3. 其他 400-499 -> `upstream_4xx`
4. 500-599 -> `upstream_5xx`
5. dial/TLS/reset/EOF/context 等网络错误 -> `transport`
6. 现有 stream data interval timeout -> `stream_idle_timeout`
7. SSE/JSON/协议帧/prelude 溢出 -> `protocol`
8. 其余 -> `other`

客户端取消在以上分类前短路。TTFT timeout 是独立 outcome，不再重复放入 failure kind。

TTFT 样本在语义 token commit 时记录 elapsed ms。账号平均 TTFT 使用所有实际观察到语义 token 的 attempt 样本，包括后来发生流错误的 attempt；timeout attempt 不把阈值伪装成真实 TTFT 样本。

## 9. 小时聚合存储

新增 migration `backend/migrations/175_first_token_timeout_stats_hourly.sql`。不使用 Ent schema，repository 通过现有 `*sql.DB` 执行批量 SQL，避免生成大量无关文件。

```sql
CREATE TABLE first_token_timeout_stats_hourly (
    bucket_start          timestamptz  NOT NULL,
    scope                 varchar(16)  NOT NULL,
    account_id            bigint       NOT NULL DEFAULT 0,
    protocol              varchar(32)  NOT NULL,
    platform              varchar(32)  NOT NULL DEFAULT '',
    model                 varchar(255) NOT NULL DEFAULT '',
    timeout_seconds       smallint     NOT NULL,
    outcome               varchar(32)  NOT NULL,
    failure_kind          varchar(32)  NOT NULL DEFAULT '',
    sample_count          bigint       NOT NULL DEFAULT 0,
    ttft_sample_count     bigint       NOT NULL DEFAULT 0,
    ttft_sum_ms           bigint       NOT NULL DEFAULT 0,
    ttft_max_ms           integer      NOT NULL DEFAULT 0,
    ttft_affected_count   bigint       NOT NULL DEFAULT 0,
    updated_at            timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (
        bucket_start, scope, account_id, protocol, platform,
        model, timeout_seconds, outcome, failure_kind
    )
);
```

迁移增加 scope/outcome/threshold/counter 非负 check，以及 `(scope, bucket_start)` 和 `(scope, account_id, bucket_start)` 查询索引。表不对 account 建外键，使账号删除后 90 天统计仍可查询。attempt 行保存真实 `account_id/platform`；request 行固定使用 `account_id=0, platform=''`，防止一个多账号请求被错误归到最终账号。账号名在查询时 left join 当前 accounts，已删除账号显示为 `#<id>`。

UPSERT 冲突更新规则：sample、TTFT sample、sum 和 affected count 做加法；max 使用 `GREATEST`；`updated_at=now()`。每条 event 先把时间截断到 UTC 小时，并保存 attempt 创建时的 threshold 快照，同一小时改阈值会形成不同 key。

attempt 行始终使用该 attempt 的阈值快照；request 行使用最终 attempt 的阈值快照。这样 request 只有一个确定维度，而此前不同阈值的 timeout attempt 仍保留在各自准确的 attempt 桶中。页面的全局 request 比率会跨阈值求和，不把 request 阈值用于账号归因。

## 10. Recorder、健康状态与保留期

`Record` 向容量 4096 的 channel 做非阻塞 send；队列满时只原子增加 `droppedSamples`。单独 goroutine 消费 event 并按聚合 key 合并，以下任一条件触发 flush：

- 5 秒 ticker 到期；
- 唯一聚合 key 达到 1000；
- 服务停止。

flush 先交换当前 map，再在 2 秒 context 内批量 UPSERT。失败批次不在请求线程重试，以批次 `sample_count` 增加累计 dropped；成功更新 `lastSuccessfulFlush`。累计 dropped 在进程生命周期内不清零，因此后续成功也不能掩盖已缺失数据。

健康快照包含 `complete|degraded`、累计 dropped、最后成功 flush 和当前 pending event 数。该状态是响应请求的当前实例健康信息；页面文案不得把它描述为严格的全集群无丢失证明。数据库聚合本身通过加法 UPSERT支持多实例。

每日 ticker 使用短超时执行 `DELETE ... WHERE bucket_start < date_trunc('hour', now() - interval '90 days')`。多实例重复执行是幂等的，不引入对现有 Ops cleanup service 的修改。停机时尝试 2 秒 flush，超时后退出，不阻塞服务无限关闭。

## 11. 查询 API 与计算口径

设置 API：

- `GET /api/v1/admin/settings/first-token-timeout`
- `PUT /api/v1/admin/settings/first-token-timeout`

统计 API：

- `GET /api/v1/admin/ttft/overview?range=24h&protocol=&model=`：一次返回汇总、小时趋势、其他失败分类和 completeness。
- `GET /api/v1/admin/ttft/accounts?range=24h&protocol=&model=&platform=&account_id=&search=&sort=&order=&page=&page_size=`：返回账号 attempt 聚合分页。

`range` 只接受 `24h|7d|30d|90d`，默认 `24h`。protocol 必须来自三个目标协议；page size 使用项目已有白名单。sort 只允许 samples、success、ttft_timeout_count/rate、other_failure_count/rate、avg_ttft_ms。

所有 rate 由后端计算并返回：

```text
attempt denominator = success + ttft_timeout + other_failure attempts
attempt TTFT timeout rate = ttft_timeout / attempt denominator
request denominator = success + recovered_after_ttft + ttft_exhausted + other_failure
final TTFT failure rate = ttft_exhausted / request denominator
other final failure rate = other_failure / request denominator
recovery denominator = SUM(request.ttft_affected_count)
recovery rate = recovered_after_ttft / recovery denominator
```

分母为零时 rate 返回 `0`，同时保留 numerator/denominator，禁止返回 NaN/null。受控请求卡使用 request `sample_count` 总和并单列 canceled 数；失败率仍排除 canceled。

overview 不接受 account/platform，account API 才接受这些筛选。model 与 protocol 同时作用于 request 和 attempt。查询直接聚合最多 90 天小时表，不访问 usage logs 或 ops error logs。

## 12. 管理员页面

`/admin/ttft` 使用现有 `AppLayout`、主题 tokens、BaseButton/BaseInput/BaseSelect/BaseTable/EmptyState 与 Chart.js，不把页面嵌入 OpsDashboard，也不继续增大 SettingsView。

页面结构从上到下为：

1. 紧凑设置带：toggle、秒数输入、保存按钮、已生效/已关闭状态和最后加载时间。
2. 筛选工具栏：24h/7d/30d/90d segmented control、协议、模型、刷新。
3. 五个汇总指标：受控请求、attempt TTFT 超时率、TTFT 换号恢复率、最终 TTFT 失败率、其他最终失败率；每项显示精确分子/分母。
4. 双图区域：失败率小时折线图、其他失败分类横向条形图。
5. 账号表：账号/平台、attempt 样本、成功、TTFT timeout 数/率、其他失败数/率、平均 TTFT、低样本标记。

低样本阈值固定为 20 个非取消 attempt，只做提示不隐藏比例。折线除颜色外使用不同线型和图例；条形图直接显示数值。筛选状态写入 query string，刷新和返回页面可恢复。

首次加载显示 skeleton；无样本显示空态且保留设置区；overview 或 accounts 单独失败时各自显示 retry，不清空另一区域的成功数据；completeness degraded 显示非阻塞警告。窄屏下汇总卡改为两列/单列，图表纵向排列，账号表保持横向滚动，所有控件必须有稳定宽高避免加载跳动。

## 13. 低冲突接入策略

必须遵守以下编辑边界：

- 不修改现有 `first_token_ms` 逻辑。
- 不重构 `GatewayHandler`、`OpenAIGatewayHandler` 或现有 forward service；每个 failover 循环只增加 begin/finalize/restore hook。
- detector 调用贴近已有 writer 写出点，每处只增加一到两行 helper 调用。
- `UpstreamFailoverError` 只增加可选字段和小型 helper，空字段保持原行为。
- stats 使用新 repository/service/handler/API/view 文件；不并入大型 Ops repository、OpsDashboard 或 SettingsView。
- migration 使用新编号文件；不改写既有迁移。
- Wire provider set、route、router、sidebar 和 locale index 只做追加式修改。

如果实现时发现某个目标入站协议存在多个实际输出 pipeline，必须逐一接入并用入口级测试证明覆盖，不能通过扩大重构来统一 pipeline。

## 14. 测试策略

### 单元测试

- controller：四状态转换、timer 释放、commit/timeout 竞态、父 context 取消、重复 Finish。
- gate：header 隔离、WriteHeader、Write/WriteString 顺序、Flush 抑制、commit 直写、rollback 零泄漏、256 KiB 边界、接口断言。
- detector：三个协议真实 SSE payload，覆盖文本、reasoning/thinking、tool/function arguments、role-only、metadata、usage、ping、空 delta、终止事件。
- classifier/tracker：每种 failure kind、client canceled 排除、TTFT 后成功、TTFT 后其他失败、恰好一次记录。
- recorder：队列满、5 秒/阈值 flush、UPSERT 失败丢弃计数、shutdown timeout、并发 Record、健康快照。
- query：所有 rate 分母、零分母、账号局部筛选、阈值快照、排序白名单和分页。

### 集成测试

- 慢账号在响应头前和 metadata/ping 后分别超时，第二账号成功且客户端只看到第二账号 headers/body。
- 候选耗尽返回各入站协议的 `504 first_token_timeout` envelope。
- 首 token 与 deadline 同时发生时只出现 commit 或 failover 之一。
- 客户端断开立即取消上游且不换号。
- 首 token 后 stream idle timeout 继续走现有行为，不 TTFT failover。
- timeout attempt 不产生 usage/billing，最终成功 attempt 正常计费。
- attempt/request 聚合 outcome、受影响 request 分母和多实例 UPSERT 加法正确。
- 开关关闭、非流式、WebSocket、图片、视频、batch 不安装 gate、不产生统计样本。

### 前端测试

- 设置加载、非法阈值、保存成功/失败和 effective 状态。
- URL range/protocol/model 恢复，账号筛选不改变 overview 请求参数。
- 指标 numerator/denominator、零样本、低样本、degraded、错误重试和分页排序。
- dark mode 与 375px/768px/desktop 布局无溢出或文字遮挡。

### 完成验证

运行目标 Go 包测试、repository migration/integration tests、`go test ./...`、前端 typecheck、Vitest 和生产构建。用 race test 覆盖 controller/gate/recorder 核心包。发布验证先确认默认关闭与旧行为一致，再以短阈值启用并核对数据库小时桶和页面分子/分母。

## 15. 发布与回滚

数据库迁移先行且只新增表/索引。应用发布时策略默认关闭，因此新 controller 和 recorder 不进入请求热路径。管理员启用后观察 timeout attempts、recovery、final 504、other failures、dropped samples 和供应商用量。

紧急回滚优先在 `/admin/ttft` 关闭开关，新 attempt 立即不再安装 gate或产生统计；保留历史查询。随后可回滚应用版本，旧版本会忽略新增表。无需删除表，避免丢失 90 天观察数据。
