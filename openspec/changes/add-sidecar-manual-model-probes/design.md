## Context

侧车已有 `ExportDirectProbeSnapshot` 与 `DirectProber.ProbeDirect`，自动引擎使用 `model.Policy` 运行单模型流式探测。手动功能应复用该链路，但不写入调度状态。

## Design

新增管理员接口 `POST /api/accounts/{accountID}/manual-probe`。请求包含 `models`、`prompt`、`reasoning_effort` 和可选 `latency_limit_ms`。接口通过当前管理员 JWT 导出一次账号直连快照，在侧车内逐模型串行探测，最多 8 个模型；每次使用独立规范化 Policy，并返回结果数组。

推理强度映射为各上游协议可识别的请求字段：OpenAI Responses 使用 `reasoning.effort`，Chat Completions 使用 `reasoning_effort`；Anthropic 使用 `thinking` 的 enabled/budget_tokens；Gemini 使用 `thinkingConfig`。若上游协议不支持，忽略该字段而不影响普通探测。为减少侵入，模型请求构造在现有 DirectProber 增加可选字段，默认自动探测 Policy 不变。

前端在账号行的操作菜单和账号详情操作区增加“手动探测”，弹窗提供模型多选、提示词、推理强度，提交后展示逐模型结果。模型候选通过侧车管理员代理读取 Sub2API 已有的 `GET /admin/accounts/{id}/models`，确保与主后台账号测试所见模型一致；不再内置或猜测模型。管理员仍可手动输入模型 ID 作为兜底。

手动探测不依赖账号是否存在旧的侧车自动检测配置。只要账号仍存在于 Sub2API、类型为 API Key，且当前管理员会话可以导出直连凭证，就可以从账号详情区或账号行右侧的溢出菜单发起探测；执行后不创建侧车托管配置。

弹窗复用侧车既有 `modal-panel`、`field`、按钮、主题色和 SVG 图标体系。模型列表独立滚动，异步加载提供 loading、error/retry 和 empty 状态；错误通过 `role="alert"` 呈现，窄屏降为单列，且弹窗滚动不会穿透到背景页面。

专属分组授权名单读取不使用主服务通用用户分页接口。侧车复用现有只读 PostgreSQL 连接，通过 `user_allowed_groups.group_id` 索引和 `users` 表执行一次精确关联查询，同时计算用户是否已拥有目标分组权限。授权写入仍逐用户调用主服务管理员 HTTP 接口，以保留业务校验、事务和缓存失效行为。未配置只读数据库时返回明确不可用错误，不回退到可能扫描 `usage_logs` 的通用用户列表。

## Safety

- 仅管理员路由；凭证只在服务器内存中使用，不返回 API Key。
- 限制模型数量、提示词长度、并发和单模型超时。
- 不调用主服务的账号检测接口，不改变账号调度状态、检测历史或自动保护计数。
