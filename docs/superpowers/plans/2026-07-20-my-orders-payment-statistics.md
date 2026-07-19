---
change: add-my-orders-payment-statistics
design-doc: docs/superpowers/specs/2026-07-20-my-orders-payment-statistics-design.md
base-ref: eece1469ac4e99ad92c519f024146f92d3f20b03
---

# 我的订单实付统计实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增独立的用户人民币订单统计页，提供自然日范围汇总、三种类型与每日聚合，以及固定每页 20 条的类型/日期只读下钻。

**Architecture:** 后端在新的 payment statistics service 文件中集中实现范围解析、共享 Ent 谓词、整数分纯聚合和下钻分页，并通过新的 handler 文件暴露两个认证 GET API。前端使用独立 view、聚合表和明细弹窗，汇总与弹窗各自以请求代次隔离竞态；现有订单页和 `/payment/orders/my` 保持不变。

**Tech Stack:** Go、Gin、Ent、modernc SQLite 测试、Vue 3 `<script setup>`、TypeScript、Axios、Vitest、Vue Test Utils、Tailwind CSS、vue-i18n。

---

## 文件结构

**新增：**

- `backend/internal/service/payment_order_statistics.go`：日期窗口、共享谓词、金额聚合、汇总和下钻查询。
- `backend/internal/service/payment_order_statistics_test.go`：纯函数、范围解析和 SQLite 查询测试。
- `backend/internal/handler/payment_statistics_handler.go`：两个认证统计 handler 与最小明细响应。
- `backend/internal/handler/payment_statistics_handler_test.go`：认证、参数、响应和用户隔离测试。
- `backend/internal/server/routes/payment_test.go`：静态统计路由注册顺序回归测试。
- `frontend/src/views/user/UserOrderStatisticsView.vue`：筛选、汇总状态和页面布局。
- `frontend/src/views/user/orderStatistics.ts`：本地日期范围与 366 天校验纯函数。
- `frontend/src/views/user/__tests__/orderStatistics.spec.ts`：日期 helper 测试。
- `frontend/src/views/user/__tests__/UserOrderStatisticsView.spec.ts`：页面状态与竞态测试。
- `frontend/src/components/payment/OrderStatisticsAggregateTable.vue`：可键盘操作的类型/每日聚合表。
- `frontend/src/components/payment/OrderStatisticsDetailsDialog.vue`：只读下钻、固定分页和独立请求状态。
- `frontend/src/components/payment/__tests__/OrderStatisticsAggregateTable.spec.ts`：行交互测试。
- `frontend/src/components/payment/__tests__/OrderStatisticsDetailsDialog.spec.ts`：弹窗分页、错误和竞态测试。
- `frontend/src/router/__tests__/order-statistics-route.spec.ts`：独立路由和 payment guard 回归测试。

**修改：**

- `backend/internal/server/routes/payment.go`：在 `/:id` 前注册两个统计静态路由。
- `backend/internal/server/middleware/server_timing_test.go`：把两个用户端统计路径加入计时范围断言。
- `frontend/src/types/payment.ts`：增加汇总、明细和参数类型。
- `frontend/src/api/payment.ts`：增加汇总与下钻请求方法。
- `frontend/src/api/__tests__/payment.spec.ts`：锁定 API 路径和参数。
- `frontend/src/router/index.ts`：增加 `/order-statistics` 路由。
- `frontend/src/components/layout/AppSidebar.vue`：在“我的订单”后追加统计入口。
- `frontend/src/components/layout/__tests__/AppSidebar.spec.ts`：锁定相邻顺序和 feature flag。
- `frontend/src/i18n/locales/zh/misc.ts`、`frontend/src/i18n/locales/en/misc.ts`：增加导航和统计页文案。
- `openspec/changes/add-my-orders-payment-statistics/tasks.md`：每完成一个映射任务立即勾选。

**明确不修改：**

- `frontend/src/views/user/UserOrdersView.vue`
- `frontend/src/components/payment/OrderTable.vue`
- `PaymentService.GetUserOrders` 与 `GET /api/v1/payment/orders/my`
- `backend/ent/schema/payment_order.go` 及任何 migration

### Task 1: 日期窗口与整数分纯聚合

