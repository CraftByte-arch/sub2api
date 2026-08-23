## Context

侧车当前把上游 access token、refresh token、Cookie 和用户 ID 放入 `AuthMaterial`，再通过 `AUTO_SCHEDULER_CREDENTIAL_KEY` 对整份材料执行 AES-GCM 加密。密码登录成功后，原始用户名和密码会被丢弃；周期同步只能尝试现有 refresh token/Cookie，刷新失败后身份进入过期状态。

本变更只涉及侧车。它必须兼容旧状态文件、Token/Cookie 登录、Sub2API 的普通或 credential-envelope 密码登录，以及 NewAPI 的密码登录。密码、验证码和会话材料不得出现在公开 API、日志或错误消息中；既有的身份 Principal 展示契约保持不变。

## Goals / Non-Goals

**Goals:**

- 密码登录成功后，把重登所需的用户名和密码保存在现有加密凭证信封内。
- 会话过期时先使用现有 refresh 机制，失败后再进行一次密码重登。
- 密码重登成功后返回并持久化新的会话材料，使当前同步继续完成。
- 旧状态、Token 登录和 Cookie/session 登录保持兼容。

**Non-Goals:**

- 不绕过验证码、Turnstile、2FA、滑块、人机验证或 IP 风控。
- 不在浏览器、公开身份响应、日志或明文状态文件中展示或保存密码与会话材料，也不新增公开的重登用户名字段。
- 不修改 Sub2API 代码、数据库、账号选择、计费或后台登录流程。
- 不为升级前已存在的身份推测或恢复历史密码。

## Decisions

### 1. 扩展现有加密 `AuthMaterial`

增加可选的 `login_username` 和 `login_password` 字段。只有密码登录成功时填充；Token 和 Cookie/session 登录保持为空。字段跟随原有 AES-GCM 信封加密并使用相同的上游 ID、身份 ID 作为关联数据，因此不需要新增明文存储或状态版本迁移。

选择复用现有信封，而不是新增独立密码文件，是因为信封已经具备加密、完整性校验、大小限制和按身份隔离能力，也能让成功刷新或重登后的凭证一次性原子更新。

### 2. refresh 优先，自动重登作为一次性回退

Sub2API 和 NewAPI 都先按现有流程验证会话并尝试 refresh。仅在服务端明确把初始会话判断为过期，且 refresh 不可用、失败或刷新后的会话仍无效时，才检查是否存在完整的加密账号密码；存在时最多执行一次密码登录和一次会话验证。

选择在适配器的验证流程内回退，而不是在 Manager 中循环调用 `Connect`，可以复用各网站类型的登录契约，同时避免递归、重复同步和身份元数据被半途覆盖。

### 3. 重登复用现有网站登录实现

Sub2API 自动重登继续使用 `sub2APILoginMaterial`，因此兼容普通明文登录协议与 RSA credential-envelope 协议；NewAPI 自动重登继续使用 `newAPILoginMaterial`。重登成功产生的材料会保留账号密码并替换 access token、refresh token、Cookie 和用户 ID，随后由 `Manager.syncIdentity` 使用现有逻辑重新加密持久化。

### 4. 安全失败且不无限重试

自动重登不保存验证码答案，不创建持久挑战，也不在同一次同步内再次进入 refresh/relogin 循环。登录接口返回验证码、2FA、访问拒绝、无效密码或网络错误时，直接沿用现有安全分类并等待下一次周期同步或管理员人工处理。

## Risks / Trade-offs

- **[升级前密码身份没有可重登材料]** → 旧信封解密后新增字段为空，继续沿用 refresh-only；管理员重新连接一次后才启用自动重登。
- **[保存密码扩大敏感材料范围]** → 密码仅进入已有 AES-GCM 密文，不进入模型、公开视图、日志或错误文本；测试检查密文和 JSON 响应不含明文。
- **[上游开启验证码或 2FA]** → 单次自动重登会返回现有挑战状态，不尝试绕过，也不循环请求。
- **[密码被上游修改]** → 自动重登安全失败并保留明确状态，管理员重新连接即可替换加密材料。

## Migration Plan

1. 发布兼容读取旧 `AuthMaterial` JSON 的新侧车镜像。
2. 旧身份继续按原逻辑同步；需要密码回退的身份由管理员在页面重新连接一次。
3. 观察侧车健康、周期同步和状态文件持久化，仅重建 `account-auto-scheduler` 容器。
4. 如需回滚，恢复上一侧车镜像；旧版本 JSON 解码会忽略新增字段，已有会话字段仍可使用。

## Open Questions

无。
