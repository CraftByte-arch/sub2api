## Why

侧车页面目前只在首次打开时接收一份管理员 JWT，并在页面会话中长期复用它。Sub2API 后台会轮换 access token，导致侧车停留一段时间后显示无权访问，管理员必须重新打开菜单才能恢复。

## What Changes

- 侧车改为使用同源 Sub2API 浏览器登录态中的 `localStorage` token，不再依赖 URL token 或侧车自己的旧 token。
- 侧车请求遇到过期 access token 时，直接调用同源现有 refresh 接口并重试原请求。
- access/refresh token 都失效时，跳转到 Sub2API `/login`，携带安全的站内 `redirect`，登录完成后回到侧车页面。
- 在侧车刷新页面、后台长时间未打开、以及管理员在其他标签页刷新 token 的情况下保持会话同步。

## Capabilities

### New Capabilities

- `sidecar-admin-session`: 侧车管理员会话的同源读取、续期、失效回跳和跨标签同步。

### Modified Capabilities

<!-- No existing Sub2API capability requirements are changed. -->

## Impact

- 仅修改 `account-auto-scheduler/internal/web/static/app.js` 及其静态测试。
- 复用 Sub2API 现有 `/api/v1/auth/refresh` 和 `/login?redirect=...`，不修改 Sub2API 后端或前端代码。
- 不再把 token 放入侧车 URL；旧 URL token 入口不再作为长期认证来源。
