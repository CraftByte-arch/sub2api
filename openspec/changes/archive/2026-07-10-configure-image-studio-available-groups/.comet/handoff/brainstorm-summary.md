# Brainstorm Summary

- Change: configure-image-studio-available-groups
- Date: 2026-07-09

## 确认的技术方案

本 change 在后台“生图设置”tab 增加两类管理员控制能力：

1. 生图体验可用分组：保存一个 group ID allowlist。内置 image studio 的 API key 下拉只展示 active、OpenAI、分组已开启生图、且分组在 allowlist 内的 key。空 allowlist 表示生图体验没有可用分组。

2. 客户端 `image_generation` tool 声明策略：新增全局策略 `strip` / `allow` / `reject`。推荐默认 `strip`，在 HTTP `/v1/responses` 和 Responses WebSocket 中剥离客户端预声明的 `image_generation` tool 后继续普通请求，避免把工具声明误判成实际生图。`allow` 允许预声明继续通过；`reject` 保持当前严格行为。

实际生图能力继续由现有分组 `allow_image_generation` 控制，包括 `/v1/images/*`、`/image-studio/jobs`、image-only model、显式选择 `image_generation` tool、以及实际生图输出相关路径。

## 关键取舍与风险

- 将工具声明策略做成全局设置，而不是分组设置：后台更简单，适合当前“生图设置 tab 统一管理”目标；未来如需精细控制可扩展为分组覆盖。
- 推荐 `strip` 默认：兼容会预声明工具的客户端，同时避免模型在禁用生图的分组中看到可用 image tool。
- `allow` 策略可能让模型看到工具但实际调用时被拦截；需要文案说明并确保显式/实际生图仍被 group gate 拦截。
- 空生图体验 allowlist 会让现有部署需要管理员配置后才显示 key；通过后台 UI 和前台 empty state 降低困惑。

## 测试策略

- 后端 settings：验证 group ID allowlist 和 declaration policy 的默认值、保存、读取、非法值归一化/拒绝、DTO 同步。
- 后端 gateway：覆盖 `/v1/responses` 中 `strip`、`allow`、`reject`；分组关闭生图时普通预声明不应在 `strip`/`allow` 下直接触发旧错误；显式 tool_choice、专用图片 endpoint、image-only model 仍拒绝。
- WebSocket：覆盖首包/入站 payload 的相同声明策略。
- Image Studio job：allowlist 与现有 group 生图开关共同生效。
- 前端：admin settings tab 保存/回填两个设置；image studio key selector 只展示 allowlist 内 eligible key；空列表 empty state 清晰。

## Spec Patch

已回写 OpenSpec：
- proposal 增加客户端 `image_generation` tool 声明策略。
- delta spec 新增“Client image generation tool declaration policy”要求与场景。
- tasks 增加后端策略、HTTP/WS 应用、后台 UI 策略选择器和测试任务。