**Files:**
- Create: `backend/internal/service/payment_order_statistics.go`
- Create: `backend/internal/service/payment_order_statistics_test.go`
- Modify: `openspec/changes/add-my-orders-payment-statistics/tasks.md`（映射 1.1–1.4）

- [x] **Step 1: 写日期窗口失败测试**

在 `payment_order_statistics_test.go` 使用 `package service` 和 `//go:build unit`，表驱动覆盖默认 30 天、缺一端、反向、366/367 天、非法日期、显式非法时区，以及 `America/New_York` 的 23/25 小时日：

```go
func TestParseOrderStatisticsWindow(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	w, err := parseOrderStatisticsWindow(OrderStatisticsQuery{Timezone: "Asia/Shanghai"}, now)
	require.NoError(t, err)
	require.Equal(t, "2026-06-21", w.StartDate)
	require.Equal(t, "2026-07-20", w.EndDate)
	require.Equal(t, 30, inclusiveCalendarDays(w.StartLocal, w.EndLocal))
}

func TestParseOrderStatisticsWindowRejectsInvalidInput(t *testing.T) {
	_, err := parseOrderStatisticsWindow(OrderStatisticsQuery{
		StartDate: "2026-01-01", EndDate: "2027-01-02", Timezone: "Asia/Shanghai",
	}, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC))
	require.Error(t, err)
}
```

- [x] **Step 2: 运行日期测试并确认 RED**

Run: `cd backend && go test -tags=unit ./internal/service -run 'TestParseOrderStatisticsWindow' -count=1`

Expected: FAIL，提示 `OrderStatisticsQuery` 或 `parseOrderStatisticsWindow` 未定义。

- [x] **Step 3: 实现范围类型和解析器**

在新 service 文件定义稳定常量和内部窗口；显式非法时区必须报 400，结束边界必须使用 `AddDate`：

```go
const (
	orderStatisticsDateLayout  = "2006-01-02"
	orderStatisticsDefaultDays = 30
	orderStatisticsMaxDays     = 366
	OrderStatisticsDetailPageSize = 20
)

type OrderStatisticsQuery struct {
	StartDate string
	EndDate   string
	Timezone  string
}

type orderStatisticsWindow struct {
	StartDate     string
	EndDate       string
	Timezone      string
	Location      *time.Location
	StartLocal    time.Time
	EndLocal      time.Time
	StartInclusive time.Time
	EndExclusive   time.Time
}
```

`parseOrderStatisticsWindow` 先加载 location，再处理“两端都空”或“两端都有”，使用 `infraerrors.BadRequest("INVALID_ORDER_STATISTICS_RANGE", message)` 返回参数错误。

- [x] **Step 4: 写整数分和纯聚合失败测试**

构造包含 `10.10`、`20.20`、`0.30`、三个状态和三个类型的输入，断言总分 `3060`、平均分 `1020`、缺失类型补零、本地日期分组倒序、空数据形状稳定；另断言不支持类型不进入 summary：

```go
func TestAggregateOrderStatisticsUsesIntegerCents(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	got := aggregateOrderStatistics([]orderStatisticsRow{
		{ID: 1, PayAmount: 10.10, OrderType: payment.OrderTypeBalance, PaidAt: time.Date(2026, 7, 19, 16, 30, 0, 0, time.UTC)},
		{ID: 2, PayAmount: 20.20, OrderType: payment.OrderTypeUsageCard, PaidAt: time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC)},
		{ID: 3, PayAmount: 0.30, OrderType: payment.OrderTypeUsageCard, PaidAt: time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC)},
	}, loc)
	require.Equal(t, 30.60, got.Summary.TotalPaidAmount)
	require.Equal(t, 3, got.Summary.OrderCount)
	require.Equal(t, 10.20, got.Summary.AveragePaidAmount)
	require.Len(t, got.ByType, 3)
	require.Equal(t, "2026-07-20", got.Daily[0].Date)
}
```

- [x] **Step 5: 运行聚合测试并确认 RED**

Run: `cd backend && go test -tags=unit ./internal/service -run 'TestAggregateOrderStatistics' -count=1`

Expected: FAIL，提示聚合类型或函数未定义。

- [x] **Step 6: 实现响应类型、分转换和纯聚合**

定义 `OrderStatisticsResponse`（固定 `Currency: "CNY"`）、`OrderStatisticsSummary`、`OrderTypeStatistics`、`DailyOrderStatistics`。每行先执行 `int64(math.Round(amount * 100))`，只累加整数分；输出时使用：

