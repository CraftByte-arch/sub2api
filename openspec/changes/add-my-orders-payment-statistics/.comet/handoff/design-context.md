# Comet Design Handoff

- Change: add-my-orders-payment-statistics
- Phase: design
- Mode: compact
- Context hash: d6e3fcdeb92bf14b4d2f7b9abfb2a339c901ce0f4a40c0e1f59050651dedad56

Generated-by: comet-handoff.sh

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## openspec/changes/add-my-orders-payment-statistics/proposal.md

- Source: openspec/changes/add-my-orders-payment-statistics/proposal.md
- Lines: 1-32
- SHA256: 150a39fe8c6064f0f43a71c4e1ac922647266e7ff9ffe2beac379cd691a0c55d

```md
## Why

用户当前只能在“我的订单”中逐页查看订单，无法按时间快速了解自己的实际支付总额、订单类型构成和每日变化。新增独立的订单统计页可以提供这些只读视图，同时避免修改现有订单页，降低与 `main` 分支并行开发时的冲突风险。

## What Changes

- 新增一个仅登录用户可访问的“订单统计”页面和相邻的用户导航入口，现有“我的订单”页面保持不变。
- 新增用户级订单统计 API，按所选时间范围和浏览器 IANA 时区，仅聚合当前用户已有的支付订单。
- 提供最近 7/30/90 天快捷范围和最长 366 个自然日的自定义日期范围。
- 统计固定使用人民币 CNY，返回区间总实付金额、成功订单数和平均实付金额，不提供币种切换、换算或分组。
- 按现有订单类型 `balance`、`usage_card`、`subscription` 聚合实付金额、订单数和平均实付金额。
- 按自然日聚合实付金额、订单数和平均实付金额，并以日期倒序列表展示。
- 类型聚合行和每日统计行支持只读下钻；点击后在弹窗中分页展示对应订单号、类型、实付金额、状态、支付方式和支付时间。
- 统计只包含已有 `paid_at` 的 `PAID`、`RECHARGING`、`COMPLETED` 订单，金额口径为原始 `pay_amount`；不处理退款或站外手工退款修正。
- 不修改数据库表、索引或 migration，不改变订单创建、支付、分页、取消等既有行为。

## Capabilities

### New Capabilities

- 无。

### Modified Capabilities

- `payment`: 增加登录用户按时间、订单类型和自然日查看个人人民币实付订单统计及只读下钻明细的行为要求。

## Impact

- 前端：新增用户订单统计视图、下钻弹窗、路由、侧边栏入口、API 类型与中英文文案；复用现有对话框、表格、分页和加载组件。
- 后端：扩展用户支付路由、handler 和 payment service，直接查询现有 `payment_orders` 数据，并保证汇总和下钻都强制限定当前认证用户。
- 测试：增加统计口径、整数分计算、时间边界、用户隔离、类型/每日聚合、下钻一致性、参数校验和新页面交互测试。
- 数据与依赖：无 schema 变更、无 migration、无新运行时依赖。
```

## openspec/changes/add-my-orders-payment-statistics/design.md

- Source: openspec/changes/add-my-orders-payment-statistics/design.md
- Lines: 1-95
- SHA256: c1fd47833b955292bf95921ff9dd1d6e0143dc41c9e32e3c9f8e2ef4e2563293

[TRUNCATED]

