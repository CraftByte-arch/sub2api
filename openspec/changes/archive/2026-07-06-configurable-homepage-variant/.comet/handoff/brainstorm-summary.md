# Brainstorm Summary

- Change: configurable-homepage-variant
- Date: 2026-07-06

## 确认的事实与约束

- 容器启动参数使用 `HOMEPAGE_VARIANT=aixw|default`。
- 默认值为 `default`，缺省或非法值都回退到源仓库默认首页。
- 不切换或修改 `main` 分支。
- 现有 `/home` 路由当前直接指向 `AixwHomeView.vue`。
- 源仓库默认首页仍在 `frontend/src/views/HomeView.vue`。
- 后端已有 `window.__APP_CONFIG__` HTML 注入和 `/api/v1/settings/public` 公共设置链路。

## 候选方案

1. 最小侵入推荐方案：后端在 HTML 注入时额外带上 `homepage_variant`，前端新增一个很薄的 homepage selector 组件，`/home` 只从直接指向 `AixwHomeView` 改为指向 selector。selector 内部按 `window.__APP_CONFIG__`/store 字段渲染默认首页或 AIXW 首页。
2. 更少前端文件但更脆弱方案：router 初始化时直接读取 `window.__APP_CONFIG__?.homepage_variant` 决定 `/home` component。文件更少，但模块导入时设置未注入或测试环境缺字段时更容易出边界问题。
3. 不推荐方案：entrypoint 改写静态文件或构建两个前端包。避免运行时组件分支，但对容器脚本和产物侵入更大。

## 确认的技术方案

采用最小侵入方案：不增加数据库设置，不增加管理 UI，不改 main，不做动态 route mutation。只做：

- 后端读取并规范化 `HOMEPAGE_VARIANT`，缺省/非法为 `default`。
- 在 public settings 和 HTML 注入 payload 增加 `homepage_variant` 一个字段。
- 新增 `frontend/src/views/public/HomeVariantView.vue` 作为薄 selector。
- `/home` 路由从 `AixwHomeView.vue` 改到 `HomeVariantView.vue`。
- 更新最少量测试和 `.env.example` 文档。

## 关键取舍与风险

- 取舍：wrapper 组件多一层间接，但保持 route table 稳定，避免动态改路由。
- 风险：非 embedded 开发模式下首屏可能先用默认首页，设置加载后切换。容器/embedded 场景通过 `window.__APP_CONFIG__` 可避免闪烁。
- 风险：public settings DTO、service payload、frontend type 漂移。通过现有 drift test 和新增 focused tests 覆盖。

## 测试策略

- 后端：配置规范化测试覆盖缺省、非法、`default`、`aixw`；public settings/API/injection 字段覆盖。
- 前端：selector 组件测试覆盖 injected settings 为 `default`/`aixw`/缺省；路由测试确认 `/home` 仍公开且指向 selector。
- 验证：运行 focused Go tests 和 frontend vitest。

## Spec Patch

无。
