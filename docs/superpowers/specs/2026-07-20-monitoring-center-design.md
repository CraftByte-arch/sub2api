# 统一监控中心（Monitoring Center）设计

日期：2026-07-20
状态：已确认
分支：codex/feature/20260719/new-requirement

## 背景与目标

现有两个监控类页面：

- **首token监控** `/admin/ttft`（`frontend/src/views/admin/ttft/`）
- **账号性能** `/admin/performance`（`frontend/src/views/admin/performance/`）

存在的问题：

1. 两个页面概念高度重叠（都有 TTFT 超时率、失败分布、账号维度表格），用户难以判断该看哪个。
2. 两个页面均绕过项目共享组件库（`StatCard`、`DataTable`、`Toggle`、`Select`、`EmptyState`、`Pagination` 等），全部手写样式，且两套手写风格互相不一致（圆角、按钮高度、配色均不同），与后台其他页面风格割裂。
3. 首token监控页顶部被低频的"超时保护设置"面板占据；指标卡使用蓝/红/绿/紫/橙彩虹色图标。
4. 账号性能页文案全部硬编码中文，未走 i18n。
5. 平台、模型筛选为自由文本输入，易输错；`PerformanceInvestigationDrawer` 名为抽屉实为 Dialog。

**目标**：合并为一个统一监控中心，在现有共享组件基础上做现代化提升，覆盖四个核心场景：整体健康概览、定位问题账号、验证 TTFT 参数效果、分析失败原因。

## 硬约束：最小化与 main/上游的合并冲突

main 后续要同步上游源仓库。经 git 核实：

- ttft/performance 的**前后端文件全部只存在于本分支**（main 上不存在），重写或删除它们零冲突风险。
- 真正的冲突点是 on-main 的"注册处"文件，对它们只做**追加式小编辑**：
  - `frontend/src/router/index.ts`：+1 路由、+2 重定向
  - `frontend/src/i18n/locales/{zh,en}/admin/index.ts`：各 +2 行（import + spread）
  - `backend/internal/server/routes/admin.go`：+注册函数与调用
  - `backend/internal/handler/handler.go`：+1 个 handler 字段（追加）
  - 导航配置（AppLayout 或 nav 配置）：+1 菜单项、−2 旧菜单项
- **不修改任何 `components/common/*` 共享组件**；需要新样式时在 monitoring 目录内自建组件。
- `wire_gen.go` 通过代码生成更新，不手改。

## 信息架构与路由

- 新页面 `/admin/monitoring`（监控中心），导航中"首token监控"与"账号性能"合并为一个菜单项。
- `/admin/ttft`、`/admin/performance` 永久重定向到 `/admin/monitoring`，保留旧链接兼容（含 query 中的 range/protocol/platform 等参数迁移到新页筛选状态，能映射的映射，不能映射的丢弃）。
- TTFT 超时保护设置收进弹窗：页面头部显示状态徽章（`已启用 · 30s` / `未启用`）+ "调整"按钮，点击弹出基于 `BaseDialog` 的设置对话框，使用共享 `Toggle` 组件替换原生 checkbox，保留 1–300 秒整数校验。保存仍调用现有 `/admin/ttft/settings` 端点。

## 页面结构（自上而下）

1. **头部**：标题 + 采集健康徽章 + TTFT 保护徽章（含"调整"入口）+ 筛选器（时间范围 segmented 按钮组 `1h/6h/24h/7d/30d/90d`、平台改为共享 `Select` 下拉、模型输入框）+ 刷新按钮。筛选状态同步到 URL query。

   时间范围为两页现有能力的并集：performance 侧已支持全部六档；TTFT stats 侧目前仅 `24h/7d/30d/90d`，需扩展支持 `1h/6h`——改动点为三处 branch-only 代码（service 的 `FirstTokenStatsRange` 常量、handler 的 `parseFirstTokenStatsRange`、repository 的 range→duration switch）。TTFT 统计桶粒度为 1 小时，短范围下趋势点数较少，可接受。
2. **KPI 行（5 卡）**：可用率、失败率、TTFT 超时率、换号恢复率、P95 TTFT。统一使用新组件 `MetricTrendCard`（大数字 + 一行上下文 + 迷你趋势线 sparkline），全部走语义色（绿=好、红=差、琥珀=警告、灰=中性），去除彩虹色图标方块。
3. **首Token保护路径漏斗**：横向 4 段漏斗条（受控请求 → 触发超时 → 换号恢复 → 最终失败），段间标注转化率。无受控请求时整块隐藏（沿用现有 `controlled_requests > 0` 判断）。
4. **趋势区**：两张并排折线图（chart.js）：
   - 比率趋势：可用率 / 失败率 / TTFT 超时率
   - 延迟趋势：P50 TTFT / P95 TTFT / P95 总耗时
   统一配色、图例、坐标轴格式化与暗色模式处理。
5. **账号健康表**（核心工作区）：合并现有两张账号表。列 = 账号 / 平台徽章（复用 `PlatformTypeBadge`）/ 健康状态（健康·关注·风险·样本不足，沿用现有 health_score 阈值 0.9/0.6 与 low_sample 规则）/ 可用率 / 失败率 / TTFT 超时率 / P95 TTFT / 样本数。支持列排序、账号搜索（300ms 防抖）、分页（共享 `Pagination`）。点击行打开钻取抽屉。
6. **钻取抽屉**：升级现有 `PerformanceInvestigationDrawer`（迁移改名，仍为 Dialog 形态）：账号指标卡（可用率/失败率/P95 TTFT/P95 总耗时）+ 该账号趋势图 + 该账号失败分布。
7. **失败分布**：横向条形图，失败类型使用统一色板与中文标签（沿用 performance 页的 outcomeLabels 映射）。

