## Context

侧车静态页面目前把入口 URL 的 `token` 复制到 `sessionStorage`，每个请求都使用这份固定值。管理员后台自身把 access token、refresh token 和过期时间保存在同源 `localStorage`，并已有 `/api/v1/auth/refresh` 轮换接口。侧车和后台同源部署在 `aixw.org`，因此侧车可以直接复用浏览器登录态，但不能把 refresh token 发给侧车服务端。

## Goals / Non-Goals

**Goals:**

- 侧车加载和刷新页面时使用同源最新 access token。
- access token 过期时在浏览器中调用同源 refresh 接口，保存轮换后的 token，并重试原请求。
- refresh token 失效时安全跳转到同源登录页，登录完成后回到侧车路由。
- 后台其他标签页刷新 token 后，侧车下一次请求自动使用新值。

**Non-Goals:**

- 不修改 Sub2API 的认证接口、JWT 配置或登录页面。
- 不把 refresh token、Admin API Key 或 token 写入侧车状态文件、侧车 URL 或侧车后端请求。
- 不承诺跨不同顶级域名部署时共享登录态；不同源部署仍需显式入口认证。

## Decisions

1. **同源 localStorage 作为唯一运行时凭证来源。**
   - 每次 API 请求前读取 `localStorage.auth_token`，避免页面内存中的旧副本。
   - 监听 `storage` 事件仅用于立即更新内存 token 和刷新当前状态，不依赖事件才能正确工作。
   - 不再保存 URL token；这样地址栏、历史记录和反向代理日志不会继续携带管理员 JWT。

2. **复用现有 refresh 合约。**
   - 侧车只在浏览器端向当前 origin 的 `/api/v1/auth/refresh` 发送 `localStorage.refresh_token`。
   - 用单文档 promise 锁避免并发刷新；收到 401 时刷新一次并重试一次，避免循环。
   - 成功后更新 `auth_token`、轮换后的 `refresh_token` 和 `token_expires_at`。

3. **登录回跳使用站内路径。**
   - 失效时优先跳转顶层窗口（iframe 场景）到 `/login?redirect=/custom/account-auto-scheduler`；独立打开时跳当前窗口。
   - 只传固定的站内路径，不把完整外部 URL 放进 `redirect`，降低开放重定向风险。
   - 现有登录页完成登录或 2FA 后会按 `redirect` 返回侧车页面。

4. **降级行为。**
   - URL 中遗留的 token 不再作为长期凭证；若部署缓存中的旧页面仍带 token，仅用于一次性迁移到同源 localStorage，随后立即从地址栏清除。
   - 非同源或未登录时显示明确的登录提示，避免把普通上游 API 错误误报成管理员无权。

## Risks / Trade-offs

- [同源 localStorage 对所有同源脚本可读] → 继续依赖 Sub2API 当前 CSP、XSS 防护和 HTTPS；不新增跨源暴露。
- [refresh token 轮换竞争] → 采用现有刷新接口的单文档锁和快照校验，发现其他标签页已轮换时采用其新 token。
- [登录页回跳依赖前端路由] → 使用固定 `/custom/account-auto-scheduler` 路径并增加静态测试；若管理员删除自定义菜单，登录后仍可落到该路由并由侧车页面显示。

## Migration Plan

发布侧车静态资源后，管理员重新打开一次侧车即可建立同源 token 读取；后续侧车刷新、后台长时间未打开和 token 轮换均自动处理。旧 `sessionStorage` token 可由新代码忽略或清理，不需要状态数据迁移。
