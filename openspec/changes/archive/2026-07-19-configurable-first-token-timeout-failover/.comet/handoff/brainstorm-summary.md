# Brainstorm Summary

- Change: configurable-first-token-timeout-failover
- Date: 2026-07-14
- Status: confirmed

## 确认的技术方案

### 范围与配置

- 首期覆盖 OpenAI Responses、OpenAI Chat Completions 和 Anthropic Messages 的 HTTP 文本流式请求；WebSocket、非流式、图片、视频、批处理和后台任务不在范围内。
- 管理员配置为独立全局开关和 1-300 秒阈值，默认关闭、热更新，只影响保存后新创建的受控 attempt。
- 没有替代账号时不追加同账号重试；候选或现有换号次数耗尽后返回 `504 first_token_timeout`。
- 超时只影响当前请求，不直接把账号设为 error 或 temp unschedulable，超时 attempt 不写正常 usage log、不扣 Sub2API 余额。

### 独立 attempt 控制

- 采用已确认的方案 A：新增独立 attempt controller、事务化 response gate 和协议 detector；现有大文件只增加必要接入点，不移动代码、不顺带重构或格式化大段代码。
- 每个账号 attempt 在发起上游 HTTP 请求前启动绝对计时器，覆盖等待响应头和等待首个语义增量的时间；使用 `context.WithCancelCause` 取消上游 I/O。
- 首 token 统一定义为首次非空文本、reasoning 或工具调用参数增量；metadata、role-only、message_start、usage、ping 和空 delta 均不结束计时。
- 首 token 前的 headers 和响应字节进入 256 KiB 有界 gate，Flush 被抑制；首 token 到达后原子 commit 并恢复直写，timeout 先获胜时 rollback 全部输出。
- controller 只允许 pending 到 committed、timed_out 或 canceled 的一次终态转换，解决首 token 与 timeout 同时发生的竞态。
- timeout 转为不可同账号重试的 typed failover error，现有 handler 负责释放账号槽、排除账号和按原上限换号；客户端取消优先且不再换号。
- 功能关闭时完全走原转发路径。现有 `first_token_ms` 统计暂不修改；新超时使用独立语义 detector，减少与 main 的合并冲突。

### 独立统计存储

- 同时统计 attempt TTFT 超时率和最终请求 TTFT 失败率：
  - attempt timeout rate = TTFT timeout attempts / controlled eligible attempts。
  - final TTFT failure rate = `ttft_exhausted` requests / controlled eligible requests。
- 其他失败按 `rate_limit`、`auth`、`upstream_4xx`、`upstream_5xx`、`transport`、`stream_idle_timeout`、`protocol`、`other` 分类；`client_canceled` 单独记录但不计入账号失败率。
- 只统计开关开启期间、实际受策略控制的流量；关闭后停止产生新样本，但仍可查看保留期内历史。
- 新增一张 `first_token_timeout_stats_hourly` 小时聚合表，使用 `attempt` 与 `request` 两种 scope，不保存请求/错误正文：
  - attempt outcomes: `success`、`ttft_timeout`、`other_failure`、`client_canceled`。
  - request outcomes: `success`、`recovered_after_ttft`、`ttft_exhausted`、`other_failure`、`client_canceled`。
  - 维度包含小时、scope、账号、协议、平台、模型、阈值、outcome 和 failure kind；累计样本数、TTFT 样本数、TTFT 总毫秒数与最大值。
- 独立 recorder 在内存按维度合并，每 5 秒或达到批量阈值后用 PostgreSQL UPSERT 原子累加；多实例可并行 flush，停机时限时 flush。
- 统计写入失败不改变网关结果，但记录 dropped count 与最后成功 flush；API 返回数据完整性状态，页面提示不完整时间段。
- 数据保留 90 天，每日删除过期小时桶；不从现有日志回填，因为现有 Ops/usage 数据缺少完整 attempt 成功分母。