## 后端设计

- **新增聚合端点** `GET /api/v1/admin/monitoring/overview`：新建 handler 文件（`backend/internal/handler/admin/` 下），组合现有 `AccountPerformanceService` 与 FirstTokenTimeout service 的只读方法，一次返回：KPI 汇总、漏斗数据、比率/延迟趋势、失败分布、采集健康、TTFT 保护生效配置。除下文两处明确列出的扩展外，两个 service 内部逻辑零改动。
- **后端小扩展（均为 branch-only 代码）**：
  - TTFT stats 时间范围扩展 `1h/6h`（见"页面结构"第 1 节）。
  - `/admin/performance/accounts` 补充 `search` 参数（见下文"账号搜索"）。
- **复用不动的端点**：
  - `GET /admin/performance/accounts`（账号健康表）
  - `GET /admin/performance/investigation`（钻取抽屉）
  - `GET/PUT /admin/ttft/settings`（设置弹窗）
- 账号表的 TTFT 超时率列：`PerformanceAccountItem.counters` 已包含 `ttft_timeout_count` 与 `attempt_count`，前端直接派生 `ttft_timeout_count / attempt_count`，不新增后端字段。
- 账号搜索：`GET /admin/performance/accounts` 目前不支持 `search` 参数（仅旧 TTFT accounts 端点支持）。为保持合并后能力不回退，为该端点补充 `search` 参数（按账号名/ID 模糊匹配）——handler 过滤解析与 service/repo 查询条件均为 branch-only 代码。
- 前端新增 API client `frontend/src/api/admin/monitoring.ts`。

## 前端组件清单

新建目录 `frontend/src/views/admin/monitoring/`：

| 组件 | 来源 |
|---|---|
| `MonitoringView.vue` | 新建（页面编排、筛选状态、加载/错误/空态） |
| `components/MetricTrendCard.vue` | 以 `PerformanceMetricCard.vue` 为蓝本改造（语义色、去彩虹图标） |
| `components/ProtectionFunnel.vue` | 以 `TTFTRecoveryFunnel.vue` 为蓝本改造（段间转化率） |
| `components/MonitoringTrendChart.vue` | 合并 `PerformanceTrendChart.vue` 与 `TTFTFailureTrendChart.vue` 的通用能力 |
| `components/AccountHealthTable.vue` | 以 `PerformanceAccountTable.vue` 为蓝本，补 TTFT 超时率列与搜索 |
| `components/InvestigationDrawer.vue` | 迁移改造 `PerformanceInvestigationDrawer.vue` |
| `components/FailureDistribution.vue` | 迁移 `PerformanceFailureDistribution.vue` |
| `components/TTFTSettingsDialog.vue` | 以 `TTFTSettingsBar.vue` 逻辑改造为 Dialog + `Toggle` |

共享组件仅使用、不修改：`Select`、`SearchInput`、`EmptyState`、`Pagination`、`Toggle`、`BaseDialog`、`Skeleton`、`Icon`、`PlatformTypeBadge`。

i18n：新建 `frontend/src/i18n/locales/{zh,en}/admin/monitoring.ts`，页面全部文案走 i18n；旧 `ttft.ts` locale 文件随旧页面一并删除（branch-only，无冲突风险），performance 页原本硬编码中文的问题随重写自然消除。

## 旧代码处置

- 删除 `frontend/src/views/admin/ttft/`、`frontend/src/views/admin/performance/`（组件先按上表迁移改造）。
- 删除 `frontend/src/api/admin/ttft.ts` 中仅旧页面使用的部分；settings 相关调用迁入 `monitoring.ts` client。
- 后端 ttft/performance 的 handler、service、路由**全部保留**（设置、账号表、钻取端点仍在用；聚合端点也复用这些 service）。

## 错误处理与加载/空态

- overview 聚合数据：首屏 skeleton；加载失败显示错误条 + 重试；已有数据时刷新失败保留旧数据并提示。
- 账号表：独立的加载/错误/重试状态；沿用现有 generation 计数器防竞态模式。
- 无样本（attempts = 0）：共享 `EmptyState` 组件引导文案。
- 采集降级（collection_health.status = degraded）：头部徽章变琥珀色 + 页面顶部说明横幅（含丢弃/待写入样本数）。

## 测试

- 前端 Vitest：
  - `MetricTrendCard`：渲染、sparkline 归一化、语义色
  - `ProtectionFunnel`：转化率计算、无受控请求时隐藏
  - `AccountHealthTable`：排序切换、健康状态阈值、行点击钻取
  - `TTFTSettingsDialog`：校验（1–300 整数）、保存事件
  - 路由：`/admin/ttft`、`/admin/performance` 重定向到 `/admin/monitoring`
- 后端 Go 单测：聚合 handler 组合两个 service 的正常路径与错误传递。
- i18n：沿用项目现有 locale 编译/key 完整性 spec 模式，为新 locale 文件补对应测试。

## 非目标（YAGNI）

- 不改性能/TTFT 指标的采集逻辑与口径（TTFT stats 仅扩展 `1h/6h` 时间范围枚举）。
- 不引入新图表库（继续 chart.js）。
- 不做自动刷新轮询、告警通知、自定义仪表盘布局。
- 不删除/合并后端 ttft、performance 的既有端点。
