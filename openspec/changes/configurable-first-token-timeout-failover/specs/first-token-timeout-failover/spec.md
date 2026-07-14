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

### Requirement: 客户端取消优先于 failover
系统 SHALL 在客户端请求 context 取消时停止 TTFT timer、取消当前上游 attempt、回收缓冲并停止后续选号或重试。

#### Scenario: 客户端在首 token 前断开
- **WHEN** 客户端在等待首 token 时取消请求
- **THEN** 系统立即结束当前 attempt，且 SHALL 不选择另一个账号

### Requirement: 超时尝试必须可观测且不得重复计费
系统 SHALL 为每次首 token 超时记录结构化内部事件和调度失败结果，包含协议、平台、账号、模型、阈值、attempt 序号和换号次数。超时 attempt SHALL 不创建正常 usage log 或扣除 Sub2API 用户余额；最终成功 attempt SHALL 继续按现有流程计费。

#### Scenario: 超时后换号成功
- **WHEN** 第一个 attempt 首 token 超时且后续 attempt 成功
- **THEN** 系统记录一次 TTFT timeout 和一次最终成功 usage，不为超时 attempt 扣除用户余额

#### Scenario: 日志安全
- **WHEN** 系统记录首 token 超时事件
- **THEN** 日志 SHALL 不包含请求正文、访问凭据或上游内部网络地址

### Requirement: 统计只覆盖实际受控流量
系统 SHALL 只在首 token 超时开关开启且 eligible 请求实际进入 TTFT controller 时记录新统计样本。关闭期间和非目标请求 SHALL 不产生样本；已有历史数据 SHALL 继续可查询，且系统 SHALL 不使用现有 usage/Ops 日志回填缺少 attempt 分母的历史区间。

#### Scenario: 开关关闭后查看历史
- **WHEN** 管理员关闭策略后查看过去 7 天
- **THEN** 页面仍返回保留期内已有小时桶，但关闭后的请求不增加统计计数

#### Scenario: 非目标请求不进入统计
- **WHEN** WebSocket、非流式、媒体或批处理请求完成或失败
- **THEN** 系统不把该请求计入 TTFT attempt 或 request 分母

### Requirement: Attempt 与最终请求必须分别聚合
系统 SHALL 分别记录受控 attempt 和最终 request 的小时聚合结果。Attempt outcome SHALL 为 `success`、`ttft_timeout`、`other_failure` 或 `client_canceled`；request outcome SHALL 为 `success`、`recovered_after_ttft`、`ttft_exhausted`、`other_failure` 或 `client_canceled`。每个 request SHALL 恰好记录一个最终 outcome，每个实际发起的受控上游 attempt SHALL 恰好记录一个 attempt outcome。

#### Scenario: 首次 attempt 成功
- **WHEN** 一个受控请求的第一个 attempt 在阈值内产生语义首 token并最终成功
- **THEN** 系统增加一个 `attempt/success` 和一个 `request/success` 样本

#### Scenario: 换号后恢复
- **WHEN** 请求先发生一个 `ttft_timeout` attempt，随后另一个账号成功
- **THEN** 系统增加相应 attempt 样本，并只增加一个 `request/recovered_after_ttft` 样本

#### Scenario: TTFT 候选耗尽
- **WHEN** 请求的最后结果是 `504 first_token_timeout`
- **THEN** 系统增加一个 `request/ttft_exhausted` 样本，并保留此前每个账号的 `attempt/ttft_timeout` 样本

#### Scenario: 客户端取消
- **WHEN** 客户端取消受控请求
- **THEN** 系统记录 `client_canceled` outcome，但从 attempt 账号失败率和最终 request 失败率的分子与分母中排除

### Requirement: 失败率口径必须稳定且可解释
系统 SHALL 使用非取消样本计算比例并同时返回分子和分母。Attempt TTFT 超时率 SHALL 为 `ttft_timeout attempts / (success + ttft_timeout + other_failure attempts)`；最终 TTFT 失败率 SHALL 为 `ttft_exhausted requests / 非取消 requests`；其他最终失败率 SHALL 为 `other_failure requests / 非取消 requests`；换号恢复率 SHALL 为 `recovered_after_ttft requests / 至少发生过一次 TTFT timeout 的 requests`。