```md
## Context

用户支付订单已经包含统计所需的用户、订单状态、订单类型、实付金额、支付时间和支付渠道。当前用户端 `/orders` 页面只负责订单查询与操作，管理端虽有支付概览，但数据范围是全站且交互只支持固定天数，不能直接暴露给普通用户。

本变更需要横跨用户路由、支付 handler/service、前端 API 和用户导航，但不得修改数据库结构。为减少与 `main` 中订单页并行改动的冲突，统计能力放在新的独立页面中，`frontend/src/views/user/UserOrdersView.vue`、`/orders` 和现有 `GET /api/v1/payment/orders/my` 契约均不参与本变更。

## Goals / Non-Goals

**Goals:**

- 为登录用户提供只读、严格按当前用户隔离的人民币订单实付统计。
- 支持 7/30/90 天快捷范围和自定义日期范围，并按浏览器 IANA 时区划分自然日。
- 提供单一区间汇总、三种订单类型聚合和日期倒序的每日聚合。
- 允许从类型聚合和每日聚合下钻到固定每页 20 条的只读订单列表。
- 复用现有订单字段、鉴权、日期语义和视觉规范，不引入 schema 或依赖变化。
- 将实现集中在新增页面和统计专用接口，保持现有订单页行为与文件不变。

**Non-Goals:**

- 不提供退款、手工退款修正、净收入或退款操作。
- 不修改管理端支付概览、订单生命周期、支付回调或现有订单操作。
- 不提供趋势图、导出、币种切换、汇率换算或多币种分组。
- 不提供超过 366 个自然日的单次查询、长期预聚合表或缓存。

## Decisions

### 1. 使用独立用户页面和独立路由

新增 `frontend/src/views/user/UserOrderStatisticsView.vue`，页面路由使用 `/order-statistics`，路由名称使用 `OrderStatistics`。侧边栏在“我的订单”后增加“订单统计”入口，沿用 `requiresPayment` 和 `flagPayment` 访问规则。使用与 `/orders` 不同的顶级路径，避免侧边栏的前缀匹配同时激活两个菜单项。

选择独立页面而不是在 `UserOrdersView.vue` 内嵌统计，是为了隔离加载状态、日期筛选、聚合列表和弹窗状态，并减少对高冲突文件的修改。统计页和下钻弹窗均不提供取消、退款或其他订单写操作。

### 2. 使用共享的自然日范围解析结果

两个统计端点共同解析 `start_date`、`end_date` 和 `timezone`，生成包含规范化日期、IANA location、绝对时间 `startInclusive` 与 `endExclusive` 的内部范围对象。前端 API client 已为 GET 请求注入浏览器 IANA 时区；时区缺省时回退站点时区，显式非法时区返回 400。

日期均使用 `YYYY-MM-DD`。未提供起止日期时使用包含当天在内的最近 30 个自然日；只提供一端、日期无效、结束早于开始或首尾合计超过 366 天均返回 400。日期边界先在有效时区构造，再以半开区间 `[startInclusive, endExclusive)` 查询，从而正确覆盖 DST 跳变日。

### 3. 新增独立汇总 API

在现有认证支付路由下新增 `GET /api/v1/payment/orders/statistics`。handler 必须通过 `requireAuth` 获取 `subject.UserID`，服务调用不接受客户端传入的用户 ID。静态路由在 `/:id` 之前注册并加入路由契约测试。

服务层按当前用户、非空 `paid_at`、共享半开时间范围、状态集合 `PAID`、`RECHARGING`、`COMPLETED` 以及三种受支持 `order_type` 查询，只选择 `id`、`pay_amount`、`order_type` 和 `paid_at` 等聚合需要字段。结果由纯函数一次遍历生成：

- `summary`：区间总实付金额、成功订单数和平均实付金额。
- `by_type`：固定按 `balance`、`usage_card`、`subscription` 返回三行，缺失类型补零。
- `daily`：按有效时区的本地日期聚合，只返回有订单的日期并按日期倒序。

响应顶层固定返回 `currency: "CNY"`，不读取或分组订单币种快照。站点当前只支持人民币；未来如需外币统计，必须另开规格变更。

### 4. 以整数分执行金额聚合

每笔两位小数的 `pay_amount` 在进入聚合器时转换为整数分，所有求和和平均计算均基于整数分。总额在响应边界转换回两位小数；平均值使用 `总分 / 订单数` 并统一按分四舍五入，零订单时返回 `0.00`。这样避免直接累加 `float64` 导致展示金额漂移，同时不改变现有数据库字段。

无匹配订单时，`summary` 返回零值，`by_type` 仍返回三种类型的零值行，`daily` 返回空数组。稳定的响应形状让前端无需用伪造数据区分加载失败和真实零值。

### 5. 新增数据库分页的下钻 API

新增 `GET /api/v1/payment/orders/statistics/details`。请求复用 `start_date`、`end_date`、`timezone` 和共享统计谓词，并要求 `order_type` 与 `date` 必须且只能提供一个：

- 类型下钻：`order_type` 只允许 `balance`、`usage_card`、`subscription`，在当前已应用时间范围内过滤。
- 日期下钻：`date` 必须是当前已应用范围内的有效本地日期，只查询该自然日且包含全部三种类型。

端点只接受页码，页大小固定为 20；数据库分别执行 `Count` 和分页查询，按 `paid_at DESC, id DESC` 稳定排序。响应使用标准分页结构，明细 DTO 仅包含 `out_trade_no`、`order_type`、`pay_amount`、`status`、`payment_type`、`paid_at`。汇总和下钻复用相同的用户、状态和时间谓词，确保点击行所见的 `total` 与对应聚合 `order_count` 一致。

### 6. 统计页使用紧凑列表和只读弹窗

页面顶部提供 7/30/90 天分段快捷项与自定义起止日期。默认选择最近 30 天。快捷项立即成为已应用范围并查询；自定义日期维护 draft/applied 两套状态，只有用户点击“查询”且请求成功后才更新已应用范围，避免失败请求改变当前统计和下钻上下文。

页面依次展示三个概要指标、三行类型聚合表和每日统计表。类型行与每日行整行可点击并支持键盘操作，打开 `BaseDialog` 的宽版只读明细；弹窗内使用 `DataTable` 和固定 20 条的 `Pagination`，在窄屏提供受控横向滚动。金额统一使用人民币格式化，订单状态和支付方式复用现有本地化语义。

### 7. 分离页面和弹窗请求生命周期

页面汇总与弹窗明细各自维护 loading/error/data 状态和递增请求代次。范围切换、重试、弹窗翻页或关闭后，旧请求即使较晚返回也不得覆盖当前状态。页面初次失败显示带重试的错误状态；弹窗失败保留弹窗和下钻上下文并在内部重试。关闭后重开或选择另一行时重置页码为 1。

页面在汇总成功但无订单时显示明确空状态，同时保留零值概要和三种类型行。网络或服务错误不能以零值响应伪装成功。

## Risks / Trade-offs

- [一年内单个用户订单量异常大时，服务层汇总会占用额外内存] -> 查询仅选择必要字段、限制 366 天并只按当前用户查询；下钻继续使用数据库计数和分页。若未来真实负载需要，可在不改变 API 的前提下替换汇总实现。
```

