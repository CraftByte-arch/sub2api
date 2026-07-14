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