### 独立管理员页面

- 新增 `/admin/ttft` 和侧边栏“首 Token 监控”入口；页面顶部放置开关、秒数输入、保存按钮和生效状态。
- 页面支持 24 小时、7 天、30 天、90 天范围，默认 24 小时；筛选状态同步 URL。
- 汇总指标包含受控请求数、attempt TTFT 超时率、换号恢复率、最终 TTFT 失败率和其他最终失败率，并显示分子/分母。
- 使用折线图展示失败率趋势，横向条形图展示其他失败分类；颜色之外同时使用线型、标签和精确数值。
- 账号表展示账号/平台、样本数、成功数、TTFT timeout 数量与比例、其他失败数量与比例、平均 TTFT，支持搜索、排序、筛选、分页和低样本标记。
- 全局 request 指标只按时间、入站协议和模型筛选；账号/平台筛选留在账号表，避免多次换号请求被错误归因。
- 复用现有 Vue 管理后台主题、字体、组件、图表库和交互模式，提供 skeleton、空态、错误重试、暗色模式和响应式布局。

## 关键取舍与风险

- 事务化 writer 比仅加 context timeout 改动略多，但能保证失败 attempt 的 headers、metadata 和 ping 不泄漏，安全换号不可缺少。
- 现有 TTFT 指标与新 detector 暂时并存，牺牲指标口径统一来降低 main 合并冲突；超时控制本身使用统一且严格的语义口径。
- 小时聚合表不能做单请求明细排障；现有 Ops 页面继续承担明细用途，避免 90 天原始 attempt 表造成高写入和慢聚合。
- 内存批量 recorder 在进程崩溃或 DB 长期故障时可能丢失少量统计；通过 dropped count、数据完整性状态和停机 flush 明确暴露，不允许影响请求链路。
- 上游可能对本地取消前已经执行的 attempt 计费，这是默认关闭、管理员主动启用的延迟保护权衡。
- 账号低样本时比例波动大，页面必须同时显示样本数并标记样本不足。

## 测试策略

- Controller 状态机、timer/context 释放、commit-timeout 竞态和客户端取消单元测试。
- Response gate 的 header 隔离、Flush 抑制、顺序 commit、零泄漏 rollback、256 KiB 上限和 `gin.ResponseWriter` 契约测试。
- 三协议真实 SSE 片段 detector 表驱动测试，覆盖文本、reasoning、工具参数、metadata、role-only、usage、ping 和空 delta。
- Handler 集成测试覆盖慢账号换号成功、全部耗尽 504、原换号上限、客户端取消、首 token 后停流和失败输出不可见。
- Migration、聚合唯一键、并发 UPSERT、90 天清理、recorder batch/flush/故障/停机、失败映射和零分母测试。
- 验证一个请求多个 attempt 只产生一个 request scope 终态；换号成功产生 timeout attempt + recovered request，耗尽产生多个 timeout attempt + 单个 exhausted request。
- 管理员设置与统计 API 的鉴权、校验、过滤、排序、分页、数据完整性测试。
- 前端设置保存、指标分子分母、趋势/分类图、账号表、低样本、loading/empty/error、暗色和响应式测试。
- 回归验证开关关闭及所有非目标请求完全保持现状，并运行后端目标/全量测试、前端类型检查/测试/生产构建。

## Spec Patch

- 把原统计要求扩展为 attempt 与 request 双层指标，明确分母、换号恢复率、最终 TTFT 失败率、其他失败分类和客户端取消排除规则。
- 新增小时聚合表、受控流量采样、阈值快照、90 天保留、数据完整性和不记录敏感正文的验收场景。
- 新增独立管理员页面、顶部配置、时间范围、趋势、失败分类、账号超时率、低样本和错误/空数据状态的验收场景。
- 修正 proposal 中“客户端可见内容 token”的歧义，使其明确包含非空 reasoning 与工具调用参数增量。