```go
func centsToAmount(cents int64) float64 { return float64(cents) / 100 }

func averageCents(total int64, count int) int64 {
	if count == 0 { return 0 }
	return int64(math.Round(float64(total) / float64(count)))
}
```

类型顺序使用固定 slice `balance, usage_card, subscription`，每日 key 使用 `PaidAt.In(loc).Format(orderStatisticsDateLayout)` 并降序排序。

- [x] **Step 7: 运行测试、勾选映射任务并提交**

Run: `cd backend && go test -tags=unit ./internal/service -run 'Test(ParseOrderStatisticsWindow|AggregateOrderStatistics)' -count=1`

Expected: PASS。

在 OpenSpec 勾选 `1.1`、`1.2`、`1.3`、`1.4`，然后：

```bash
git add backend/internal/service/payment_order_statistics.go backend/internal/service/payment_order_statistics_test.go openspec/changes/add-my-orders-payment-statistics/tasks.md
git commit -m "feat(payment): define personal order statistics aggregation"
```

### Task 2: 有界汇总查询与数据库分页下钻

**Files:**
- Modify: `backend/internal/service/payment_order_statistics.go`
- Modify: `backend/internal/service/payment_order_statistics_test.go`
- Modify: `openspec/changes/add-my-orders-payment-statistics/tasks.md`（映射 1.5、2.3、2.5）

- [x] **Step 1: 建立 SQLite fixture 并写查询失败测试**

使用 `enttest.NewClient`、唯一的内存 DSN 和两个用户，写入：三个成功状态、无 `paid_at`、退款状态、第四类型、范围外订单和同一 `paid_at` 的多行。断言汇总只含当前用户和合法行：

```go
stats, err := svc.GetUserOrderStatistics(ctx, user.ID, OrderStatisticsQuery{
	StartDate: "2026-07-01", EndDate: "2026-07-20", Timezone: "Asia/Shanghai",
})
require.NoError(t, err)
require.Equal(t, "CNY", stats.Currency)
require.Equal(t, 3, stats.Summary.OrderCount)
```

为 details 写表驱动测试：类型条件、日期条件、两者都有、两者都无、非法类型、范围外日期、第二页和相同时间按 ID 倒序。

- [x] **Step 2: 运行 service 查询测试并确认 RED**

Run: `cd backend && go test -tags=unit ./internal/service -run 'TestPaymentServiceGetUserOrderStatistics' -count=1`

Expected: FAIL，提示两个 service 方法或 details query 类型未定义。

- [x] **Step 3: 实现共享谓词与汇总查询**

定义共享基础谓词，两个端点必须调用同一 helper：

```go
func paidOrderStatisticsPredicates(userID int64, w orderStatisticsWindow) []predicate.PaymentOrder {
	return []predicate.PaymentOrder{
		paymentorder.UserIDEQ(userID),
		paymentorder.StatusIn(OrderStatusPaid, OrderStatusRecharging, OrderStatusCompleted),
		paymentorder.OrderTypeIn(payment.OrderTypeBalance, payment.OrderTypeUsageCard, payment.OrderTypeSubscription),
		paymentorder.PaidAtNotNil(),
		paymentorder.PaidAtGTE(w.StartInclusive),
		paymentorder.PaidAtLT(w.EndExclusive),
	}
}
```

`GetUserOrderStatistics` 使用 `Select(FieldID, FieldPayAmount, FieldOrderType, FieldPaidAt)`，把结果转换成纯聚合输入，不读取 provider snapshot、退款字段或关联。

- [x] **Step 4: 实现 details 校验、Count 和固定分页**

定义互斥 selector 和最小 DTO：

```go
type OrderStatisticsDetailsQuery struct {
	OrderStatisticsQuery
	Page      int
	OrderType string
	Date      string
}

type OrderStatisticsDetail struct {
	OutTradeNo string    `json:"out_trade_no"`
	OrderType  string    `json:"order_type"`
	PayAmount  float64   `json:"pay_amount"`
	Status     string    `json:"status"`
	PaymentType string   `json:"payment_type"`
	PaidAt     time.Time `json:"paid_at"`
}
```

