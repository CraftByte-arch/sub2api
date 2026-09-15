## Why

上游管理目前只能看到登录身份下的 API Key，管理员无法确认该身份可用哪些上游分组、各分组的倍率，以及充值倍率换算后的实际成本。将这些信息直接展示在同一处，才能在绑定账号和设置倍率保护前做出正确判断。

## What Changes

- 在侧车服务保存上游身份同步时已获取的可用分组、分组倍率和平台类型快照，不增加额外的上游请求。
- 在上游管理的每个登录身份下展示全部可用分组、OpenAI / Anthropic / Gemini / GROK 类型、分组倍率和计算后的最终倍率。
- 对上游未返回倍率、未配置充值倍率或同步暂时失败的情形显示明确状态，避免将未知数据误显示为零或有效倍率。

## Capabilities

### New Capabilities

- `upstream-group-rate-visibility`: 在侧车上游管理中按登录身份展示上游可用分组、平台类型、原始倍率和最终倍率。

### Modified Capabilities

- 无。

## Impact

- 仅影响 `account-auto-scheduler` 侧车的上游同步快照、状态持久化、公开管理 API 及上游管理前端。
- 复用 Sub2API 与 NewAPI 同步流程已请求的数据；不修改 Sub2API 主服务，也不增加上游轮询频率。
