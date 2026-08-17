## Why

部分 Sub2API 兼容站点要求浏览器凭据加密流程：登录前取得临时公钥和流程 Cookie，再提交 `credential_envelope`，否则拒绝普通账号密码请求。当前侧车只支持传统账号密码 JSON，因此这些站点即使验证码正确也无法登录，并被误报为管理站点拒绝访问。

## What Changes

- 为侧车的 Sub2API 密码登录增加 `/api/v1/auth/credential-key` 能力探测。
- 对支持 `RSA-OAEP-256+A256GCM` 的站点，在服务器内存中生成与浏览器一致的凭据加密信封，并携带同源流程 Cookie 登录。
- 对明确不支持该接口的传统 Sub2API 站点保留现有明文 JSON 登录，包括 404、405 和明确的前端 HTML 回退响应。
- 对已响应安全凭据协议但返回无效、过期或不受支持材料的站点安全失败，不降级发送账号密码。
- 保持 NewAPI、Token、Cookie/session、验证码挑战、状态文件和管理员页面协议不变，并提供准确的错误分类。

## Capabilities

### New Capabilities

- `sub2api-credential-envelope-login`: Sub2API 密码登录的加密凭据能力探测、安全信封生成、同源 Cookie 传递和传统登录回退行为。

### Modified Capabilities

<!-- No existing main specification covers the sidecar upstream login behavior. -->

## Impact

- 影响范围仅限 `account-auto-scheduler/internal/upstream` 的 Sub2API 密码登录适配器及其测试。
- 不修改 Sub2API 主服务、数据库、侧车状态文件结构、管理员 API 或前端请求格式。
- 使用 Go 标准库完成 RSA-OAEP、AES-256-GCM、SPKI 公钥解析、随机数和 base64url 编码，不增加第三方依赖。