其他失败 SHALL 归一化为 `rate_limit`、`auth`、`upstream_4xx`、`upstream_5xx`、`transport`、`stream_idle_timeout`、`protocol` 或 `other`，且分类规则 SHALL 对 attempt 和 request 查询保持一致。

#### Scenario: 返回比例指标
- **WHEN** 管理员查询任一支持时间范围
- **THEN** API 为每个比例返回精确 numerator、denominator 和 rate，denominator 为零时返回零比例而不是非有限值

#### Scenario: TTFT 后以其他错误结束
- **WHEN** 请求先发生 TTFT timeout，后续 attempt 以其他失败结束
- **THEN** 最终 request 记为 `other_failure`，同时仍计入换号恢复率的“受 TTFT 影响请求”分母但不计入恢复分子

### Requirement: 小时聚合必须可并发写入并保留 90 天
系统 SHALL 使用 `first_token_timeout_stats_hourly` 保存小时桶，维度至少包含 scope、账号、入站协议、平台、模型、阈值快照、outcome 和 failure kind，计数至少包含样本数、TTFT 样本数、TTFT 总毫秒、TTFT 最大毫秒和受 TTFT timeout 影响的 request 数。多实例 SHALL 通过原子加法 UPSERT 合并相同维度；系统 SHALL 每日删除早于 90 天的桶。

#### Scenario: 多实例写入同一小时桶
- **WHEN** 两个实例分别 flush 相同维度的聚合增量
- **THEN** 数据库中的样本数与总耗时等于两批增量之和，最大 TTFT 等于两批最大值中的较大值

#### Scenario: 阈值在小时内改变
- **WHEN** 管理员在同一小时把阈值从 30 秒改为 20 秒
- **THEN** 新旧 attempt 按各自创建时的阈值快照进入不同聚合维度

#### Scenario: 保留期清理
- **WHEN** 每日清理运行
- **THEN** 早于 90 天的小时桶被删除，保留期内数据不受影响

### Requirement: 统计故障不得影响网关请求
系统 SHALL 通过独立有界内存 recorder 异步合并统计，并每 5 秒或达到批量阈值时 flush。队列满、数据库写入失败或停机 flush 超时 SHALL 不改变客户端请求结果；系统 SHALL 暴露 dropped sample count、最后成功 flush 时间和数据完整性状态。

#### Scenario: 数据库暂时不可用
- **WHEN** recorder flush 聚合增量时数据库失败
- **THEN** 网关请求继续按原结果完成，recorder 增加 dropped count，统计 API 把 completeness 标记为 degraded

#### Scenario: 正常 flush 恢复
- **WHEN** 后续 flush 成功
- **THEN** API 更新最后成功 flush 时间，但累计 dropped count 仍可见，避免把缺失区间误报为完整

### Requirement: 管理员使用独立页面配置并查看 TTFT
系统 SHALL 提供 `/admin/ttft` 独立页面和侧边栏“首 Token 监控”入口。页面顶部 SHALL 放置开关、1-300 秒输入、保存操作和当前生效状态；下方 SHALL 展示受控请求数、attempt TTFT 超时率、换号恢复率、最终 TTFT 失败率、其他最终失败率、失败趋势、其他失败分类和账号明细。

#### Scenario: 管理员保存设置
- **WHEN** 管理员在 `/admin/ttft` 修改开关或阈值并保存
- **THEN** 页面显示保存后的生效状态，统计筛选和已加载数据保持可用

#### Scenario: 查看账号超时率
- **WHEN** 管理员按时间范围查询账号表
- **THEN** 每行显示账号/平台、非取消 attempt 样本、成功数、TTFT timeout 数量与比例、其他失败数量与比例、平均 TTFT 和低样本提示，并支持搜索、筛选、排序和分页

#### Scenario: 查询数据不完整
- **WHEN** API 返回 degraded completeness
- **THEN** 页面明确提示当前范围可能缺样本，同时继续展示可用聚合数据和最后成功 flush 时间

### Requirement: 页面筛选不得制造错误的 request 归因
系统 SHALL 支持 24 小时、7 天、30 天和 90 天范围并默认 24 小时。全局 request 汇总与趋势 SHALL 只按时间、入站协议和模型筛选；账号和平台筛选 SHALL 只作用于 attempt 账号统计。

#### Scenario: 筛选单个账号
- **WHEN** 管理员在账号表选择一个账号
- **THEN** 账号表只展示该账号的 attempt 数据，而全局 request 汇总不因该账号筛选而改变
