## Why

管理员需要在“分组与账号”页面临时验证某个 API Key 账号的指定模型，而不修改自动调度配置或触发后台主动探测。现有直连流式探测能力可复用，但需要一个安全、可控的手动入口。

## What Changes

- 为 API Key 账号增加“手动探测”操作。
- 弹窗支持从账号可用模型中多选模型，并设置自定义提示词和推理强度。
- 后端逐模型执行直连流式探测，返回每个模型的成功、耗时、响应摘要、用量和错误原因。
- 手动探测仅返回结果，不修改自动调度状态、连续失败计数或成功率统计。

## Capabilities

### New Capabilities
- `manual-model-probe`: 管理员对 API Key 账号执行可配置的多模型手动直连探测。

### Modified Capabilities
- 无。

## Impact

仅修改 `account-auto-scheduler` 的管理员 API、静态页面和直连探测编排；复用已有管理员 JWT 凭证导出和 DirectProber，不修改 Sub2API 主服务或自动调度状态。