Full source: openspec/changes/add-my-orders-payment-statistics/design.md

## openspec/changes/add-my-orders-payment-statistics/tasks.md

- Source: openspec/changes/add-my-orders-payment-statistics/tasks.md
- Lines: 1-37
- SHA256: a69d16c4e409196e8a2b06f29c738f9343847751e0216517e9f98f339aeab7af

```md
## 1. 后端统计范围与聚合

- [ ] 1.1 为默认 30 天、显式起止日期、浏览器时区、DST 日界线、非法参数和 366 天上限编写失败测试。
- [ ] 1.2 实现共享日期范围解析器，生成规范化日期、有效 IANA location 和半开绝对时间区间。
- [ ] 1.3 为 `PAID`、`RECHARGING`、`COMPLETED` 状态过滤、用户隔离、缺少 `paid_at`、三种类型补零和每日倒序编写聚合测试。
- [ ] 1.4 为小数金额累计与平均舍入编写失败测试，并实现整数分转换和纯聚合函数。
- [ ] 1.5 实现最长 366 天的当前用户有界字段查询，返回固定 CNY 的单一 `summary`、三行 `by_type` 和 `daily`。

## 2. 用户统计与下钻 API

- [ ] 2.1 为未认证请求、有效统计查询、用户 ID 强制取自认证主体、固定 CNY 响应和服务错误映射编写 handler 测试。
- [ ] 2.2 新增 `GET /api/v1/payment/orders/statistics` handler 和认证静态路由，并验证其不受 `/:id` 路由影响。
- [ ] 2.3 为下钻选择器互斥、非法类型/日期、范围外日期、固定每页 20 条和稳定排序编写失败测试。
- [ ] 2.4 新增 `GET /api/v1/payment/orders/statistics/details`，使用数据库计数和分页并返回最小只读 DTO。
- [ ] 2.5 增加一致性测试，验证类型/日期聚合的 `order_count` 与对应下钻 `total` 相等且不会泄露其他用户数据。

## 3. 独立订单统计页面

- [ ] 3.1 增加统计与下钻请求/响应类型、payment API 方法及参数和固定分页契约测试。
- [ ] 3.2 为新页面编写组件测试，覆盖默认 30 天、7/30/90 天快捷范围、自定义 draft/applied 状态、加载、错误、重试和空状态。
- [ ] 3.3 实现 `UserOrderStatisticsView.vue`，展示人民币总实付、成功订单数、平均实付、三种类型聚合和按日列表。
- [ ] 3.4 增加 `/order-statistics` 路由、支付 feature flag 保护、相邻侧边栏入口和中英文文案，并测试菜单可见性与激活状态。

## 4. 聚合下钻弹窗

- [ ] 4.1 为类型行与每日行的鼠标/键盘下钻、弹窗上下文、关闭重开和页码重置编写组件测试。
- [ ] 4.2 实现整行可操作的类型/每日列表，以及使用 `BaseDialog`、`DataTable` 和固定每页 20 条 `Pagination` 的只读明细弹窗。
- [ ] 4.3 覆盖弹窗加载、空状态、内部错误重试、翻页以及订单号、类型、金额、状态、支付方式和支付时间展示。
- [ ] 4.4 为页面与弹窗加入独立请求代次防护，测试较晚返回的旧请求在范围切换、下钻切换、翻页或关闭后被忽略。

## 5. 回归验证与交付

- [ ] 5.1 增加回归断言，确认 `UserOrdersView.vue`、`/orders` 和 `GET /api/v1/payment/orders/my` 未被修改。
- [ ] 5.2 运行 payment service、handler、路由/API 契约相关 Go 测试及 race/vet 检查。
- [ ] 5.3 运行新增页面 Vitest、前端 lint、typecheck 和生产构建。
- [ ] 5.4 在桌面和移动视口做统计页视觉检查，覆盖长订单号、长日期、空状态和弹窗表格横向滚动，确认无溢出、重叠或布局跳动。
- [ ] 5.5 运行 `openspec validate add-my-orders-payment-statistics --type change --strict --no-interactive` 和 `git diff --check`，记录验证结果。
```