`GetUserOrderStatisticsDetails` 严格校验 page > 0 和 selector 恰好一个；日期 selector 必须位于 window 内，再生成该日子窗口。先 `Clone().Count(ctx)`，再按 `dbent.Desc(paymentorder.FieldPaidAt), dbent.Desc(paymentorder.FieldID)`、`Limit(20)`、`Offset((page-1)*20)` 查询。

- [x] **Step 5: 运行查询和一致性测试**

Run: `cd backend && go test -tags=unit ./internal/service -run 'TestPaymentServiceGetUserOrderStatistics' -count=1`

Expected: PASS，包括每个 type/day 聚合 count 与对应 details total 相等。

- [x] **Step 6: 勾选映射任务并提交**

勾选 OpenSpec `1.5`、`2.3`、`2.5`：

```bash
git add backend/internal/service/payment_order_statistics.go backend/internal/service/payment_order_statistics_test.go openspec/changes/add-my-orders-payment-statistics/tasks.md
git commit -m "feat(payment): query personal statistics and drilldowns"
```

### Task 3: 认证 handler、最小响应与静态路由

**Files:**
- Create: `backend/internal/handler/payment_statistics_handler.go`
- Create: `backend/internal/handler/payment_statistics_handler_test.go`
- Create: `backend/internal/server/routes/payment_test.go`
- Modify: `backend/internal/server/routes/payment.go`
- Modify: `backend/internal/server/middleware/server_timing_test.go`
- Modify: `openspec/changes/add-my-orders-payment-statistics/tasks.md`（映射 2.1、2.2、2.4）

- [x] **Step 1: 写 handler RED 测试**

复用 Task 2 的 SQLite fixture 构造真实 `PaymentService`。分别直接调用 handler，覆盖无 AuthSubject 返回 401、认证用户不能通过 query 覆盖 user ID、有效汇总、非法时区 400、details selector 错误 400 和分页 envelope：

```go
ctx.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: user.ID})
ctx.Request = httptest.NewRequest(http.MethodGet,
	"/api/v1/payment/orders/statistics?start_date=2026-07-01&end_date=2026-07-20&timezone=Asia%2FShanghai", nil)
handler.GetOrderStatistics(ctx)
require.Equal(t, http.StatusOK, recorder.Code)
```

解析 JSON 后明确断言 details item 只有六个字段，不出现 `id`、`user_id`、`refund_amount` 或 provider snapshot。

- [x] **Step 2: 运行 handler 测试并确认 RED**

Run: `cd backend && go test -tags=unit ./internal/handler -run 'TestPaymentStatistics' -count=1`

Expected: FAIL，提示 handler 方法未定义。

- [x] **Step 3: 实现两个 handler**

两个方法先调用现有 `requireAuth`。汇总把三个范围 query 传给 service 并 `response.Success`；details 严格解析正整数 page，调用 service 后执行：

```go
response.Paginated(c, items, int64(total), page, service.OrderStatisticsDetailPageSize)
```

不要调用会接受客户端 `page_size` 的 `response.ParsePagination`；handler 只解析正整数 `page`，并始终使用 service 导出的固定页大小常量。

- [x] **Step 4: 写路由顺序 RED 测试并注册路由**

`payment_test.go` 读取同目录 `payment.go`，断言两个静态注册文本存在且索引早于 `orders.GET("/:id"`：

