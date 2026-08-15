## Why

上游探测倍率描述的是上游返回或探测到的价格信号，不包含管理员配置的充值汇率，因此不能准确代表实际成本。当前如果直接用它做利润优先调度，可能会选中探测倍率较低但最终成本更高的账号；同时不能复用 `accounts.rate_multiplier`，因为该字段会改变账号成本统计和配额计费。

## What Changes

- 为 API Key 账号增加独立的 `final_cost_multiplier` 调度字段，不改变现有 `rate_multiplier` 的计费语义。
- 侧车在上游 Key 倍率或充值倍率变化后计算并同步最终成本倍率。
- Sub2API 调度器在利润优先信号可用时使用最终成本倍率；最终成本倍率未设置、无效或不可用时完全回退到原本的探测倍率/原本选号逻辑。
- 保留探测倍率用于状态检测、诊断和现有展示，不将其改写为最终成本倍率。
- 对未绑定、绑定歧义或未配置充值倍率的账号不写入最终成本倍率，避免错误影响调度。
- Sub2API 单独部署且没有侧车写入该字段时，账号选择、利润准入、计费、配额和用量记录与原版本完全一致。

## Capabilities

### New Capabilities

- `final-cost-scheduling`: 使用独立最终成本倍率进行利润优先账号调度，并在字段缺失时保持原有行为。

### Modified Capabilities

<!-- No existing OpenSpec capability has a matching scheduling requirement. -->

## Impact

- Sub2API：仅在 OpenAI 利润/上游成本调度读取点接入独立成本信号，并增加对应单元测试；不新增账号管理 DTO 或专用接口。
- account-auto-scheduler：最终倍率计算结果通过现有管理员批量更新接口合并到账号 Extra；不修改账号 `rate_multiplier`。
- 数据库：优先复用现有 `accounts.extra` JSONB 或等价非计费字段，避免新增计费列和历史数据迁移；具体落点在设计阶段确定。
- 兼容性：最终成本倍率为空时，调度、计费、配额和原有探测逻辑保持不变。