## openspec/changes/add-my-orders-payment-statistics/specs/payment/spec.md

- Source: openspec/changes/add-my-orders-payment-statistics/specs/payment/spec.md
- Lines: 1-185
- SHA256: b9c4e322be878150cef95d84e640760c2c45cf1b5ac902282828f16a5272f506

[TRUNCATED]

```md
## ADDED Requirements

### Requirement: Authenticated users can access a standalone order statistics page
The system SHALL provide an authenticated user-facing order statistics page that is separate from the existing “My Orders” page and is visible only when the payment feature is available.

#### Scenario: User opens order statistics from navigation
- **WHEN** an authenticated user selects the “订单统计” navigation entry
- **THEN** the system opens the standalone `/order-statistics` page
- **AND** the page loads personal payment statistics without loading the paginated order list

#### Scenario: Existing order page remains unchanged
- **WHEN** the order statistics capability is introduced
- **THEN** the existing `/orders` page continues to provide its current filtering, pagination, cancellation, and refund-request behavior
- **AND** no statistics controls or results are embedded into that page
- **AND** the existing `GET /api/v1/payment/orders/my` contract remains unchanged

#### Scenario: Payment feature is unavailable
- **WHEN** the payment feature is not available to the current user
- **THEN** the “订单统计” navigation entry follows the same visibility and route-access policy as “我的订单”

### Requirement: Personal order statistics are user-isolated and time-bounded
The system MUST calculate order statistics only from the authenticated user's payment orders whose `paid_at` falls within the selected inclusive calendar-date range in the effective timezone, whose status is `PAID`, `RECHARGING`, or `COMPLETED`, and whose `order_type` is `balance`, `usage_card`, or `subscription`.

#### Scenario: Default date range is used
- **WHEN** an authenticated user requests statistics without explicit start and end dates
- **THEN** the system uses the most recent 30 calendar days including the current day in the effective timezone
- **AND** the response identifies the normalized start date, end date, and timezone

#### Scenario: Valid custom date range is used
- **WHEN** the user selects an inclusive custom range of at most 366 calendar days
- **THEN** the system includes only qualifying orders paid from the selected start date at 00:00 through the instant before the day after the selected end date in that timezone

#### Scenario: Date range is invalid
- **WHEN** a request provides only one range boundary, an invalid date, an explicit invalid IANA timezone, an end date before the start date, or more than 366 inclusive calendar days
- **THEN** the system rejects the request with HTTP 400
- **AND** it does not return partial or fallback statistics

#### Scenario: Timezone is omitted
- **WHEN** a request does not provide a timezone
- **THEN** the system uses the configured site timezone for both query boundaries and daily grouping

#### Scenario: Another user's orders exist in the same range
- **WHEN** qualifying orders owned by other users exist within the selected range
- **THEN** none of their amounts, counts, types, or dates contribute to the authenticated user's response

#### Scenario: Unpaid or unsupported-status orders exist
- **WHEN** the current user has orders without `paid_at` or with a status outside `PAID`, `RECHARGING`, and `COMPLETED`
- **THEN** those orders do not contribute to any statistic or drilldown result

#### Scenario: An unsupported order type exists
- **WHEN** the current user has an order type outside `balance`, `usage_card`, and `subscription`
- **THEN** that order does not contribute to the summary, type aggregates, daily aggregates, or drilldown results

#### Scenario: Statistics request is unauthenticated
- **WHEN** a request to either statistics endpoint has no valid authenticated subject
- **THEN** the system returns HTTP 401
- **AND** no order data is returned

### Requirement: Selected-range totals use a single CNY payment basis
The system SHALL return one CNY summary for the selected range using each qualifying order's original `pay_amount`, without currency selection, conversion, or grouping.

#### Scenario: Qualifying orders exist
- **WHEN** one or more qualifying orders exist in the selected range
- **THEN** the response declares `currency` as `CNY`
- **AND** `summary` contains the sum of `pay_amount`, the successful order count, and the arithmetic average `pay_amount`

#### Scenario: Amounts are aggregated
- **WHEN** qualifying `pay_amount` values are accumulated
- **THEN** the system converts each value to integer cents before summing
- **AND** it rounds the final average to the nearest cent
- **AND** returned amounts contain at most two decimal places without floating-point accumulation drift

#### Scenario: No qualifying orders exist
- **WHEN** the selected range contains no qualifying orders for the authenticated user
- **THEN** `summary` contains zero amount, zero count, and zero average
- **AND** the page displays an explicit no-data state rather than treating a failed request as a successful zero result

### Requirement: Selected-range totals are grouped by supported order type
The system SHALL group qualifying orders by the existing order types `balance`, `usage_card`, and `subscription`, returning paid amount, order count, and average paid amount for each type.

```

Full source: openspec/changes/add-my-orders-payment-statistics/specs/payment/spec.md