```go
source, err := os.ReadFile("payment.go")
require.NoError(t, err)
dynamic := strings.Index(string(source), `orders.GET("/:id"`)
require.NotEqual(t, -1, dynamic)
summary := strings.Index(string(source), `orders.GET("/statistics"`)
details := strings.Index(string(source), `orders.GET("/statistics/details"`)
require.NotEqual(t, -1, summary)
require.NotEqual(t, -1, details)
require.Less(t, summary, dynamic)
require.Less(t, details, dynamic)
```

在路由文件中按 `/statistics`、`/statistics/details`、`/my`、`/:id` 顺序注册。将两个路径加入 `TestIsUserTimingPath` 期望 `true` 的表。

- [x] **Step 5: 运行后端相关测试**

Run: `cd backend && go test -tags=unit ./internal/handler ./internal/server/routes ./internal/server/middleware -run 'Test(PaymentStatistics|PaymentStatisticsRoutes|IsUserTimingPath)' -count=1`

Expected: PASS。

执行记录（2026-07-20）：`internal/handler` 与 `internal/server/routes` 的统计定向测试 PASS。`internal/server/middleware` 包测试在编译既有 `api_key_auth_test.go` 时失败，原因是该文件仍按三参数调用已改为四参数的 `NewAPIKeyAuthMiddleware`；本变更未修改该测试。新增路径仍由现有 `/payment/` 前缀逻辑覆盖，并已保留在 `TestIsUserTimingPath` 的表中。

- [x] **Step 6: 勾选映射任务并提交**

勾选 OpenSpec `2.1`、`2.2`、`2.4`：

```bash
git add backend/internal/handler/payment_statistics_handler.go backend/internal/handler/payment_statistics_handler_test.go backend/internal/server/routes/payment.go backend/internal/server/routes/payment_test.go backend/internal/server/middleware/server_timing_test.go openspec/changes/add-my-orders-payment-statistics/tasks.md
git commit -m "feat(payment): expose personal order statistics APIs"
```

### Task 4: 前端类型、API 和本地日期 helper

**Files:**
- Modify: `frontend/src/types/payment.ts`
- Modify: `frontend/src/api/payment.ts`
- Modify: `frontend/src/api/__tests__/payment.spec.ts`
- Create: `frontend/src/views/user/orderStatistics.ts`
- Create: `frontend/src/views/user/__tests__/orderStatistics.spec.ts`
- Modify: `openspec/changes/add-my-orders-payment-statistics/tasks.md`（映射 3.1）

- [ ] **Step 1: 写 API 和日期 helper RED 测试**

API 测试断言汇总路径、details 的 type/date 两种参数和 `page` 原样传入。日期测试使用本地 `Date` 构造，不经过 `toISOString`：

```ts
expect(rangeForLastDays(30, new Date(2026, 6, 20, 12))).toEqual({
  startDate: '2026-06-21',
  endDate: '2026-07-20',
})
expect(validateInclusiveRange('2025-07-20', '2026-07-20')).toBeNull()
expect(validateInclusiveRange('2025-07-19', '2026-07-20')).toBe('tooLong')
```

- [ ] **Step 2: 运行前端单测并确认 RED**

Run: `pnpm --dir frontend exec vitest run src/api/__tests__/payment.spec.ts src/views/user/__tests__/orderStatistics.spec.ts`

Expected: FAIL，提示 API 方法和日期 helper 未定义。

- [ ] **Step 3: 增加严格 TypeScript 类型**

在 `types/payment.ts` 增加：

```ts
export interface OrderStatisticsMetric {
  total_paid_amount: number
  order_count: number
  average_paid_amount: number
}
export interface OrderTypeStatistics extends OrderStatisticsMetric { order_type: OrderType }
export interface DailyOrderStatistics extends OrderStatisticsMetric { date: string }
export interface OrderStatisticsResponse {
  start_date: string; end_date: string; timezone: string; currency: 'CNY'
  summary: OrderStatisticsMetric
  by_type: OrderTypeStatistics[]
  daily: DailyOrderStatistics[]
}
export interface OrderStatisticsDetail {
  out_trade_no: string; order_type: OrderType; pay_amount: number
  status: OrderStatus; payment_type: string; paid_at: string
}
```

details params 使用 union 保证 `order_type` 与 `date` 编译期互斥，再与 `{ start_date; end_date; page? }` 相交。

- [ ] **Step 4: 实现 API 和日期纯函数**

新增 `paymentAPI.getOrderStatistics(params?)` 与 `getOrderStatisticsDetails(params)`，均由共享 client 自动注入 timezone。helper 使用 `getFullYear/getMonth/getDate` 格式化，`rangeForLastDays` 使用 `setDate`，包含天数通过本地日期序号计算并限制 366。

- [ ] **Step 5: 运行测试、勾选任务并提交**

Run: `pnpm --dir frontend exec vitest run src/api/__tests__/payment.spec.ts src/views/user/__tests__/orderStatistics.spec.ts`

Expected: PASS。

勾选 OpenSpec `3.1`：

```bash
git add frontend/src/types/payment.ts frontend/src/api/payment.ts frontend/src/api/__tests__/payment.spec.ts frontend/src/views/user/orderStatistics.ts frontend/src/views/user/__tests__/orderStatistics.spec.ts openspec/changes/add-my-orders-payment-statistics/tasks.md
git commit -m "feat(payment): add order statistics client contract"
```

### Task 5: 可操作聚合表与只读下钻弹窗

**Files:**
- Create: `frontend/src/components/payment/OrderStatisticsAggregateTable.vue`
- Create: `frontend/src/components/payment/OrderStatisticsDetailsDialog.vue`
- Create: `frontend/src/components/payment/__tests__/OrderStatisticsAggregateTable.spec.ts`
- Create: `frontend/src/components/payment/__tests__/OrderStatisticsDetailsDialog.spec.ts`
- Modify: `openspec/changes/add-my-orders-payment-statistics/tasks.md`（映射 4.1–4.4）

- [ ] **Step 1: 写聚合表交互 RED 测试**

分别传 type 和 daily rows，断言点击、Enter、Space 均只发出一次 `select`，行有 `tabindex="0"`，类型顺序和标签为“余额 / 余额卡 / 订阅”：

```ts
await wrapper.get('[data-test="statistics-row-balance"]').trigger('keydown', { key: 'Enter' })
expect(wrapper.emitted('select')?.[0]?.[0]).toMatchObject({ kind: 'type', orderType: 'balance' })
```

- [ ] **Step 2: 写弹窗 RED 测试**

mock `getOrderStatisticsDetails`，覆盖打开即加载、固定 `page: 1`、翻页、关闭重开重置、类型/日期标题、六列、加载、空、错误重试。用两个 deferred Promise 验证关闭或选择改变后旧响应不覆盖新列表。

- [ ] **Step 3: 运行组件测试并确认 RED**

Run: `pnpm --dir frontend exec vitest run src/components/payment/__tests__/OrderStatisticsAggregateTable.spec.ts src/components/payment/__tests__/OrderStatisticsDetailsDialog.spec.ts`

Expected: FAIL，提示两个组件不存在。

- [ ] **Step 4: 实现聚合表**

组件接收 `kind`, `rows`, `currency`，类型/每日使用各自列头；row 绑定 click 与：

```vue
@keydown.enter.prevent="emitSelection(row)"
@keydown.space.prevent="emitSelection(row)"
```

金额统一调用现有 `formatPaymentAmount(value, 'CNY', locale.value)`。表格外层固定 overflow 容器，行高至少 44px，并提供可见 `focus-visible` ring。

- [ ] **Step 5: 实现明细弹窗和独立请求代次**

弹窗 props 包含 `show`, `selection`, `startDate`, `endDate`。内部 `requestGeneration` 每次打开、切换、翻页、重试和关闭时递增；响应写入前比较本地 generation。复用 `BaseDialog width="extra-wide"`、`DataTable`、`OrderStatusBadge` 和 `Pagination :show-page-size-selector="false"`，不提供 action 列。

- [ ] **Step 6: 运行组件测试、勾选任务并提交**

Run: `pnpm --dir frontend exec vitest run src/components/payment/__tests__/OrderStatisticsAggregateTable.spec.ts src/components/payment/__tests__/OrderStatisticsDetailsDialog.spec.ts`

Expected: PASS。

勾选 OpenSpec `4.1`、`4.2`、`4.3`、`4.4`：

```bash
git add frontend/src/components/payment/OrderStatisticsAggregateTable.vue frontend/src/components/payment/OrderStatisticsDetailsDialog.vue frontend/src/components/payment/__tests__/OrderStatisticsAggregateTable.spec.ts frontend/src/components/payment/__tests__/OrderStatisticsDetailsDialog.spec.ts openspec/changes/add-my-orders-payment-statistics/tasks.md
git commit -m "feat(payment): add statistics drilldown components"
```

### Task 6: 独立统计页筛选状态机

**Files:**
- Create: `frontend/src/views/user/UserOrderStatisticsView.vue`
- Create: `frontend/src/views/user/__tests__/UserOrderStatisticsView.spec.ts`
- Modify: `openspec/changes/add-my-orders-payment-statistics/tasks.md`（映射 3.2、3.3）

- [ ] **Step 1: 写页面 RED 测试**

mock AppLayout、子组件和 payment API，覆盖：默认最近 30 天；7/30/90 快捷立即查询；编辑 draft 不查询；自定义成功才提交 applied；失败保留旧 applied 与旧数据；首次错误重试；空数据；刷新；点击 type/day 打开正确 selection。

用 deferred Promise 验证较旧汇总响应在快捷切换后被忽略：

```ts
const first = deferred<OrderStatisticsResponse>()
const second = deferred<OrderStatisticsResponse>()
getOrderStatistics.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
await wrapper.get('[data-test="range-7"]').trigger('click')
second.resolve(responseFor('2026-07-14'))
await flushPromises()
first.resolve(responseFor('2026-06-21'))
await flushPromises()
expect(wrapper.text()).toContain('2026-07-14')
```

- [ ] **Step 2: 运行页面测试并确认 RED**

Run: `pnpm --dir frontend exec vitest run src/views/user/__tests__/UserOrderStatisticsView.spec.ts`

Expected: FAIL，提示 view 不存在。

- [ ] **Step 3: 实现页面状态和筛选**

使用 `draftRange`、`appliedRange`、`candidateRange` 和 `summaryGeneration`。默认 30 天 onMounted 查询；快捷范围立即替换 applied 并查询；自定义查询成功后才写 applied。初次失败显示整页 retry，已有成功数据后的自定义失败保留数据并显示非阻断错误。

- [ ] **Step 4: 实现页面布局和下钻连接**

`AppLayout` 内依次放：标题/刷新、7/30/90 segmented controls、自定义两个 `type=date` 输入和查询按钮、三个稳定尺寸指标卡、类型表、每日表、一个 details dialog。页面只把当前 `appliedRange` 交给弹窗；不导入或渲染 `OrderTable`。

- [ ] **Step 5: 运行页面测试、勾选任务并提交**

Run: `pnpm --dir frontend exec vitest run src/views/user/__tests__/UserOrderStatisticsView.spec.ts`

Expected: PASS。

勾选 OpenSpec `3.2`、`3.3`：

```bash
git add frontend/src/views/user/UserOrderStatisticsView.vue frontend/src/views/user/__tests__/UserOrderStatisticsView.spec.ts openspec/changes/add-my-orders-payment-statistics/tasks.md
git commit -m "feat(payment): build personal order statistics page"
```

### Task 7: 路由、侧栏、i18n 与旧页面零修改回归

**Files:**
- Modify: `frontend/src/router/index.ts`
- Create: `frontend/src/router/__tests__/order-statistics-route.spec.ts`
- Modify: `frontend/src/components/layout/AppSidebar.vue`
- Modify: `frontend/src/components/layout/__tests__/AppSidebar.spec.ts`
- Modify: `frontend/src/i18n/locales/zh/misc.ts`
- Modify: `frontend/src/i18n/locales/en/misc.ts`
- Modify: `openspec/changes/add-my-orders-payment-statistics/tasks.md`（映射 3.4、5.1）

- [ ] **Step 1: 写路由、侧栏和文案 RED 测试**

路由测试捕获传给 `createRouter` 的 routes，断言 `/order-statistics` 使用 lazy view、`requiresAuth: true`、`requiresAdmin: false`、`requiresPayment: true`。侧栏源码测试断言新 path 位于 `/orders` 后且同样使用 `flagPayment`。i18n 编译测试沿用现有全量 locale suite。

- [ ] **Step 2: 运行导航测试并确认 RED**

Run: `pnpm --dir frontend exec vitest run src/router/__tests__/order-statistics-route.spec.ts src/components/layout/__tests__/AppSidebar.spec.ts src/i18n/__tests__/localesMessageCompile.spec.ts`

Expected: FAIL，提示新路由、菜单项或 locale key 缺失。

- [ ] **Step 3: 添加路由、菜单和中英文文案**

路由紧邻 `/orders`，名称 `OrderStatistics`，标题 key 使用 `nav.orderStatistics`。侧栏复用 `ChartIcon`：

```ts
{ path: '/order-statistics', label: t('nav.orderStatistics'), icon: ChartIcon, hideInSimpleMode: true, featureFlag: flagPayment }
```

在 `payment.statistics` 下加入标题、范围、指标、类型、每日、明细六列、加载/错误/空状态和重试文案；中文类型固定“余额 / 余额卡 / 订阅”。

- [ ] **Step 4: 运行测试并执行旧页面 diff 断言**

Run: `pnpm --dir frontend exec vitest run src/router/__tests__/order-statistics-route.spec.ts src/router/__tests__/feature-access.spec.ts src/components/layout/__tests__/AppSidebar.spec.ts src/i18n/__tests__/localesMessageCompile.spec.ts`

Expected: PASS。

Run: `git diff --exit-code eece1469ac4e99ad92c519f024146f92d3f20b03 -- frontend/src/views/user/UserOrdersView.vue frontend/src/components/payment/OrderTable.vue`

Expected: exit 0，无输出。

- [ ] **Step 5: 勾选任务并提交**

勾选 OpenSpec `3.4`、`5.1`：

```bash
git add frontend/src/router/index.ts frontend/src/router/__tests__/order-statistics-route.spec.ts frontend/src/components/layout/AppSidebar.vue frontend/src/components/layout/__tests__/AppSidebar.spec.ts frontend/src/i18n/locales/zh/misc.ts frontend/src/i18n/locales/en/misc.ts openspec/changes/add-my-orders-payment-statistics/tasks.md
git commit -m "feat(payment): expose order statistics navigation"
```

### Task 8: 全量验证、视觉检查与规格闭环

**Files:**
- Modify: `openspec/changes/add-my-orders-payment-statistics/tasks.md`（映射 5.2–5.5）

- [ ] **Step 1: 运行后端定向测试**

Run: `cd backend && go test -tags=unit ./internal/service ./internal/handler ./internal/server/routes ./internal/server/middleware -run 'Test(PaymentServiceGetUserOrderStatistics|ParseOrderStatisticsWindow|AggregateOrderStatistics|PaymentStatistics|PaymentStatisticsRoutes|IsUserTimingPath)' -count=1`

Expected: PASS。

- [ ] **Step 2: 运行后端 race、vet 和 build**

Run: `cd backend && go test -race -tags=unit ./internal/service ./internal/handler ./internal/server/routes ./internal/server/middleware`

Run: `cd backend && go vet ./...`

Run: `make build-backend`

Expected: 三条命令均 exit 0。

- [ ] **Step 3: 运行前端定向与全量静态验证**

Run: `pnpm --dir frontend exec vitest run src/api/__tests__/payment.spec.ts src/views/user/__tests__/orderStatistics.spec.ts src/views/user/__tests__/UserOrderStatisticsView.spec.ts src/components/payment/__tests__/OrderStatisticsAggregateTable.spec.ts src/components/payment/__tests__/OrderStatisticsDetailsDialog.spec.ts src/router/__tests__/order-statistics-route.spec.ts src/components/layout/__tests__/AppSidebar.spec.ts`

Run: `pnpm --dir frontend run lint:check`

Run: `pnpm --dir frontend run typecheck`

Run: `pnpm --dir frontend run build`

Expected: 全部 exit 0。

- [ ] **Step 4: 启动开发服务器并做视觉检查**

Run: `pnpm --dir frontend run dev -- --host 127.0.0.1`

使用可用浏览器控制工具在 1440x900 与 375x812 检查浅色/深色统计页和明细弹窗。固定检查：长订单号不覆盖相邻列、日期和人民币金额不截断、空状态不改变区段高度、弹窗表格只在自身横向滚动、按钮/文字不重叠、键盘焦点可见。保留截图路径作为验证证据，检查完停止 dev server。

- [ ] **Step 5: 运行规格与工作区检查**

Run: `openspec validate add-my-orders-payment-statistics --type change --strict --no-interactive`

Run: `git diff --check`

Run: `git diff --exit-code eece1469ac4e99ad92c519f024146f92d3f20b03 -- frontend/src/views/user/UserOrdersView.vue frontend/src/components/payment/OrderTable.vue`

Expected: 全部 exit 0，OpenSpec 输出 change valid，两个旧页面文件无 diff。

- [ ] **Step 6: 勾选验证任务并提交**

勾选 OpenSpec `5.2`、`5.3`、`5.4`、`5.5`，确认该文件所有 checkbox 已完成：

```bash
git add openspec/changes/add-my-orders-payment-statistics/tasks.md
git commit -m "test(payment): verify personal order statistics"
```

- [ ] **Step 7: 请求代码审查并处理结果**

执行方式为 `executing-plans` 时，按 Comet guard 要求加载 `superpowers:requesting-code-review` 并至少完成一次审查。CRITICAL 问题先修复并重新运行受影响测试；接受非 CRITICAL 偏差时把理由写入 `tasks.md` 注释或验证报告草稿，再进入 build guard。
