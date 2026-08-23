## Why

上游身份的 access token、refresh token 或 Cookie 最终都可能失效，当前侧车只能把身份标记为过期并等待管理员重新连接，导致余额、Key 和倍率同步长期中断。对于已由管理员使用账号密码连接的上游，侧车应当在常规会话刷新失败后安全地自动重新登录并恢复同步。

## What Changes

- 对密码登录模式，在现有加密凭证信封中保存重新登录所需的用户名和密码；不新增公开密码、Token 或重登字段，既有身份 Principal 展示保持不变。
- 保留现有 access token、refresh token 和 Cookie 刷新优先级；只有常规刷新不可用或失败时，才使用保存的密码进行至多一次自动重登。
- 自动重登成功后持久化新的会话材料，并继续本轮余额、Key 和倍率同步。
- Token、Cookie/session 身份保持原行为，不尝试密码重登。
- 验证码、2FA、人机验证或 IP 风控仍要求管理员人工处理，不保存验证答案，也不循环重试。
- 旧的密码身份在管理员重新连接一次之前继续使用原有 refresh-only 行为。

## Capabilities

### New Capabilities

- `upstream-password-auto-relogin`: 上游密码登录材料的加密保存、刷新失败后的单次自动重登、兼容与安全边界。

### Modified Capabilities

<!-- No existing Sub2API capability requirements are changed. -->

## Impact

- 仅修改 `account-auto-scheduler` 的上游凭证、Sub2API/NewAPI 适配器及相关测试。
- 不修改 Sub2API 生产代码、数据库、账号选择或计费逻辑。
- 旧状态文件无需迁移；新增敏感字段只存在于现有加密凭证密文内。
- HC2 发布时只重建 `account-auto-scheduler` 容器，现有 Sub2API 槽位保持不变。
