# 统一监控中心（Monitoring Center）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将「首token监控」(`/admin/ttft`) 与「账号性能」(`/admin/performance`) 合并为统一监控中心 `/admin/monitoring`，覆盖整体健康概览、定位问题账号、验证 TTFT 参数效果、分析失败原因四个场景。

**Architecture:** 后端新增聚合端点 `GET /api/v1/admin/monitoring/overview`（组合现有 AccountPerformanceService 与 FirstTokenTimeout stats repository 的只读方法），复用 performance accounts/investigation 与 TTFT settings 端点；前端在 `views/admin/monitoring/` 全新目录实现页面，迁移改造现有 ttft/performance 组件，删除旧页面。

**Tech Stack:** Go (gin, sqlmock 测试) / Vue 3 + TypeScript + Tailwind / chart.js + vue-chartjs / vue-i18n / Vitest + @vue/test-utils。

**Spec:** `docs/superpowers/specs/2026-07-20-monitoring-center-design.md`

## Global Constraints

- **冲突规避（最高优先级）**：不修改 `frontend/src/components/common/*` 任何共享组件；on-main 文件只做追加式小编辑（`router/index.ts`、`i18n/{zh,en}/admin/index.ts`、`i18n/{zh,en}/common.ts`、`backend/internal/server/routes/admin.go`、`backend/internal/handler/handler.go`、`backend/internal/handler/wire.go`、`AppSidebar.vue`）；`backend/cmd/server/wire_gen.go` 优先用 wire 重新生成，工具不可用时按既有模式手改并 `go build` 验证。
- **不改采集逻辑与指标口径**；TTFT stats 仅扩展 `1h/6h` 时间范围枚举。
- 新页面**全部文案走 i18n**（`admin.monitoring.*`），禁止硬编码中文/英文。
- 图表继续用 chart.js；色板固定：绿 `#10b981`、红 `#ef4444`、琥珀 `#f59e0b`、青 `#0ea5e9`、紫 `#8b5cf6`、橙 `#f97316`、灰 `#64748b`。实现图表组件前阅读 dataviz skill。
- 提交信息格式遵循仓库历史：`feat(monitoring): ...` / `fix(monitoring): ...` / `refactor(monitoring): ...`。
- 前端测试命令：`cd frontend && npm run test:run -- <file>`；类型检查 `cd frontend && npm run typecheck`。
- 后端测试命令：`cd backend && go test ./internal/... -run <TestName> -count=1`；构建 `cd backend && go build ./...`。
- 平台列表固定为：`anthropic`、`openai`、`gemini`、`antigravity`、`grok`。

## File Structure

**后端（新建 1 文件，追加式修改 5 处）：**
- Create: `backend/internal/handler/admin/monitoring_handler.go` — 聚合 overview handler
- Create: `backend/internal/handler/admin/monitoring_handler_test.go`
- Modify: `backend/internal/service/first_token_timeout_stats.go` — +2 个 range 常量
- Modify: `backend/internal/handler/admin/first_token_timeout_handler.go` — range 解析 + 错误消息
- Modify: `backend/internal/repository/first_token_timeout_stats_repo.go` — range→duration 映射
- Modify: `backend/internal/handler/admin/account_performance_handler.go` — search 参数
- Modify: `backend/internal/service/account_performance.go` — filter 加 Search 字段
- Modify: `backend/internal/repository/account_performance_repo.go` — search WHERE 子句
- Modify: `backend/internal/handler/handler.go`、`backend/internal/handler/wire.go`、`backend/cmd/server/wire_gen.go`、`backend/internal/server/routes/admin.go` — 注册

**前端（新建目录，删除 2 个旧目录）：**
- Create: `frontend/src/api/admin/monitoring.ts` + `frontend/src/api/__tests__/admin.monitoring.spec.ts`
- Create: `frontend/src/i18n/locales/{zh,en}/admin/monitoring.ts`；Modify 两个 `admin/index.ts`（+2 行）、两个 `common.ts`（nav 各 +1 行）
- Create: `frontend/src/views/admin/monitoring/MonitoringView.vue`
- Create: `frontend/src/views/admin/monitoring/components/{MetricTrendCard,ProtectionFunnel,MonitoringTrendChart,FailureDistribution,AccountHealthTable,TTFTSettingsDialog,InvestigationDrawer}.vue` + 各自 `__tests__/*.spec.ts`
- Modify: `frontend/src/router/index.ts`、`frontend/src/components/layout/AppSidebar.vue`
- Delete: `frontend/src/views/admin/ttft/`、`frontend/src/views/admin/performance/`、`frontend/src/api/admin/ttft.ts`、`frontend/src/api/__tests__/admin.ttft.spec.ts`、`frontend/src/i18n/locales/{zh,en}/admin/ttft.ts`

---

### Task 1: 后端 — TTFT stats 时间范围扩展 1h/6h

**Files:**
- Modify: `backend/internal/service/first_token_timeout_stats.go`（约 55-61 行常量块）
- Modify: `backend/internal/handler/admin/first_token_timeout_handler.go`（`parseFirstTokenStatsRange`，约 506-519 行；错误消息在 `parseFirstTokenStatsOverviewFilter` 约 263 行）
- Modify: `backend/internal/repository/first_token_timeout_stats_repo.go`（`normalizeFirstTokenStatsOverviewFilter`，约 513-526 行）
- Test: `backend/internal/handler/admin/first_token_timeout_handler_test.go`、`backend/internal/repository/first_token_timeout_stats_repo_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `service.FirstTokenStatsRange1Hour`、`service.FirstTokenStatsRange6Hours`（Task 3 的聚合 handler 与 parse 函数依赖这两个值可合法通过）

- [ ] **Step 1: 写失败测试（handler 层）**

在 `backend/internal/handler/admin/first_token_timeout_handler_test.go` 追加：

```go
func TestParseFirstTokenStatsRangeAcceptsShortRanges(t *testing.T) {
	for _, raw := range []string{"1h", "6h"} {
		statsRange, ok := parseFirstTokenStatsRange(raw)
		require.True(t, ok, "range %s should be accepted", raw)
		require.Equal(t, service.FirstTokenStatsRange(raw), statsRange)
	}
}
```

（文件已有 `parseFirstTokenStatsRange` 相关测试与 import，复用其 import 块即可；若无 `service` import 则添加 `"github.com/Wei-Shaw/sub2api/internal/service"`。）

- [ ] **Step 2: 写失败测试（repo 层）**

在 `backend/internal/repository/first_token_timeout_stats_repo_test.go` 追加（该文件为纯函数单测 + sqlmock 混合，此测试是纯函数）：

```go
func TestNormalizeFirstTokenStatsOverviewFilterSupportsShortRanges(t *testing.T) {
	end := time.Date(2026, 7, 20, 10, 30, 0, 0, time.UTC)
	cases := []struct {
		rangeValue service.FirstTokenStatsRange
		wantStart  time.Time
	}{
		{service.FirstTokenStatsRange1Hour, time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)},
		{service.FirstTokenStatsRange6Hours, time.Date(2026, 7, 20, 5, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		start, normalizedEnd, err := normalizeFirstTokenStatsOverviewFilter(service.FirstTokenStatsOverviewFilter{Range: tc.rangeValue, End: end})
		require.NoError(t, err)
		require.Equal(t, time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC), normalizedEnd)
		require.Equal(t, tc.wantStart, start)
	}
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `cd backend && go test ./internal/handler/admin/ -run TestParseFirstTokenStatsRangeAcceptsShortRanges -count=1 && go test ./internal/repository/ -run TestNormalizeFirstTokenStatsOverviewFilterSupportsShortRanges -count=1`
Expected: FAIL — `parseFirstTokenStatsRange` 返回 `ok=false`；repo 报 `unsupported first token stats range "1h"`

- [ ] **Step 4: 实现 — service 常量**

`backend/internal/service/first_token_timeout_stats.go`，在 `FirstTokenStatsRange24Hours` 前插入：

```go
	FirstTokenStatsRange1Hour  FirstTokenStatsRange = "1h"
	FirstTokenStatsRange6Hours FirstTokenStatsRange = "6h"
```

- [ ] **Step 5: 实现 — handler 解析与错误消息**

`first_token_timeout_handler.go` 的 `parseFirstTokenStatsRange`，在 `case "", service.FirstTokenStatsRange24Hours:` 前插入：

```go
	case service.FirstTokenStatsRange1Hour:
		return service.FirstTokenStatsRange1Hour, true
	case service.FirstTokenStatsRange6Hours:
		return service.FirstTokenStatsRange6Hours, true
```

同文件 `parseFirstTokenStatsOverviewFilter` 中错误消息改为：

```go
		response.BadRequest(c, "range must be one of 1h, 6h, 24h, 7d, 30d, or 90d")
```

注意：同文件内如有其它引用旧错误文案的测试，同步更新。

- [ ] **Step 6: 实现 — repo duration 映射**

`first_token_timeout_stats_repo.go` 的 `normalizeFirstTokenStatsOverviewFilter` switch，在 `case service.FirstTokenStatsRange24Hours:` 前插入：

```go
	case service.FirstTokenStatsRange1Hour:
		duration = time.Hour
	case service.FirstTokenStatsRange6Hours:
		duration = 6 * time.Hour
```

- [ ] **Step 7: 运行测试确认通过**

Run: `cd backend && go test ./internal/handler/admin/ -run TestParseFirstTokenStatsRange -count=1 && go test ./internal/repository/ -run TestNormalizeFirstTokenStatsOverviewFilter -count=1 && go build ./...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add backend/internal/service/first_token_timeout_stats.go backend/internal/handler/admin/first_token_timeout_handler.go backend/internal/repository/first_token_timeout_stats_repo.go backend/internal/handler/admin/first_token_timeout_handler_test.go backend/internal/repository/first_token_timeout_stats_repo_test.go
git commit -m "feat(monitoring): support 1h and 6h ranges for first token stats"
```

---

### Task 2: 后端 — performance accounts 增加 search 参数

**Files:**
- Modify: `backend/internal/handler/admin/account_performance_handler.go`（`accountPerformanceFilter`  struct 约 94 行、`parseAccountPerformanceFilter` 约 107 行、`GetAccounts` 约 39 行）
- Modify: `backend/internal/service/account_performance.go`（`AccountPerformanceAccountFilter` 约 139 行）
- Modify: `backend/internal/repository/account_performance_repo.go`（`QueryAccounts` 约 396 行）
- Test: `backend/internal/handler/admin/account_performance_handler_test.go`、`backend/internal/repository/account_performance_repo_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `AccountPerformanceAccountFilter.Search string`；`GET /admin/performance/accounts` 接受 `search` query（按账号名模糊匹配，含 `#<id>` 回退名），Task 12 的前端账号表依赖它

- [ ] **Step 1: 写失败测试（handler 层）**

在 `account_performance_handler_test.go` 追加：

```go
func TestParseAccountPerformanceFilterAcceptsSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?range=24h&search=prod", nil)

	filter, ok := parseAccountPerformanceFilter(c)

	require.True(t, ok)
	require.Equal(t, "prod", filter.search)
}

func TestParseAccountPerformanceFilterRejectsLongSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?search="+strings.Repeat("a", 256), nil)

	_, ok := parseAccountPerformanceFilter(c)

	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
```

（若文件缺少 `strings` import 则添加。）

- [ ] **Step 2: 写失败测试（repo 层）**

在 `account_performance_repo_test.go` 追加（参照该文件现有 sqlmock 测试的 mock 方式；`beginReadSnapshot` 需要 `mock.ExpectBegin()`）：

```go
func TestQueryAccountsAppliesSearchFilter(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewAccountPerformanceRepository(db)
	mock.ExpectBegin()
	mock.ExpectQuery("FROM enriched WHERE \\(\\$20::text = '' OR account_name ILIKE \\$20").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), int64(20), int64(0), "%prod%").
		WillReturnRows(sqlmock.NewRows([]string{"account_id"}))
	mock.ExpectRollback()

	_, err = repo.QueryAccounts(context.Background(), service.AccountPerformanceAccountFilter{
		Start: time.Now().Add(-time.Hour), End: time.Now(), Page: 1, PageSize: 20, Search: "prod", SortBy: "health_score", SortOrder: "asc",
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
```

> 说明：该测试的精确 args 数量取决于 `normalizeAccountPerformanceFilter` 产出的参数个数。若现有 `QueryAccounts` 测试已有更简洁的 mock 写法，优先模仿它；核心断言是 SQL 含 `account_name ILIKE` 且最后一个参数为 `"%prod%"`。

- [ ] **Step 3: 运行测试确认失败**

Run: `cd backend && go test ./internal/handler/admin/ -run TestParseAccountPerformanceFilter -count=1 && go test ./internal/repository/ -run TestQueryAccountsAppliesSearchFilter -count=1`
Expected: FAIL — search 不在 allowed map 中（400）；`AccountPerformanceAccountFilter` 无 `Search` 字段（编译错误）

- [ ] **Step 4: 实现 — handler**

`account_performance_handler.go`：

1. `accountPerformanceFilter` struct 增加字段 `search string`（加在 `accountID int64` 后）。
2. `parseAccountPerformanceFilter` 的 allowed map 增加 `"search": true`。
3. 同函数中 `filter := accountPerformanceFilter{...}` 字面量加入 `search: strings.TrimSpace(c.Query("search"))`，并在其后追加长度校验（文件需 import `"unicode/utf8"`）：

```go
	if utf8.RuneCountInString(filter.search) > 255 {
		response.BadRequest(c, "search exceeds 255 characters")
		return accountPerformanceFilter{}, false
	}
```

4. `GetAccounts` 中 `service.AccountPerformanceAccountFilter{...}` 字面量加入 `Search: filter.search`。

- [ ] **Step 5: 实现 — service filter 字段**

`backend/internal/service/account_performance.go` 的 `AccountPerformanceAccountFilter` struct，在 `AccountID int64` 后加：

```go
	Search    string
```

- [ ] **Step 6: 实现 — repo SQL**

`account_performance_repo.go` 的 `QueryAccounts`：

1. 在 `query := ...` 之前加：

```go
	search := ""
	if filter.Search != "" {
		search = "%" + escapeAccountPerformanceLike(filter.Search) + "%"
	}
```

2. SQL 字符串中 `FROM enriched` 与 `ORDER BY` 之间插入一行（注意保持字符串拼接形式与现有一致）：

```sql
WHERE ($20::text = '' OR account_name ILIKE $20 ESCAPE '\')
```

3. `queryArgs := append(args, int64(pageSize), offset)` 改为：

```go
	queryArgs := append(args, int64(pageSize), offset, search)
```

4. 文件末尾（或 `escapeFirstTokenStatsLike` 同款位置风格）新增：

```go
func escapeAccountPerformanceLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
```

- [ ] **Step 7: 运行测试确认通过**

Run: `cd backend && go test ./internal/handler/admin/ -run TestParseAccountPerformanceFilter -count=1 && go test ./internal/repository/ -run TestQueryAccounts -count=1 && go build ./...`
Expected: PASS（包括该文件既有的全部 QueryAccounts 测试）

- [ ] **Step 8: Commit**

```bash
git add backend/internal/handler/admin/account_performance_handler.go backend/internal/service/account_performance.go backend/internal/repository/account_performance_repo.go backend/internal/handler/admin/account_performance_handler_test.go backend/internal/repository/account_performance_repo_test.go
git commit -m "feat(monitoring): add search filter to account performance accounts"
```

---

### Task 3: 后端 — monitoring 聚合 overview 端点

**Files:**
- Create: `backend/internal/handler/admin/monitoring_handler.go`
- Create: `backend/internal/handler/admin/monitoring_handler_test.go`
- Modify: `backend/internal/handler/handler.go`（`AdminHandlers` struct，`AccountPerformance` 字段后 +1 行）
- Modify: `backend/internal/handler/wire.go`（`ProvideAdminHandlers` 参数列表 + 结构体字面量各 +1 行）
- Modify: `backend/cmd/server/wire_gen.go`（构造 + 传参，见 Step 5）
- Modify: `backend/internal/server/routes/admin.go`（`registerAccountPerformanceRoutes` 附近）

**Interfaces:**
- Consumes: `service.AccountPerformanceService.Overview(ctx, AccountPerformanceOverviewFilter)`；`service.FirstTokenTimeoutStatsRepository.QueryOverview(ctx, FirstTokenStatsOverviewFilter)`；`FirstTokenTimeoutStatsRecorder.Health()`；Task 1 的短 range 支持；同包的 `parseAccountPerformanceFilter`、`parseFirstTokenStatsRange`、`firstTokenStatsOverviewResponseFromService`、`writeAccountPerformanceError`
- Produces: `GET /api/v1/admin/monitoring/overview?range&platform&model`，响应：

```json
{
  "performance": { "...": "现有 AccountPerformanceOverviewResult JSON（含 summary/trend/collection_health/coverage_start/coverage_end）" },
  "ttft": { "summary": {}, "trend": [], "other_failures": [], "completeness": {} }
}
```

前端 Task 4 的 `MonitoringOverview` 类型依赖此结构。

- [ ] **Step 1: 写失败测试**

创建 `backend/internal/handler/admin/monitoring_handler_test.go`：

```go
package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMonitoringGetOverviewUnavailableWithoutDependencies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?range=24h", nil)

	handler := &MonitoringHandler{}
	handler.GetOverview(c)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestMonitoringGetOverviewRejectsInvalidRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?range=15m", nil)

	handler := &MonitoringHandler{}
	handler.GetOverview(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMonitoringGetOverviewRejectsUnknownQueryKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?range=24h&bogus=1", nil)

	handler := &MonitoringHandler{}
	handler.GetOverview(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd backend && go test ./internal/handler/admin/ -run TestMonitoring -count=1`
Expected: FAIL — `MonitoringHandler` undefined（编译错误）

- [ ] **Step 3: 实现 handler**

创建 `backend/internal/handler/admin/monitoring_handler.go`：

```go
package admin

import (
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// MonitoringHandler serves the unified monitoring center by composing the
// account performance and first-token-timeout read models into one response.
type MonitoringHandler struct {
	performance  *service.AccountPerformanceService
	ttftRepo     service.FirstTokenTimeoutStatsRepository
	ttftRecorder *service.FirstTokenTimeoutStatsRecorder
}

func NewMonitoringHandler(performance *service.AccountPerformanceService, ttftRepo service.FirstTokenTimeoutStatsRepository, ttftRecorder *service.FirstTokenTimeoutStatsRecorder) *MonitoringHandler {
	return &MonitoringHandler{performance: performance, ttftRepo: ttftRepo, ttftRecorder: ttftRecorder}
}

type monitoringOverviewResponse struct {
	Performance *service.AccountPerformanceOverviewResult `json:"performance"`
	TTFT        firstTokenStatsOverviewResponse           `json:"ttft"`
}

func (h *MonitoringHandler) GetOverview(c *gin.Context) {
	filter, ok := parseAccountPerformanceFilter(c)
	if !ok {
		return
	}
	statsRange, ok := parseFirstTokenStatsRange(c.Query("range"))
	if !ok {
		response.BadRequest(c, "range must be one of 1h, 6h, 24h, 7d, 30d, or 90d")
		return
	}
	if h == nil || h.performance == nil || h.ttftRepo == nil {
		response.Error(c, http.StatusServiceUnavailable, "Monitoring overview is unavailable")
		return
	}
	performance, err := h.performance.Overview(c.Request.Context(), filter.overview())
	if err != nil {
		writeAccountPerformanceError(c, err)
		return
	}
	ttft, err := h.ttftRepo.QueryOverview(c.Request.Context(), service.FirstTokenStatsOverviewFilter{
		Range:    statsRange,
		End:      time.Now().UTC(),
		Protocol: filter.protocol,
		Model:    filter.model,
	})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to load first token timeout overview")
		return
	}
	var health service.FirstTokenTimeoutStatsRecorderHealth
	if h.ttftRecorder != nil {
		health = h.ttftRecorder.Health()
	}
	response.Success(c, monitoringOverviewResponse{
		Performance: performance,
		TTFT:        firstTokenStatsOverviewResponseFromService(ttft, health),
	})
}
```

注意：测试里 `&MonitoringHandler{}` 请求合法 range 时会先通过解析再命中 nil 检查 → 503；`range=15m` 时先被 `parseAccountPerformanceFilter` 拒绝 → 400。行为与测试一致。

- [ ] **Step 4: 注册到 AdminHandlers 与 wire**

1. `backend/internal/handler/handler.go` `AdminHandlers` struct，在 `AccountPerformance     *admin.AccountPerformanceHandler` 后加：

```go
	Monitoring             *admin.MonitoringHandler
```

2. `backend/internal/handler/wire.go` `ProvideAdminHandlers`：参数列表在 `accountPerformanceHandler *admin.AccountPerformanceHandler,` 后加 `monitoringHandler *admin.MonitoringHandler,`；返回字面量在 `AccountPerformance:     accountPerformanceHandler,` 后加：

```go
		Monitoring:             monitoringHandler,
```

3. `backend/internal/server/routes/admin.go`：在 `registerAccountPerformanceRoutes(admin, h)` 调用后加 `registerMonitoringRoutes(admin, h)`；在 `registerAccountPerformanceRoutes` 函数后新增：

```go
func registerMonitoringRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	monitoring := admin.Group("/monitoring")
	{
		monitoring.GET("/overview", h.Admin.Monitoring.GetOverview)
	}
}
```

- [ ] **Step 5: wire_gen.go 更新**

优先重新生成：

Run: `cd backend && go run github.com/google/wire/cmd/wire ./cmd/server`
Expected: wire_gen.go 更新且无手改

若 wire 不可用（网络限制），按既有模式手改 `backend/cmd/server/wire_gen.go`：
1. 在 `accountPerformanceHandler := admin.NewAccountPerformanceHandler(accountPerformanceService)`（约 235 行）后加：

```go
	monitoringHandler := admin.NewMonitoringHandler(accountPerformanceService, firstTokenTimeoutStatsRepository, firstTokenTimeoutStatsRecorder)
```

2. 找到调用 `handler.ProvideAdminHandlers(` 的位置，在实参列表中与 wire.go 形参 `monitoringHandler` 相同的位置插入 `monitoringHandler,`。

- [ ] **Step 6: 运行测试与构建**

Run: `cd backend && go test ./internal/handler/admin/ -run TestMonitoring -count=1 && go build ./... && go test ./cmd/server/ -count=1`
Expected: PASS（含 wire_gen_test）

- [ ] **Step 7: Commit**

```bash
git add backend/internal/handler/admin/monitoring_handler.go backend/internal/handler/admin/monitoring_handler_test.go backend/internal/handler/handler.go backend/internal/handler/wire.go backend/cmd/server/wire_gen.go backend/internal/server/routes/admin.go
git commit -m "feat(monitoring): add aggregated monitoring overview endpoint"
```

---

### Task 4: 前端 — monitoring API client

**Files:**
- Create: `frontend/src/api/admin/monitoring.ts`
- Test: `frontend/src/api/__tests__/admin.monitoring.spec.ts`
- 参照: `frontend/src/api/__tests__/admin.ttft.spec.ts`（mock apiClient 的既有模式）

**Interfaces:**
- Consumes: `apiClient`（`../client`）；`./performance` 的类型与 helper
- Produces（Task 6-12 全部依赖）：默认导出 `{ getOverview, getAccounts, getInvestigation, getSettings, updateSettings }`；命名导出类型 `MonitoringRange`、`MonitoringOverview`、`MonitoringOverviewParams`、`MonitoringAccountsParams`、`MonitoringInvestigationParams`、`MonitoringTTFTSummary`、`FirstTokenTimeoutSettings`、`FirstTokenTimeoutSettingsValue`；命名导出 `performanceMetricsFromCounters`、`performanceMetricsFromTimePoint`（re-export）

- [ ] **Step 1: 写失败测试**

创建 `frontend/src/api/__tests__/admin.monitoring.spec.ts`（先读 `admin.ttft.spec.ts` 复用其 mock 写法，以下为内容）：

```ts
import { beforeEach, describe, expect, it, vi } from 'vitest'

const get = vi.hoisted(() => vi.fn())
const put = vi.hoisted(() => vi.fn())

vi.mock('@/api/client', () => ({ apiClient: { get, put } }))

import monitoringAPI, { getOverview, getAccounts, getSettings, updateSettings } from '../admin/monitoring'

describe('admin monitoring api', () => {
  beforeEach(() => {
    get.mockReset()
    put.mockReset()
    get.mockResolvedValue({ data: {} })
    put.mockResolvedValue({ data: {} })
  })

  it('requests the aggregated overview with default range', async () => {
    await getOverview({ platform: 'openai' })
    expect(get).toHaveBeenCalledWith('/admin/monitoring/overview', { params: { range: '24h', platform: 'openai' } })
  })

  it('requests accounts with search and pagination', async () => {
    await getAccounts({ range: '7d', search: 'prod', sort: 'health_score', order: 'asc', page: 2, page_size: 20 })
    expect(get).toHaveBeenCalledWith('/admin/performance/accounts', {
      params: { range: '7d', search: 'prod', sort: 'health_score', order: 'asc', page: 2, page_size: 20 }
    })
  })

  it('loads and saves first token timeout settings', async () => {
    await getSettings()
    expect(get).toHaveBeenCalledWith('/admin/settings/first-token-timeout')
    await updateSettings({ enabled: true, timeout_seconds: 30 })
    expect(put).toHaveBeenCalledWith('/admin/settings/first-token-timeout', { enabled: true, timeout_seconds: 30 })
  })

  it('exposes a default export with all methods', () => {
    expect(Object.keys(monitoringAPI).sort()).toEqual(['getAccounts', 'getInvestigation', 'getOverview', 'getSettings', 'updateSettings'])
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd frontend && npm run test:run -- src/api/__tests__/admin.monitoring.spec.ts`
Expected: FAIL — `../admin/monitoring` 模块不存在

- [ ] **Step 3: 实现 client**

创建 `frontend/src/api/admin/monitoring.ts`：

```ts
import { apiClient } from '../client'
import {
  performanceMetricsFromCounters,
  performanceMetricsFromTimePoint,
  type PerformanceAccountItem,
  type PerformanceAccountPage,
  type PerformanceCounters,
  type PerformanceInvestigation,
  type PerformanceMetrics,
  type PerformanceOrder,
  type PerformanceOverview,
  type PerformanceRange,
  type PerformanceTimePoint
} from './performance'

// ---- Types (mirrors of the removed ttft client, kept self-contained) ----

export type MonitoringRange = PerformanceRange
export type { PerformanceOrder, PerformanceRange }

export interface RateMetric {
  numerator: number
  denominator: number
  rate: number
}

export interface MonitoringTTFTSummary {
  controlled_requests: number
  client_canceled_requests: number
  attempt_ttft_timeout_rate: RateMetric
  recovery_rate: RateMetric
  final_ttft_failure_rate: RateMetric
  other_final_failure_rate: RateMetric
}

export interface TTFTTrendPoint {
  bucket_start: string
  attempt_ttft_timeout_rate: RateMetric
  recovery_rate: RateMetric
  final_ttft_failure_rate: RateMetric
  other_final_failure_rate: RateMetric
}

export interface TTFTFailureDistributionItem {
  failure_kind: string
  sample_count: number
}

export interface CollectionHealth {
  status: 'complete' | 'degraded' | string
  dropped_samples: number
  pending_samples: number
  last_successful_flush_at: string | null
}

export interface MonitoringTTFTOverview {
  summary: MonitoringTTFTSummary
  trend: TTFTTrendPoint[]
  other_failures: TTFTFailureDistributionItem[]
  completeness: CollectionHealth
}

export interface MonitoringOverview {
  performance: PerformanceOverview
  ttft: MonitoringTTFTOverview
}

export interface FirstTokenTimeoutSettingsValue {
  enabled: boolean
  timeout_seconds: number
}

export interface FirstTokenTimeoutSettings {
  saved: FirstTokenTimeoutSettingsValue
  effective: FirstTokenTimeoutSettingsValue
  loaded_at: string
}

export interface MonitoringOverviewParams {
  range?: MonitoringRange
  platform?: string
  model?: string
}

export interface MonitoringAccountsParams extends MonitoringOverviewParams {
  search?: string
  sort?: string
  order?: PerformanceOrder
  page?: number
  page_size?: number
}

export interface MonitoringInvestigationParams extends MonitoringOverviewParams {
  account_id: number
}

function withDefaultRange<T extends { range?: MonitoringRange }>(params: T) {
  return { ...params, range: params.range ?? '24h' }
}

// ---- API ----

export async function getOverview(params: MonitoringOverviewParams): Promise<MonitoringOverview> {
  const { data } = await apiClient.get<MonitoringOverview>('/admin/monitoring/overview', { params: withDefaultRange(params) })
  return data
}

export async function getAccounts(params: MonitoringAccountsParams): Promise<PerformanceAccountPage> {
  const { data } = await apiClient.get<PerformanceAccountPage>('/admin/performance/accounts', { params: withDefaultRange(params) })
  return data
}

export async function getInvestigation(params: MonitoringInvestigationParams): Promise<PerformanceInvestigation> {
  const { data } = await apiClient.get<PerformanceInvestigation>('/admin/performance/investigation', { params: withDefaultRange(params) })
  return data
}

export async function getSettings(): Promise<FirstTokenTimeoutSettings> {
  const { data } = await apiClient.get<FirstTokenTimeoutSettings>('/admin/settings/first-token-timeout')
  return data
}

export async function updateSettings(payload: FirstTokenTimeoutSettingsValue): Promise<FirstTokenTimeoutSettings> {
  const { data } = await apiClient.put<FirstTokenTimeoutSettings>('/admin/settings/first-token-timeout', payload)
  return data
}

export { performanceMetricsFromCounters, performanceMetricsFromTimePoint }
export type { PerformanceAccountItem, PerformanceAccountPage, PerformanceCounters, PerformanceInvestigation, PerformanceMetrics, PerformanceOverview, PerformanceTimePoint }

export default { getOverview, getAccounts, getInvestigation, getSettings, updateSettings }
```

> 若 `./performance` 未导出上述某个类型/helper，先在该文件对应声明上加 `export`（属 branch-only 文件，可改）。settings 路径以 `api/admin/ttft.ts` 现有值 `/admin/settings/first-token-timeout` 为准。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd frontend && npm run test:run -- src/api/__tests__/admin.monitoring.spec.ts && npm run typecheck`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api/admin/monitoring.ts frontend/src/api/__tests__/admin.monitoring.spec.ts
git commit -m "feat(monitoring): add monitoring api client"
```

---

### Task 5: 前端 — i18n locale 文件与注册

**Files:**
- Create: `frontend/src/i18n/locales/zh/admin/monitoring.ts`
- Create: `frontend/src/i18n/locales/en/admin/monitoring.ts`
- Modify: `frontend/src/i18n/locales/zh/admin/index.ts`（+2 行）、`frontend/src/i18n/locales/en/admin/index.ts`（+2 行）
- Modify: `frontend/src/i18n/locales/zh/common.ts`（nav 块，约 171 行处 +1 行）、`frontend/src/i18n/locales/en/common.ts`（同）

**Interfaces:**
- Consumes: 无
- Produces: i18n key 空间 `admin.monitoring.*`（Task 6-12 的组件使用）、`nav.monitoring`（Task 13 侧边栏使用）

- [ ] **Step 1: 创建 zh locale**

创建 `frontend/src/i18n/locales/zh/admin/monitoring.ts`：

```ts
export default {
  monitoring: {
    title: '监控中心',
    description: '请求健康度、首字节延迟与账号级异常的统一视图',
    coverage: '数据覆盖 {start} 至 {end}',
    health: {
      complete: '采集完整',
      degraded: '采集降级',
      pending: '等待采集'
    },
    degradedBanner: '采集器当前降级，指标可能不完整。已丢弃 {dropped} 个样本，{pending} 个待写入。',
    protection: {
      enabled: '首 Token 保护 · {seconds}s',
      disabled: '首 Token 保护未启用',
      adjust: '调整'
    },
    filters: {
      range: '时间范围',
      platform: '平台',
      allPlatforms: '全部平台',
      model: '模型',
      modelPlaceholder: '按模型过滤'
    },
    kpi: {
      availability: '可用率',
      failureRate: '失败率',
      ttftTimeoutRate: 'TTFT 超时率',
      recoveryRate: '换号恢复率',
      p95Ttft: 'P95 TTFT',
      requestsContext: '{count} 次请求',
      ratioContext: '{numerator} / {denominator}',
      timeoutsContext: '{count} 次超时',
      p95TtftContext: 'P50 {p50} · 总耗时 {duration}'
    },
    funnel: {
      title: '首 Token 保护路径',
      subtitle: '超时触发后的换号恢复与最终结果',
      controlled: '受控请求',
      triggered: '触发超时',
      recovered: '换号恢复',
      finalFailure: '最终失败',
      platformNote: '漏斗数据不随平台筛选变化'
    },
    trends: {
      rates: '请求健康趋势',
      latency: '延迟趋势',
      availability: '可用率',
      failureRate: '失败率',
      ttftTimeoutRate: 'TTFT 超时率',
      p50Ttft: 'P50 TTFT',
      p95Ttft: 'P95 TTFT',
      p95Duration: 'P95 总耗时',
      empty: '所选时间段暂无数据'
    },
    accounts: {
      title: '账号表现',
      total: '{count} 个账号',
      searchPlaceholder: '搜索账号名称或 ID',
      account: '账号',
      platform: '平台',
      status: '状态',
      availability: '可用率',
      failureRate: '失败率',
      ttftTimeoutRate: 'TTFT 超时率',
      p95Ttft: 'P95 TTFT',
      samples: '样本数',
      healthy: '健康',
      watch: '关注',
      risk: '风险',
      lowSample: '样本不足',
      empty: '所选时间段暂无账号性能数据'
    },
    failures: {
      title: '失败分布',
      empty: '暂无失败记录',
      outcomes: {
        ttft_timeout: 'TTFT 超时',
        rate_limit: '限流',
        auth: '鉴权',
        upstream_4xx: '上游 4xx',
        upstream_5xx: '上游 5xx',
        transport: '网络传输',
        protocol: '协议',
        other_failure: '其他失败'
      }
    },
    drawer: {
      title: '账号性能详情',
      loading: '正在加载账号详情',
      empty: '暂无可供分析的性能数据',
      availability: '可用率',
      failureRate: '失败率',
      p95Ttft: 'P95 TTFT',
      p95Duration: 'P95 总耗时',
      successContext: '{success} / {total} 次成功',
      failureContext: '{failure} / {total} 次失败',
      ttftContext: '首字节响应延迟',
      durationContext: '完整请求耗时',
      trendTitle: '性能趋势',
      failureTitle: '失败分布'
    },
    settings: {
      title: '首 Token 超时保护',
      description: '请求超过设定秒数未返回首个 Token 时，自动切换账号重试。',
      enabled: '启用保护',
      timeoutSeconds: '超时时间（秒）',
      timeoutError: '超时时间必须是 1-300 的整数',
      effectiveEnabled: '当前生效：{seconds} 秒',
      effectiveDisabled: '当前未启用'
    },
    empty: {
      title: '暂无可分析样本',
      description: '性能样本会在部署完成并处理请求后逐步累积。'
    },
    errors: {
      overview: '无法加载监控概览',
      accounts: '无法加载账号性能数据',
      investigation: '无法加载账号性能详情',
      settings: '无法保存设置'
    }
  }
}
```

- [ ] **Step 2: 创建 en locale**

创建 `frontend/src/i18n/locales/en/admin/monitoring.ts`：

```ts
export default {
  monitoring: {
    title: 'Monitoring',
    description: 'A unified view of request health, first-byte latency, and account-level anomalies',
    coverage: 'Coverage {start} to {end}',
    health: {
      complete: 'Collection healthy',
      degraded: 'Collection degraded',
      pending: 'Awaiting collection'
    },
    degradedBanner: 'The collector is degraded; metrics may be incomplete. {dropped} samples dropped, {pending} pending.',
    protection: {
      enabled: 'First token protection · {seconds}s',
      disabled: 'First token protection off',
      adjust: 'Adjust'
    },
    filters: {
      range: 'Time range',
      platform: 'Platform',
      allPlatforms: 'All platforms',
      model: 'Model',
      modelPlaceholder: 'Filter by model'
    },
    kpi: {
      availability: 'Availability',
      failureRate: 'Failure rate',
      ttftTimeoutRate: 'TTFT timeout rate',
      recoveryRate: 'Failover recovery rate',
      p95Ttft: 'P95 TTFT',
      requestsContext: '{count} requests',
      ratioContext: '{numerator} / {denominator}',
      timeoutsContext: '{count} timeouts',
      p95TtftContext: 'P50 {p50} · duration {duration}'
    },
    funnel: {
      title: 'First token protection path',
      subtitle: 'Failover recovery and final outcomes after timeouts',
      controlled: 'Controlled requests',
      triggered: 'Timeout triggered',
      recovered: 'Recovered via failover',
      finalFailure: 'Final failure',
      platformNote: 'Funnel data is not affected by the platform filter'
    },
    trends: {
      rates: 'Request health trend',
      latency: 'Latency trend',
      availability: 'Availability',
      failureRate: 'Failure rate',
      ttftTimeoutRate: 'TTFT timeout rate',
      p50Ttft: 'P50 TTFT',
      p95Ttft: 'P95 TTFT',
      p95Duration: 'P95 duration',
      empty: 'No data for the selected range'
    },
    accounts: {
      title: 'Account performance',
      total: '{count} accounts',
      searchPlaceholder: 'Search account name or ID',
      account: 'Account',
      platform: 'Platform',
      status: 'Status',
      availability: 'Availability',
      failureRate: 'Failure rate',
      ttftTimeoutRate: 'TTFT timeout rate',
      p95Ttft: 'P95 TTFT',
      samples: 'Samples',
      healthy: 'Healthy',
      watch: 'Watch',
      risk: 'At risk',
      lowSample: 'Low sample',
      empty: 'No account performance data for the selected range'
    },
    failures: {
      title: 'Failure distribution',
      empty: 'No failures recorded',
      outcomes: {
        ttft_timeout: 'TTFT timeout',
        rate_limit: 'Rate limit',
        auth: 'Auth',
        upstream_4xx: 'Upstream 4xx',
        upstream_5xx: 'Upstream 5xx',
        transport: 'Transport',
        protocol: 'Protocol',
        other_failure: 'Other'
      }
    },
    drawer: {
      title: 'Account performance details',
      loading: 'Loading account details',
      empty: 'No performance data to analyze',
      availability: 'Availability',
      failureRate: 'Failure rate',
      p95Ttft: 'P95 TTFT',
      p95Duration: 'P95 duration',
      successContext: '{success} / {total} succeeded',
      failureContext: '{failure} / {total} failed',
      ttftContext: 'Time to first byte',
      durationContext: 'Full request duration',
      trendTitle: 'Performance trend',
      failureTitle: 'Failure distribution'
    },
    settings: {
      title: 'First token timeout protection',
      description: 'Automatically retries with another account when the first token takes longer than the configured seconds.',
      enabled: 'Enable protection',
      timeoutSeconds: 'Timeout (seconds)',
      timeoutError: 'Timeout must be an integer between 1 and 300',
      effectiveEnabled: 'Effective: {seconds}s',
      effectiveDisabled: 'Currently disabled'
    },
    empty: {
      title: 'No samples yet',
      description: 'Performance samples accumulate after deployment once requests are processed.'
    },
    errors: {
      overview: 'Failed to load monitoring overview',
      accounts: 'Failed to load account performance data',
      investigation: 'Failed to load account details',
      settings: 'Failed to save settings'
    }
  }
}
```

- [ ] **Step 3: 注册 locale 与 nav key**

1. 两个 `admin/index.ts`：import 区末尾加 `import monitoring from './monitoring'`，spread 区末尾加 `...monitoring,`。
2. `zh/common.ts` nav 块（`ttftMonitoring` 附近）加一行 `monitoring: '监控中心',`；`en/common.ts` 加 `monitoring: 'Monitoring',`。（旧 key `ttftMonitoring`/`accountPerformance` 保留，Task 14 统一清理。）

- [ ] **Step 4: 验证**

Run: `cd frontend && npm run test:run -- src/i18n && npm run typecheck`
Expected: PASS（locale 编译测试通过）

- [ ] **Step 5: Commit**

```bash
git add frontend/src/i18n/locales/zh/admin/monitoring.ts frontend/src/i18n/locales/en/admin/monitoring.ts frontend/src/i18n/locales/zh/admin/index.ts frontend/src/i18n/locales/en/admin/index.ts frontend/src/i18n/locales/zh/common.ts frontend/src/i18n/locales/en/common.ts
git commit -m "feat(monitoring): add monitoring center locale files"
```

---

### Task 6: 前端 — MetricTrendCard 组件

**Files:**
- Create: `frontend/src/views/admin/monitoring/components/MetricTrendCard.vue`
- Test: `frontend/src/views/admin/monitoring/components/__tests__/MetricTrendCard.spec.ts`
- 蓝本: `frontend/src/views/admin/performance/components/PerformanceMetricCard.vue`（可整段复制改造）

**Interfaces:**
- Consumes: `@/components/icons/Icon.vue`（不需要可去掉）
- Produces: props `{ label: string; value: string; context: string; tone: 'success' | 'danger' | 'warning' | 'neutral'; trend?: number[] }`。label/value/context 由父组件翻译后传入（组件内不用 i18n）

- [ ] **Step 1: 写失败测试**

创建 `__tests__/MetricTrendCard.spec.ts`（模仿 `PerformanceMetricCard.spec.ts`）：

```ts
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import MetricTrendCard from '../MetricTrendCard.vue'

const baseProps = { label: '可用率', value: '99.95%', context: '10,000 次请求', tone: 'success' as const }

describe('MetricTrendCard', () => {
  it('renders value, context and sparkline for a trend', () => {
    const wrapper = mount(MetricTrendCard, { props: { ...baseProps, trend: [0.998, 0.999, 0.9995] } })
    expect(wrapper.text()).toContain('99.95%')
    expect(wrapper.text()).toContain('10,000 次请求')
    expect(wrapper.get('[data-testid="metric-trend-sparkline"]').attributes('aria-label')).toContain('可用率')
  })

  it.each([{ trend: [] }, { trend: [0.9995] }])('hides sparkline for an incomplete trend', ({ trend }) => {
    const wrapper = mount(MetricTrendCard, { props: { ...baseProps, trend } })
    expect(wrapper.find('[data-testid="metric-trend-sparkline"]').exists()).toBe(false)
  })

  it('applies the tone class to the sparkline stroke', () => {
    const wrapper = mount(MetricTrendCard, { props: { ...baseProps, tone: 'danger', trend: [1, 2, 3] } })
    expect(wrapper.get('[data-testid="metric-trend-sparkline"] polyline').classes().join(' ')).toContain('stroke-red-500')
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd frontend && npm run test:run -- src/views/admin/monitoring/components/__tests__/MetricTrendCard.spec.ts`
Expected: FAIL — 组件不存在

- [ ] **Step 3: 实现组件**

创建 `MetricTrendCard.vue`：以 `PerformanceMetricCard.vue` 为蓝本，做以下确定性修改：
1. `PerformanceTone` 改为 `type MetricTone = 'success' | 'danger' | 'warning' | 'neutral'`（删除 `info`，`sky` 相关条目一并删除）。
2. 删除 `icon` prop 与图标渲染块（`PerformanceIconName`、`toneClasses` 图标方块、`wide` prop 及 `wide` 分支样式）。
3. sparkline 的 `data-testid` 改为 `metric-trend-sparkline`。
4. 卡片根节点样式固定为 `relative min-w-0 overflow-hidden rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800`（无 wide 变体）。
5. 值与上下文排版保持蓝本样式（`text-2xl font-semibold tabular-nums`、sparkline 100x32 viewBox）。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd frontend && npm run test:run -- src/views/admin/monitoring/components/__tests__/MetricTrendCard.spec.ts && npm run typecheck`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/views/admin/monitoring/components/MetricTrendCard.vue frontend/src/views/admin/monitoring/components/__tests__/MetricTrendCard.spec.ts
git commit -m "feat(monitoring): add metric trend card component"
```

---

### Task 7: 前端 — 图表组件迁移（MonitoringTrendChart + FailureDistribution）

**Files:**
- Create（git mv 改造）: `frontend/src/views/admin/monitoring/components/MonitoringTrendChart.vue`（源 `performance/components/PerformanceTrendChart.vue`）
- Create: `frontend/src/views/admin/monitoring/components/FailureDistribution.vue`（新建，替代 `PerformanceFailureDistribution.vue`，接口改为预标注数据）
- Test: `__tests__/FailureDistribution.spec.ts`（MonitoringTrendChart 为纯迁移、无行为变化，不新增测试）

**Interfaces:**
- Consumes: chart.js、vue-chartjs；`PerformanceTimePoint` 类型（来自 `@/api/admin/monitoring` re-export）
- Produces:
  - `MonitoringTrendChart` props 与 `PerformanceTrendChart` 完全一致：`{ title: string; points: PerformanceTimePoint[]; timeRange: string; series: PerformanceSeriesDefinition[]; loading?: boolean; valueFormat?: 'number' | 'percent' }`；`PerformanceSeriesDefinition` 类型从本文件 export
  - `FailureDistribution` props `{ failures: Array<{ label: string; count: number; color: string }>; title: string; loading?: boolean }`（label/color 由父组件用 i18n 与色板生成，组件不内建映射）

- [ ] **Step 1: 迁移 MonitoringTrendChart**

```bash
git mv frontend/src/views/admin/performance/components/PerformanceTrendChart.vue frontend/src/views/admin/monitoring/components/MonitoringTrendChart.vue
mkdir -p frontend/src/views/admin/monitoring/components/__tests__
```

（该组件无既有 spec 文件，迁移后无需搬运测试。）

修改 `MonitoringTrendChart.vue`：`import type { PerformanceTimePoint } from '@/api/admin/performance'` 改为 `from '@/api/admin/monitoring'`；`export interface PerformanceSeriesDefinition` 保留原名导出（避免连锁改名）。

- [ ] **Step 2: 写失败测试（FailureDistribution）**

```ts
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import FailureDistribution from '../FailureDistribution.vue'

describe('FailureDistribution', () => {
  it('shows empty text when all counts are zero', () => {
    const wrapper = mount(FailureDistribution, { props: { title: '失败分布', failures: [{ label: '限流', count: 0, color: '#f97316' }] } })
    expect(wrapper.text()).toContain('暂无失败记录')
  })

  it('renders provided labels in the accessible summary', () => {
    const wrapper = mount(FailureDistribution, { props: { title: '失败分布', failures: [{ label: '限流', count: 3, color: '#f97316' }] } })
    expect(wrapper.text()).toContain('限流：3')
  })
})
```

- [ ] **Step 3: 实现 FailureDistribution**

以 `PerformanceFailureDistribution.vue` 为蓝本，确定性修改：
1. props 改为 `failures: Array<{ label: string; count: number; color: string }>`；删除 `outcomeLabels`、`normalizeOutcome`、`readableOutcome`、`colorForOutcome` 及 `Outcome/Count` 接口。
2. `visibleFailures` = `props.failures.filter((f) => f.count > 0)`；chart labels/data/backgroundColor 直接取 `label/count/color`。
3. 空态与加载文案保留 props 插槽：`<slot name="empty">暂无失败记录</slot>`（父组件传 i18n 文案；默认文案允许存在，父组件总是覆盖）。
4. summary 计算改为 `${label}：${count}` 拼接。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd frontend && npm run test:run -- src/views/admin/monitoring/components/__tests__ && npm run typecheck`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/views/admin/monitoring frontend/src/views/admin/performance
git commit -m "feat(monitoring): migrate and generalize trend and failure charts"
```

---

### Task 8: 前端 — ProtectionFunnel 组件

**Files:**
- Create: `frontend/src/views/admin/monitoring/components/ProtectionFunnel.vue`
- Test: `__tests__/ProtectionFunnel.spec.ts`
- 蓝本: `frontend/src/views/admin/ttft/components/TTFTRecoveryFunnel.vue`

**Interfaces:**
- Consumes: `MonitoringTTFTSummary`（`@/api/admin/monitoring`）
- Produces: props `{ summary: MonitoringTTFTSummary }`；`controlled_requests === 0` 时整体不渲染

- [ ] **Step 1: 写失败测试**

```ts
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ProtectionFunnel from '../ProtectionFunnel.vue'

const summary = {
  controlled_requests: 1000,
  client_canceled_requests: 10,
  attempt_ttft_timeout_rate: { numerator: 100, denominator: 1000, rate: 0.1 },
  recovery_rate: { numerator: 80, denominator: 100, rate: 0.8 },
  final_ttft_failure_rate: { numerator: 20, denominator: 100, rate: 0.2 },
  other_final_failure_rate: { numerator: 0, denominator: 100, rate: 0 }
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

describe('ProtectionFunnel', () => {
  it('renders four stages with conversion rates', () => {
    const wrapper = mount(ProtectionFunnel, { props: { summary } })
    expect(wrapper.text()).toContain('1,000')
    expect(wrapper.text()).toContain('100')
    expect(wrapper.text()).toContain('80')
    expect(wrapper.text()).toContain('20')
    expect(wrapper.text()).toContain('10.0%') // 触发超时占比
    expect(wrapper.text()).toContain('80.0%') // 恢复率
    expect(wrapper.text()).toContain('20.0%') // 最终失败率
  })

  it('renders nothing without controlled requests', () => {
    const wrapper = mount(ProtectionFunnel, { props: { summary: { ...summary, controlled_requests: 0 } } })
    expect(wrapper.find('[data-testid="protection-funnel"]').exists()).toBe(false)
  })
})
```

（`vi` import 放文件顶部；mock 需 hoist 在 import 组件之前——参照 `FirstTokenTimeoutView.spec.ts` 的写法顺序。）

- [ ] **Step 2: 运行测试确认失败**

Run: `cd frontend && npm run test:run -- src/views/admin/monitoring/components/__tests__/ProtectionFunnel.spec.ts`
Expected: FAIL

- [ ] **Step 3: 实现组件**

创建 `ProtectionFunnel.vue`：

```vue
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MonitoringTTFTSummary } from '@/api/admin/monitoring'

const props = defineProps<{ summary: MonitoringTTFTSummary }>()
const { t } = useI18n()

const visible = computed(() => props.summary.controlled_requests > 0)

const stages = computed(() => [
  { label: t('admin.monitoring.funnel.controlled'), value: props.summary.controlled_requests, rate: null as string | null, tone: 'border-blue-200 bg-blue-50 text-blue-800 dark:border-blue-900/70 dark:bg-blue-950/30 dark:text-blue-200' },
  { label: t('admin.monitoring.funnel.triggered'), value: props.summary.attempt_ttft_timeout_rate.numerator, rate: formatRate(props.summary.attempt_ttft_timeout_rate.rate), tone: 'border-red-200 bg-red-50 text-red-800 dark:border-red-900/70 dark:bg-red-950/30 dark:text-red-200' },
  { label: t('admin.monitoring.funnel.recovered'), value: props.summary.recovery_rate.numerator, rate: formatRate(props.summary.recovery_rate.rate), tone: 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900/70 dark:bg-emerald-950/30 dark:text-emerald-200' },
  { label: t('admin.monitoring.funnel.finalFailure'), value: props.summary.final_ttft_failure_rate.numerator, rate: formatRate(props.summary.final_ttft_failure_rate.rate), tone: 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/70 dark:bg-amber-950/30 dark:text-amber-200' }
])

const accessibleSummary = computed(() => stages.value.map((stage) => `${stage.label} ${stage.value}`).join('，'))

function formatRate(rate: number): string {
  return `${((Number.isFinite(rate) ? rate : 0) * 100).toFixed(1)}%`
}
</script>

<template>
  <section v-if="visible" data-testid="protection-funnel" class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800" :aria-label="accessibleSummary">
    <div class="mb-4 flex items-center justify-between gap-3">
      <div>
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.monitoring.funnel.title') }}</h2>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.monitoring.funnel.subtitle') }}</p>
      </div>
      <span class="text-xs tabular-nums text-gray-500 dark:text-gray-400">{{ summary.controlled_requests.toLocaleString() }}</span>
    </div>
    <ol class="grid gap-2 sm:grid-cols-4">
      <li v-for="(stage, index) in stages" :key="stage.label" class="relative min-w-0">
        <div class="min-h-20 rounded-md border p-3" :class="stage.tone">
          <div class="text-xs font-medium">{{ stage.label }}</div>
          <div class="mt-2 text-2xl font-semibold tabular-nums">{{ stage.value.toLocaleString() }}</div>
        </div>
        <div v-if="index < stages.length - 1" class="hidden text-center text-xs tabular-nums text-gray-400 sm:absolute sm:-right-2 sm:top-8 sm:z-10 sm:block sm:w-4" aria-hidden="true">→</div>
        <div v-if="stage.rate" class="mt-1 text-center text-xs tabular-nums text-gray-500 dark:text-gray-400">{{ stage.rate }}</div>
      </li>
    </ol>
  </section>
</template>
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd frontend && npm run test:run -- src/views/admin/monitoring/components/__tests__/ProtectionFunnel.spec.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/views/admin/monitoring/components/ProtectionFunnel.vue frontend/src/views/admin/monitoring/components/__tests__/ProtectionFunnel.spec.ts
git commit -m "feat(monitoring): add first token protection funnel"
```

---

### Task 9: 前端 — AccountHealthTable 组件

**Files:**
- Create: `frontend/src/views/admin/monitoring/components/AccountHealthTable.vue`
- Test: `__tests__/AccountHealthTable.spec.ts`
- 蓝本: `frontend/src/views/admin/performance/components/PerformanceAccountTable.vue`

**Interfaces:**
- Consumes: `PerformanceAccountPage`、`PerformanceOrder`（`@/api/admin/monitoring`）；共享组件 `SearchInput.vue`、`Pagination.vue`、`PlatformTypeBadge.vue`、`PlatformIcon.vue`、`Icon.vue`
- Produces: props `{ page: PerformanceAccountPage | null; loading: boolean; error: string; sort: string; order: PerformanceOrder; search: string }`；emits `{ retry: []; sort: [value: string]; page: [value: number]; select: [account: PerformanceAccountItem]; 'update:search': [value: string] }`

- [ ] **Step 1: 写失败测试**

```ts
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import AccountHealthTable from '../AccountHealthTable.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const counters = {
  attempt_count: 100, success_count: 95, client_canceled_count: 0, ttft_timeout_count: 4,
  rate_limit_count: 1, auth_count: 0, upstream_4xx_count: 0, upstream_5xx_count: 0,
  transport_count: 0, protocol_count: 0, other_failure_count: 0, failover_count: 0,
  ttft_sum_ms: 0, duration_sum_ms: 0,
  ttft_latency: { Samples: 0, LE1000MS: 0, LE2500MS: 0, LE5000MS: 0, LE10000MS: 0, LE30000MS: 0, GT30000MS: 0 },
  duration_latency: { Samples: 0, LE1000MS: 0, LE2500MS: 0, LE5000MS: 0, LE10000MS: 0, LE30000MS: 0, GT30000MS: 0 }
}

const page = {
  items: [{ account_id: 7, account_name: 'prod-1', account_type: '', platform: 'openai', counters, availability: 0.95, failure_rate: 0.05, health_score: 0.95, low_sample: false, p95_ttft_ms: 1200, p95_duration_ms: 8000 }],
  total: 1, page: 1, page_size: 20, pages: 1,
  collection_health: { status: 'complete', dropped_samples: 0, pending_samples: 0, last_successful_flush_at: null }
}

describe('AccountHealthTable', () => {
  it('renders the derived ttft timeout rate column', () => {
    const wrapper = mount(AccountHealthTable, { props: { page, loading: false, error: '', sort: 'health_score', order: 'asc', search: '' } })
    expect(wrapper.text()).toContain('4.00%') // 4 / 100
  })

  it('emits select with the account on row click', async () => {
    const wrapper = mount(AccountHealthTable, { props: { page, loading: false, error: '', sort: 'health_score', order: 'asc', search: '' } })
    await wrapper.get('tbody tr').trigger('click')
    expect(wrapper.emitted('select')?.[0]?.[0]).toMatchObject({ account_id: 7 })
  })

  it('forwards search input as update:search', async () => {
    const wrapper = mount(AccountHealthTable, { props: { page, loading: false, error: '', sort: 'health_score', order: 'asc', search: '' } })
    await wrapper.get('[data-testid="account-search"] input').setValue('prod')
    expect(wrapper.emitted('update:search')?.at(-1)?.[0]).toBe('prod')
  })
})
```

> 若 `PerformanceAccountItem` 类型字段名与上面 page 字面量不一致（如 `low_sample`），以 `frontend/src/api/admin/performance.ts` 的实际类型为准调整测试字面量。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd frontend && npm run test:run -- src/views/admin/monitoring/components/__tests__/AccountHealthTable.spec.ts`
Expected: FAIL

- [ ] **Step 3: 实现组件**

以 `PerformanceAccountTable.vue` 为蓝本整段复制为 `AccountHealthTable.vue`，做以下确定性修改：

1. import 类型从 `@/api/admin/performance` 改为 `@/api/admin/monitoring`；增加 `import SearchInput from '@/components/common/SearchInput.vue'`、`import Pagination from '@/components/common/Pagination.vue'`；`useI18n` 引入并替换全部硬编码中文为 `t('admin.monitoring.accounts.*')`。
2. props 增加 `search: string`；emits 增加 `'update:search': [value: string]`。
3. 标题区下方加搜索行：

```vue
    <div class="mt-3 max-w-xs" data-testid="account-search">
      <SearchInput :model-value="search" :placeholder="t('admin.monitoring.accounts.searchPlaceholder')" @update:model-value="(value: string) => emit('update:search', value)" />
    </div>
```

4. 表格列改为：账号、平台、状态、可用率、失败率、TTFT 超时率、P95 TTFT、样本数。删除「P95 总耗时」「成功调用」「失败调用」列；「成功/失败调用」信息保留在钻取抽屉。可排序列：`health_score`、`availability`、`failure_rate`、`p95_ttft_ms`、`samples`（对应蓝本已有 sort 键）。TTFT 超时率列不可排序：

```vue
              <th class="px-3 py-2 font-medium">{{ t('admin.monitoring.accounts.ttftTimeoutRate') }}</th>
```

单元格：

```vue
              <td class="px-3 py-3 tabular-nums">{{ ttftTimeoutRate(item) }}</td>
```

script 中加：

```ts
function ttftTimeoutRate(account: PerformanceAccount) {
  const attempts = account.counters.attempt_count
  if (!attempts) return '--'
  return `${((account.counters.ttft_timeout_count / attempts) * 100).toFixed(2)}%`
}
```

样本数列：`{{ item.counters.attempt_count.toLocaleString() }}`。
5. 删除手写的「上一页/下一页」分页块，替换为：

```vue
    <Pagination v-if="page && page.pages > 1" class="mt-4" :total="page.total" :page="page.page" :page-size="page.page_size" :show-page-size-selector="false" @update:page="(value: number) => emit('page', value)" />
```
6. i18n 健康状态标签用 `t('admin.monitoring.accounts.healthy' | 'watch' | 'risk' | 'lowSample')`。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd frontend && npm run test:run -- src/views/admin/monitoring/components/__tests__/AccountHealthTable.spec.ts && npm run typecheck`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/views/admin/monitoring/components/AccountHealthTable.vue frontend/src/views/admin/monitoring/components/__tests__/AccountHealthTable.spec.ts
git commit -m "feat(monitoring): add account health table with search and ttft rate"
```

---

### Task 10: 前端 — TTFTSettingsDialog 组件

**Files:**
- Create: `frontend/src/views/admin/monitoring/components/TTFTSettingsDialog.vue`
- Test: `__tests__/TTFTSettingsDialog.spec.ts`
- 蓝本: `frontend/src/views/admin/ttft/components/TTFTSettingsBar.vue`（逻辑部分）

**Interfaces:**
- Consumes: `BaseDialog.vue`、`Toggle.vue`；`FirstTokenTimeoutSettings`（`@/api/admin/monitoring`）
- Produces: props `{ open: boolean; settings: FirstTokenTimeoutSettings | null; saving: boolean; error: string }`；emits `{ close: []; save: [payload: { enabled: boolean; timeout_seconds: number }] }`

- [ ] **Step 1: 写失败测试**

```ts
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import TTFTSettingsDialog from '../TTFTSettingsDialog.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const settings = {
  saved: { enabled: true, timeout_seconds: 20 },
  effective: { enabled: true, timeout_seconds: 20 },
  loaded_at: '2026-07-15T00:00:00Z'
}

describe('TTFTSettingsDialog', () => {
  it('rejects out-of-range timeout values', async () => {
    const wrapper = mount(TTFTSettingsDialog, { props: { open: true, settings, saving: false, error: '' } })
    await wrapper.get('input[type="number"]').setValue(0)
    await wrapper.get('form').trigger('submit.prevent')
    expect(wrapper.emitted('save')).toBeUndefined()
  })

  it('emits save with current values', async () => {
    const wrapper = mount(TTFTSettingsDialog, { props: { open: true, settings, saving: false, error: '' } })
    await wrapper.get('input[type="number"]').setValue(45)
    await wrapper.get('form').trigger('submit.prevent')
    expect(wrapper.emitted('save')?.[0]?.[0]).toEqual({ enabled: true, timeout_seconds: 45 })
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd frontend && npm run test:run -- src/views/admin/monitoring/components/__tests__/TTFTSettingsDialog.spec.ts`
Expected: FAIL

- [ ] **Step 3: 实现组件**

逻辑沿用 `TTFTSettingsBar.vue`（`enabled`/`timeoutSeconds` ref、watch settings 回填、1–300 整数校验），模板改为：

```vue
<template>
  <BaseDialog :show="open" :title="t('admin.monitoring.settings.title')" width="wide" @close="emit('close')">
    <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.monitoring.settings.description') }}</p>
    <form class="mt-4 space-y-4" @submit.prevent="save">
      <label class="flex items-center gap-3 text-sm font-medium text-gray-700 dark:text-gray-200">
        <Toggle v-model="enabled" />
        {{ t('admin.monitoring.settings.enabled') }}
      </label>
      <label class="grid gap-1 text-xs font-medium text-gray-500 dark:text-gray-400">
        <span>{{ t('admin.monitoring.settings.timeoutSeconds') }}</span>
        <input v-model.number="timeoutSeconds" type="number" min="1" max="300" inputmode="numeric" class="h-10 w-32 rounded-md border border-gray-300 bg-white px-3 text-sm text-gray-900 outline-none focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-600 dark:bg-dark-900 dark:text-white" :aria-invalid="validationError" />
      </label>
      <p v-if="validationError" class="text-sm text-red-600 dark:text-red-400">{{ t('admin.monitoring.settings.timeoutError') }}</p>
      <p v-else-if="error" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <p v-else-if="settings" class="text-sm text-gray-600 dark:text-gray-300">{{ settings.effective.enabled ? t('admin.monitoring.settings.effectiveEnabled', { seconds: settings.effective.timeout_seconds }) : t('admin.monitoring.settings.effectiveDisabled') }}</p>
      <div class="flex justify-end gap-2">
        <button type="button" class="h-10 rounded-md border border-gray-300 px-4 text-sm font-medium text-gray-700 hover:bg-gray-50 dark:border-dark-600 dark:text-gray-200 dark:hover:bg-dark-800" @click="emit('close')">{{ t('common.cancel') }}</button>
        <button data-testid="ttft-settings-save" type="submit" :disabled="saving || validationError" class="h-10 rounded-md bg-primary-600 px-4 text-sm font-medium text-white hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-60">{{ saving ? t('common.saving') : t('common.save') }}</button>
      </div>
    </form>
  </BaseDialog>
</template>
```

script setup 部分：props/emits 按 Interfaces 定义；`enabled = ref(false)`、`timeoutSeconds = ref(30)`、`validationError` computed 与 `save()` 逻辑同蓝本；watch 同蓝本。`BaseDialog` 的 `width="wide"` 是合法值（`DialogWidth = 'narrow' | 'normal' | 'wide' | 'extra-wide' | 'full'`）。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd frontend && npm run test:run -- src/views/admin/monitoring/components/__tests__/TTFTSettingsDialog.spec.ts && npm run typecheck`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/views/admin/monitoring/components/TTFTSettingsDialog.vue frontend/src/views/admin/monitoring/components/__tests__/TTFTSettingsDialog.spec.ts
git commit -m "feat(monitoring): add ttft settings dialog"
```

---

### Task 11: 前端 — InvestigationDrawer 迁移

**Files:**
- Create（git mv 改造）: `frontend/src/views/admin/monitoring/components/InvestigationDrawer.vue`（源 `performance/components/PerformanceInvestigationDrawer.vue`）
- Test: 迁移既有 spec（若存在）并按新 import 修正

**Interfaces:**
- Consumes: `MetricTrendCard`、`MonitoringTrendChart`、`FailureDistribution`（本目录）；`BaseDialog.vue`、`PlatformTypeBadge.vue`；`performanceMetricsFromCounters`、`performanceMetricsFromTimePoint`、`PerformanceInvestigation`（`@/api/admin/monitoring`）
- Produces: props `{ open: boolean; account: PerformanceAccountItem | null; investigation: PerformanceInvestigation | null; loading: boolean; error: string }`；emits `{ close: []; retry: [] }`

- [ ] **Step 1: git mv 并改造**

```bash
git mv frontend/src/views/admin/performance/components/PerformanceInvestigationDrawer.vue frontend/src/views/admin/monitoring/components/InvestigationDrawer.vue
git mv frontend/src/views/admin/performance/components/__tests__/PerformanceInvestigationDrawer.spec.ts frontend/src/views/admin/monitoring/components/__tests__/InvestigationDrawer.spec.ts
```

确定性修改：
1. import：`@/api/admin/performance` → `@/api/admin/monitoring`；`./PerformanceMetricCard.vue` → `./MetricTrendCard.vue`；`./PerformanceTrendChart.vue` → `./MonitoringTrendChart.vue`；`./PerformanceFailureDistribution.vue` → `./FailureDistribution.vue`。
2. 四张指标卡改为 `MetricTrendCard`，删除 `icon` prop；label 改用 `t('admin.monitoring.drawer.*')`；`tone` 映射：可用率 `success`、失败率 `danger`、P95 TTFT `neutral`、P95 总耗时 `neutral`。
3. 全部硬编码中文替换为 `admin.monitoring.drawer.*` 与 `admin.monitoring.trends.*` key。
4. `FailureDistribution` 调用改为预标注数据：script 中把 `investigation.failures` 映射为 `{ label, count, color }`（label 用 `t('admin.monitoring.failures.outcomes.<key>')`，normalize 逻辑——小写、驼峰转下划线——从旧 `PerformanceFailureDistribution.vue` 搬入本组件；color 用色板：ttft_timeout `#ef4444`、rate_limit `#f97316`、其他 `#64748b`）。
5. 趋势 series 标签改用 `admin.monitoring.trends.*` key。

- [ ] **Step 2: 运行既有测试并修正**

Run: `cd frontend && npm run test:run -- src/views/admin/monitoring && npm run typecheck`
Expected: PASS（若迁移的 spec 断言了旧硬编码文案，更新断言为 i18n key 或新文案）

- [ ] **Step 3: Commit**

```bash
git add frontend/src/views/admin/monitoring frontend/src/views/admin/performance
git commit -m "feat(monitoring): migrate investigation drawer"
```

---

### Task 12: 前端 — MonitoringView 页面

**Files:**
- Create: `frontend/src/views/admin/monitoring/MonitoringView.vue`
- Test: `frontend/src/views/admin/monitoring/__tests__/MonitoringView.spec.ts`

**Interfaces:**
- Consumes: Task 4 client、Task 5 i18n、Task 6-11 全部组件；`AppLayout.vue`；共享 `Select.vue`
- Produces: 页面路由组件（Task 13 注册）。URL query：`range`、`platform`、`model`

- [ ] **Step 1: 写失败测试**

创建 `__tests__/MonitoringView.spec.ts`（mock 模式参照 `FirstTokenTimeoutView.spec.ts`）：

```ts
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const getOverview = vi.hoisted(() => vi.fn())
const getAccounts = vi.hoisted(() => vi.fn())
const getSettings = vi.hoisted(() => vi.fn())
const getInvestigation = vi.hoisted(() => vi.fn())

vi.mock('@/api/admin/monitoring', () => ({
  default: { getOverview, getAccounts, getSettings, getInvestigation, updateSettings: vi.fn() },
  performanceMetricsFromCounters: () => ({}),
  performanceMetricsFromTimePoint: () => ({})
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return { ...actual, useRoute: () => ({ query: {} }), useRouter: () => ({ replace: vi.fn() }) }
})

import MonitoringView from '../MonitoringView.vue'

const overviewResponse = {
  performance: {
    summary: {
      attempts: 1000,
      availability: { numerator: 990, denominator: 1000, rate: 0.99 },
      failure_rate: { numerator: 10, denominator: 1000, rate: 0.01 },
      ttft_timeout_count: 5,
      p50_ttft_ms: 800, p95_ttft_ms: 2000, p95_duration_ms: 9000
    },
    trend: [],
    collection_health: { status: 'complete', dropped_samples: 0, pending_samples: 0, last_successful_flush_at: null },
    coverage_start: '2026-07-19T00:00:00Z',
    coverage_end: '2026-07-20T00:00:00Z'
  },
  ttft: {
    summary: {
      controlled_requests: 500, client_canceled_requests: 0,
      attempt_ttft_timeout_rate: { numerator: 50, denominator: 500, rate: 0.1 },
      recovery_rate: { numerator: 40, denominator: 50, rate: 0.8 },
      final_ttft_failure_rate: { numerator: 10, denominator: 50, rate: 0.2 },
      other_final_failure_rate: { numerator: 0, denominator: 50, rate: 0 }
    },
    trend: [], other_failures: [],
    completeness: { status: 'complete', dropped_samples: 0, pending_samples: 0, last_successful_flush_at: null }
  }
}

describe('MonitoringView', () => {
  beforeEach(() => {
    getOverview.mockReset().mockResolvedValue(overviewResponse)
    getAccounts.mockReset().mockResolvedValue({ items: [], total: 0, page: 1, pages: 0 })
    getSettings.mockReset().mockResolvedValue({ saved: { enabled: true, timeout_seconds: 30 }, effective: { enabled: true, timeout_seconds: 30 }, loaded_at: '2026-07-20T00:00:00Z' })
  })

  it('loads overview, accounts and settings on mount', async () => {
    mount(MonitoringView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } } })
    await flushPromises()
    expect(getOverview).toHaveBeenCalled()
    expect(getAccounts).toHaveBeenCalled()
    expect(getSettings).toHaveBeenCalled()
  })

  it('renders the protection badge with effective timeout', async () => {
    const wrapper = mount(MonitoringView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } } })
    await flushPromises()
    expect(wrapper.get('[data-testid="protection-badge"]').exists()).toBe(true)
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd frontend && npm run test:run -- src/views/admin/monitoring/__tests__/MonitoringView.spec.ts`
Expected: FAIL — 组件不存在

- [ ] **Step 3: 实现页面**

创建 `frontend/src/views/admin/monitoring/MonitoringView.vue`（完整代码）：

```vue
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import monitoringAPI, {
  performanceMetricsFromCounters,
  performanceMetricsFromTimePoint,
  type FirstTokenTimeoutSettings,
  type FirstTokenTimeoutSettingsValue,
  type MonitoringOverview,
  type MonitoringRange,
  type PerformanceAccountItem,
  type PerformanceAccountPage,
  type PerformanceInvestigation,
  type PerformanceOrder
} from '@/api/admin/monitoring'
import MetricTrendCard from './components/MetricTrendCard.vue'
import ProtectionFunnel from './components/ProtectionFunnel.vue'
import MonitoringTrendChart, { type PerformanceSeriesDefinition } from './components/MonitoringTrendChart.vue'
import FailureDistribution from './components/FailureDistribution.vue'
import AccountHealthTable from './components/AccountHealthTable.vue'
import InvestigationDrawer from './components/InvestigationDrawer.vue'
import TTFTSettingsDialog from './components/TTFTSettingsDialog.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const ranges: MonitoringRange[] = ['1h', '6h', '24h', '7d', '30d', '90d']

const range = ref<MonitoringRange>('24h')
const platform = ref('')
const model = ref('')
const settings = ref<FirstTokenTimeoutSettings | null>(null)
const overview = ref<MonitoringOverview | null>(null)
const accounts = ref<PerformanceAccountPage | null>(null)
const investigation = ref<PerformanceInvestigation | null>(null)
const selectedAccount = ref<PerformanceAccountItem | null>(null)
const settingsLoading = ref(true)
const overviewLoading = ref(true)
const accountsLoading = ref(true)
const investigationLoading = ref(false)
const settingsSaving = ref(false)
const settingsOpen = ref(false)
const settingsError = ref('')
const overviewError = ref('')
const accountsError = ref('')
const investigationError = ref('')
const hasOverviewLoaded = ref(false)
const hasAccountsLoaded = ref(false)
const accountSearch = ref('')
const accountSort = ref('health_score')
const accountOrder = ref<PerformanceOrder>('asc')
const accountPage = ref(1)
const accountPageSize = 20
let overviewGeneration = 0
let accountsGeneration = 0
let investigationGeneration = 0
let syncingRouteQuery = false
let accountSearchTimer: ReturnType<typeof setTimeout> | undefined

const pageFilters = computed(() => ({ range: range.value, platform: platform.value || undefined, model: model.value.trim() || undefined }))
const degradedHealth = computed(() => {
  const perfHealth = overview.value?.performance.collection_health
  if (perfHealth?.status === 'degraded') return perfHealth
  const ttftHealth = overview.value?.ttft.completeness
  return ttftHealth?.status === 'degraded' ? ttftHealth : null
})
const degraded = computed(() => degradedHealth.value !== null)
const hasSamples = computed(() => (overview.value?.performance.summary.attempts ?? 0) > 0)
const coverage = computed(() => {
  const perf = overview.value?.performance
  if (!perf) return ''
  return t('admin.monitoring.coverage', { start: formatDate(perf.coverage_start), end: formatDate(perf.coverage_end) })
})
const protectionLabel = computed(() => {
  const effective = settings.value?.effective
  if (!effective) return ''
  return effective.enabled
    ? t('admin.monitoring.protection.enabled', { seconds: effective.timeout_seconds })
    : t('admin.monitoring.protection.disabled')
})
const ttftTimeoutRate = computed(() => {
  const summary = overview.value?.performance.summary
  if (!summary || summary.availability.denominator <= 0) return 0
  return summary.ttft_timeout_count / summary.availability.denominator
})
const trendMetrics = computed(() => overview.value?.performance.trend.map(performanceMetricsFromTimePoint) ?? [])
const kpiCards = computed(() => {
  const perf = overview.value?.performance
  if (!perf) return []
  const summary = perf.summary
  const recovery = overview.value?.ttft.summary.recovery_rate
  return [
    { label: t('admin.monitoring.kpi.availability'), value: percent(summary.availability.rate), context: t('admin.monitoring.kpi.requestsContext', { count: summary.attempts.toLocaleString() }), tone: 'success' as const, trend: trendMetrics.value.map((m) => m.availability) },
    { label: t('admin.monitoring.kpi.failureRate'), value: percent(summary.failure_rate.rate), context: t('admin.monitoring.kpi.ratioContext', { numerator: summary.failure_rate.numerator, denominator: summary.failure_rate.denominator }), tone: 'danger' as const, trend: trendMetrics.value.map((m) => m.failure_rate) },
    { label: t('admin.monitoring.kpi.ttftTimeoutRate'), value: percent(ttftTimeoutRate.value), context: t('admin.monitoring.kpi.timeoutsContext', { count: summary.ttft_timeout_count }), tone: 'warning' as const, trend: trendMetrics.value.map((m) => m.ttft_timeout_rate) },
    { label: t('admin.monitoring.kpi.recoveryRate'), value: percent(recovery?.rate ?? 0), context: t('admin.monitoring.kpi.ratioContext', { numerator: recovery?.numerator ?? 0, denominator: recovery?.denominator ?? 0 }), tone: 'success' as const, trend: overview.value?.ttft.trend.map((point) => point.recovery_rate.rate) ?? [] },
    { label: t('admin.monitoring.kpi.p95Ttft'), value: milliseconds(summary.p95_ttft_ms), context: t('admin.monitoring.kpi.p95TtftContext', { p50: milliseconds(summary.p50_ttft_ms), duration: milliseconds(summary.p95_duration_ms) }), tone: 'neutral' as const, trend: trendMetrics.value.map((m) => m.p95_ttft_ms) }
  ]
})

const ratesSeries: PerformanceSeriesDefinition[] = [
  { label: t('admin.monitoring.trends.availability'), color: '#10b981', selector: (point) => performanceMetricsFromCounters(point.counters).availability, formatter: percent },
  { label: t('admin.monitoring.trends.failureRate'), color: '#ef4444', selector: (point) => performanceMetricsFromCounters(point.counters).failure_rate, formatter: percent, fill: false },
  { label: t('admin.monitoring.trends.ttftTimeoutRate'), color: '#f59e0b', selector: (point) => performanceMetricsFromCounters(point.counters).ttft_timeout_rate, formatter: percent, fill: false }
]
const latencySeries: PerformanceSeriesDefinition[] = [
  { label: t('admin.monitoring.trends.p50Ttft'), color: '#0ea5e9', selector: (point) => performanceMetricsFromTimePoint(point).p50_ttft_ms, formatter: milliseconds },
  { label: t('admin.monitoring.trends.p95Ttft'), color: '#8b5cf6', selector: (point) => performanceMetricsFromTimePoint(point).p95_ttft_ms, formatter: milliseconds, fill: false },
  { label: t('admin.monitoring.trends.p95Duration'), color: '#f97316', selector: (point) => performanceMetricsFromTimePoint(point).p95_duration_ms, formatter: milliseconds, fill: false }
]

const FAILURE_COLORS: Record<string, string> = { ttft_timeout: '#ef4444', rate_limit: '#f97316' }
const failureItems = computed(() => {
  const totals = new Map<string, number>()
  for (const point of overview.value?.performance.trend ?? []) {
    const counters = point.counters
    totals.set('ttft_timeout', (totals.get('ttft_timeout') ?? 0) + counters.ttft_timeout_count)
    totals.set('rate_limit', (totals.get('rate_limit') ?? 0) + counters.rate_limit_count)
    totals.set('auth', (totals.get('auth') ?? 0) + counters.auth_count)
    totals.set('upstream_4xx', (totals.get('upstream_4xx') ?? 0) + counters.upstream_4xx_count)
    totals.set('upstream_5xx', (totals.get('upstream_5xx') ?? 0) + counters.upstream_5xx_count)
    totals.set('transport', (totals.get('transport') ?? 0) + counters.transport_count)
    totals.set('protocol', (totals.get('protocol') ?? 0) + counters.protocol_count)
    totals.set('other_failure', (totals.get('other_failure') ?? 0) + counters.other_failure_count)
  }
  return [...totals.entries()].map(([key, count]) => ({ label: t(`admin.monitoring.failures.outcomes.${key}`), count, color: FAILURE_COLORS[key] ?? '#64748b' }))
})

const platformOptions = computed(() => [
  { value: '', label: t('admin.monitoring.filters.allPlatforms') },
  ...['anthropic', 'openai', 'gemini', 'antigravity', 'grok'].map((value) => ({ value, label: value }))
])

function percent(value: number) { return `${(value * 100).toFixed(2)}%` }
function milliseconds(value: number) { return value > 0 ? `${Math.round(value).toLocaleString()} ms` : '--' }
function formatDate(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '--' : date.toLocaleString([], { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

function readQuery() {
  const query = route.query
  range.value = ranges.includes(query.range as MonitoringRange) ? (query.range as MonitoringRange) : '24h'
  platform.value = typeof query.platform === 'string' ? query.platform : ''
  model.value = typeof query.model === 'string' ? query.model : ''
}

async function syncQuery() {
  const query: Record<string, string> = { range: range.value }
  if (platform.value) query.platform = platform.value
  if (model.value.trim()) query.model = model.value.trim()
  syncingRouteQuery = true
  try { await router.replace({ query }) } finally { syncingRouteQuery = false }
}

async function loadSettings() {
  settingsLoading.value = !settings.value
  settingsError.value = ''
  try { settings.value = await monitoringAPI.getSettings() } catch { settingsError.value = t('admin.monitoring.errors.settings') } finally { settingsLoading.value = false }
}

async function loadOverview() {
  const generation = ++overviewGeneration
  overviewLoading.value = !hasOverviewLoaded.value
  overviewError.value = ''
  try {
    const response = await monitoringAPI.getOverview(pageFilters.value)
    if (generation !== overviewGeneration) return
    overview.value = response
    hasOverviewLoaded.value = true
  } catch {
    if (generation !== overviewGeneration) return
    overviewError.value = t('admin.monitoring.errors.overview')
  } finally {
    if (generation === overviewGeneration) overviewLoading.value = false
  }
}

async function loadAccounts() {
  const generation = ++accountsGeneration
  accountsLoading.value = !hasAccountsLoaded.value
  accountsError.value = ''
  try {
    const response = await monitoringAPI.getAccounts({
      ...pageFilters.value,
      search: accountSearch.value.trim() || undefined,
      sort: accountSort.value,
      order: accountOrder.value,
      page: accountPage.value,
      page_size: accountPageSize
    })
    if (generation !== accountsGeneration) return
    accounts.value = response
    hasAccountsLoaded.value = true
  } catch {
    if (generation !== accountsGeneration) return
    accountsError.value = t('admin.monitoring.errors.accounts')
  } finally {
    if (generation === accountsGeneration) accountsLoading.value = false
  }
}

async function loadInvestigation() {
  if (!selectedAccount.value) return
  const generation = ++investigationGeneration
  investigationLoading.value = true
  investigationError.value = ''
  investigation.value = null
  try {
    const response = await monitoringAPI.getInvestigation({ ...pageFilters.value, account_id: selectedAccount.value.account_id })
    if (generation !== investigationGeneration) return
    investigation.value = response
  } catch {
    if (generation !== investigationGeneration) return
    investigationError.value = t('admin.monitoring.errors.investigation')
  } finally {
    if (generation === investigationGeneration) investigationLoading.value = false
  }
}

async function refreshAll() { await Promise.all([loadOverview(), loadAccounts()]) }

async function saveSettings(payload: FirstTokenTimeoutSettingsValue) {
  settingsSaving.value = true
  settingsError.value = ''
  try {
    settings.value = await monitoringAPI.updateSettings(payload)
    settingsOpen.value = false
  } catch {
    settingsError.value = t('admin.monitoring.errors.settings')
  } finally {
    settingsSaving.value = false
  }
}

async function changeGlobalFilters() {
  accountPage.value = 1
  closeInvestigation()
  await syncQuery()
  await refreshAll()
}

function changeAccountSort(sort: string) {
  if (accountSort.value === sort) accountOrder.value = accountOrder.value === 'asc' ? 'desc' : 'asc'
  else { accountSort.value = sort; accountOrder.value = 'asc' }
  accountPage.value = 1
  void loadAccounts()
}

function selectAccount(account: PerformanceAccountItem) {
  selectedAccount.value = account
  investigation.value = null
  investigationError.value = ''
  void loadInvestigation()
}

function closeInvestigation() {
  investigationGeneration++
  selectedAccount.value = null
  investigation.value = null
  investigationError.value = ''
  investigationLoading.value = false
}

watch(accountSearch, () => {
  if (accountSearchTimer) clearTimeout(accountSearchTimer)
  accountSearchTimer = setTimeout(() => {
    accountSearchTimer = undefined
    accountPage.value = 1
    void loadAccounts()
  }, 300)
})

watch(
  () => route.query,
  async () => {
    if (syncingRouteQuery) return
    readQuery()
    accountPage.value = 1
    closeInvestigation()
    await refreshAll()
  },
  { deep: true }
)

onMounted(async () => { readQuery(); await syncQuery(); await Promise.all([loadSettings(), refreshAll()]) })
onUnmounted(() => {
  if (accountSearchTimer) clearTimeout(accountSearchTimer)
  overviewGeneration++
  accountsGeneration++
  investigationGeneration++
})
</script>

<template>
  <AppLayout>
    <main class="mx-auto min-w-0 max-w-7xl space-y-6 px-4 py-6 sm:px-6">
      <header class="flex flex-col gap-4 border-b border-gray-200 pb-5 dark:border-dark-700 xl:flex-row xl:items-end xl:justify-between">
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.monitoring.title') }}</h1>
            <span
              class="inline-flex items-center rounded-md border px-2.5 py-1 text-xs font-medium"
              :class="degraded ? 'border-amber-300 bg-amber-50 text-amber-800 dark:border-amber-800 dark:bg-amber-500/10 dark:text-amber-200' : 'border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-800 dark:bg-emerald-500/10 dark:text-emerald-200'"
            >{{ degraded ? t('admin.monitoring.health.degraded') : t('admin.monitoring.health.complete') }}</span>
            <button
              v-if="settings"
              data-testid="protection-badge"
              type="button"
              class="inline-flex items-center gap-1 rounded-md border border-primary-300 bg-primary-50 px-2.5 py-1 text-xs font-medium text-primary-700 hover:bg-primary-100 dark:border-primary-800 dark:bg-primary-500/10 dark:text-primary-300 dark:hover:bg-primary-500/20"
              @click="settingsOpen = true"
            >{{ protectionLabel }} · {{ t('admin.monitoring.protection.adjust') }}</button>
          </div>
          <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.monitoring.description') }}</p>
          <p v-if="coverage" class="mt-1 text-xs text-gray-400 dark:text-gray-500">{{ coverage }}</p>
        </div>
        <div class="flex flex-col gap-2 sm:flex-row sm:items-end">
          <div class="flex flex-wrap rounded-md border border-gray-300 p-1 dark:border-dark-600" role="group" :aria-label="t('admin.monitoring.filters.range')">
            <button
              v-for="item in ranges"
              :key="item"
              type="button"
              class="h-8 rounded px-2.5 text-xs font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
              :class="range === item ? 'bg-primary-600 text-white' : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'"
              @click="range = item; changeGlobalFilters()"
            >{{ item }}</button>
          </div>
          <label class="grid gap-1 text-xs text-gray-500 dark:text-gray-400">
            <span>{{ t('admin.monitoring.filters.platform') }}</span>
            <Select v-model="platform" :options="platformOptions" class="sm:w-40" @change="changeGlobalFilters" />
          </label>
          <label class="grid gap-1 text-xs text-gray-500 dark:text-gray-400">
            <span>{{ t('admin.monitoring.filters.model') }}</span>
            <input v-model="model" :placeholder="t('admin.monitoring.filters.modelPlaceholder')" class="h-10 min-w-36 rounded-md border border-gray-300 bg-white px-3 text-sm text-gray-900 outline-none focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-600 dark:bg-dark-900 dark:text-white" @change="changeGlobalFilters" />
          </label>
          <button type="button" class="flex h-10 w-10 items-center justify-center rounded-md border border-gray-300 text-gray-600 hover:bg-gray-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:border-dark-600 dark:text-gray-300 dark:hover:bg-dark-700" :aria-label="t('common.refresh')" :title="t('common.refresh')" @click="refreshAll">
            <Icon name="refresh" size="md" aria-hidden="true" />
          </button>
        </div>
      </header>

      <aside v-if="degraded && degradedHealth" class="border-l-4 border-amber-500 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:bg-amber-500/10 dark:text-amber-200">
        {{ t('admin.monitoring.degradedBanner', { dropped: degradedHealth.dropped_samples, pending: degradedHealth.pending_samples }) }}
      </aside>

      <section v-if="overviewLoading && !hasOverviewLoaded" data-testid="monitoring-skeleton" class="grid grid-cols-2 gap-3 lg:grid-cols-3 xl:grid-cols-5" aria-label="loading">
        <div v-for="item in 5" :key="item" class="h-32 animate-pulse rounded-lg bg-gray-100 dark:bg-dark-800" />
      </section>
      <section v-else-if="overviewError && !overview" class="flex flex-wrap items-center gap-3 border-l-4 border-red-500 bg-red-50 px-4 py-3 text-sm text-red-800 dark:bg-red-500/10 dark:text-red-200">
        <span>{{ overviewError }}</span>
        <button data-testid="monitoring-overview-retry" type="button" class="min-h-9 rounded-md border border-current px-3 py-1.5 font-medium" @click="loadOverview">{{ t('common.refresh') }}</button>
      </section>
      <template v-else-if="overview">
        <EmptyState v-if="!hasSamples" :title="t('admin.monitoring.empty.title')" :description="t('admin.monitoring.empty.description')" />
        <template v-else>
          <section class="grid grid-cols-2 gap-3 lg:grid-cols-3 xl:grid-cols-5" :aria-label="t('admin.monitoring.title')">
            <MetricTrendCard v-for="card in kpiCards" :key="card.label" v-bind="card" />
          </section>
          <ProtectionFunnel :summary="overview.ttft.summary" />
          <section class="grid grid-cols-1 gap-6 xl:grid-cols-2" :aria-label="t('admin.monitoring.trends.rates')">
            <MonitoringTrendChart :title="t('admin.monitoring.trends.rates')" :points="overview.performance.trend" :time-range="range" :series="ratesSeries" :loading="overviewLoading" value-format="percent" />
            <MonitoringTrendChart :title="t('admin.monitoring.trends.latency')" :points="overview.performance.trend" :time-range="range" :series="latencySeries" :loading="overviewLoading" />
          </section>
        </template>
      </template>

      <AccountHealthTable
        :page="accounts"
        :loading="accountsLoading"
        :error="accountsError"
        :sort="accountSort"
        :order="accountOrder"
        :search="accountSearch"
        @update:search="(value: string) => (accountSearch = value)"
        @retry="loadAccounts"
        @sort="changeAccountSort"
        @page="(value: number) => { accountPage = value; loadAccounts() }"
        @select="selectAccount"
      />

      <FailureDistribution v-if="overview && hasSamples" :failures="failureItems" :title="t('admin.monitoring.failures.title')" :loading="overviewLoading" />
    </main>
    <InvestigationDrawer :open="Boolean(selectedAccount)" :account="selectedAccount" :investigation="investigation" :loading="investigationLoading" :error="investigationError" @close="closeInvestigation" @retry="loadInvestigation" />
    <TTFTSettingsDialog :open="settingsOpen" :settings="settings" :saving="settingsSaving" :error="settingsError" @close="settingsOpen = false" @save="saveSettings" />
  </AppLayout>
</template>
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd frontend && npm run test:run -- src/views/admin/monitoring && npm run typecheck`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/views/admin/monitoring
git commit -m "feat(monitoring): add unified monitoring center view"
```

---

### Task 13: 前端 — 路由与侧边栏

**Files:**
- Modify: `frontend/src/router/index.ts`（替换 `/admin/ttft`、`/admin/performance` 两个路由块）
- Modify: `frontend/src/components/layout/AppSidebar.vue`（777-778 行）
- Test: `frontend/src/views/admin/monitoring/__tests__/monitoringRoutes.spec.ts`

**Interfaces:**
- Consumes: Task 12 的 `MonitoringView`
- Produces: `/admin/monitoring` 路由；`/admin/ttft`、`/admin/performance` → 重定向（保留 query）

- [ ] **Step 1: 写失败测试**

```ts
import { describe, expect, it } from 'vitest'
import router from '@/router'

// router/index.ts 只默认导出 router 实例（routes 数组未导出），
// 用 getRoutes() 与 resolve() 断言，不触发导航（避免路由守卫依赖）。

describe('monitoring routes', () => {
  it('registers /admin/monitoring', () => {
    expect(router.getRoutes().some((route) => route.path === '/admin/monitoring')).toBe(true)
  })

  it.each(['/admin/ttft', '/admin/performance'])('redirects %s to /admin/monitoring preserving query', (path) => {
    const resolved = router.resolve({ path, query: { range: '7d' } })
    expect(resolved.path).toBe('/admin/monitoring')
    expect(resolved.query.range).toBe('7d')
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd frontend && npm run test:run -- src/views/admin/monitoring/__tests__/monitoringRoutes.spec.ts`
Expected: FAIL

- [ ] **Step 3: 修改路由**

`router/index.ts`：删除 `/admin/ttft` 与 `/admin/performance` 两个路由块，替换为：

```ts
  {
    path: '/admin/monitoring',
    name: 'AdminMonitoring',
    component: () => import('@/views/admin/monitoring/MonitoringView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Monitoring',
      titleKey: 'admin.monitoring.title',
      descriptionKey: 'admin.monitoring.description'
    }
  },
  {
    path: '/admin/ttft',
    redirect: (to) => ({ path: '/admin/monitoring', query: to.query })
  },
  {
    path: '/admin/performance',
    redirect: (to) => ({ path: '/admin/monitoring', query: to.query })
  },
```

- [ ] **Step 4: 修改侧边栏**

`AppSidebar.vue` 777-778 两行替换为：

```ts
    { path: '/admin/monitoring', label: t('nav.monitoring'), icon: ChartIcon },
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd frontend && npm run test:run -- src/views/admin/monitoring/__tests__/monitoringRoutes.spec.ts && npm run typecheck`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add frontend/src/router/index.ts frontend/src/components/layout/AppSidebar.vue frontend/src/views/admin/monitoring/__tests__/monitoringRoutes.spec.ts
git commit -m "feat(monitoring): route monitoring center and redirect legacy pages"
```

---

### Task 14: 清理 — 删除旧页面与 locale，全量验证

**Files:**
- Delete: `frontend/src/views/admin/ttft/`、`frontend/src/views/admin/performance/`（整目录）
- Delete: `frontend/src/api/admin/ttft.ts`、`frontend/src/api/__tests__/admin.ttft.spec.ts`
- Delete: `frontend/src/i18n/locales/zh/admin/ttft.ts`、`frontend/src/i18n/locales/en/admin/ttft.ts`
- Modify: `frontend/src/i18n/locales/{zh,en}/admin/index.ts`（移除 ttft import 与 spread）
- Modify: `frontend/src/i18n/locales/{zh,en}/common.ts`（移除 `ttftMonitoring`、`accountPerformance` 两个 nav key）

- [ ] **Step 1: 残留引用检查（删除前）**

Run: `cd frontend && grep -rn "admin/ttft\|admin/performance\|admin\.ttft\.\|ttftMonitoring\|accountPerformance" src --include="*.vue" --include="*.ts" | grep -v "src/views/admin/monitoring" | grep -v "api/admin/performance.ts"`
Expected: 仅列出即将删除/修改的文件自身；若出现其它引用（如 `UsageView.vue`、`OpsOpenAITokenStatsCard.vue` 引用了 ttft client 的类型或 `admin.ttft.*` key），先将这些引用迁移到 `@/api/admin/monitoring` 或相应新 key，再继续

- [ ] **Step 2: 执行删除与 locale 清理**

```bash
git rm -r frontend/src/views/admin/ttft frontend/src/views/admin/performance
git rm frontend/src/api/admin/ttft.ts frontend/src/api/__tests__/admin.ttft.spec.ts
git rm frontend/src/i18n/locales/zh/admin/ttft.ts frontend/src/i18n/locales/en/admin/ttft.ts
```

两个 `admin/index.ts` 移除 `import ttft from './ttft'` 与 `...ttft,`；两个 `common.ts` 移除 `ttftMonitoring` 与 `accountPerformance` 行。

- [ ] **Step 3: 全量前端验证**

Run: `cd frontend && npm run test:run && npm run typecheck && npm run lint:check && npm run build`
Expected: 全部 PASS；build 成功

- [ ] **Step 4: 全量后端验证**

Run: `cd backend && go build ./... && go test ./internal/handler/... ./internal/repository/... ./internal/service/... -count=1`
Expected: PASS

- [ ] **Step 5: 手动冒烟（可选但推荐）**

`cd backend && go run ./cmd/server` + `cd frontend && npm run dev`，用 admin 账号验证：
1. `/admin/monitoring` 正常渲染五张 KPI 卡、漏斗、两张趋势图、账号表、失败分布
2. `/admin/ttft`、`/admin/performance` 重定向且 query 保留
3. 调整 TTFT 超时设置弹窗保存成功
4. 切换 1h/6h 范围无 400 错误
5. 账号表搜索、排序、行点击钻取正常

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor(monitoring): remove legacy ttft and performance pages"
```

---

## Self-Review 记录

- **Spec 覆盖**：聚合端点→Task 3；时间范围扩展→Task 1；accounts search→Task 2；TTFT 设置弹窗→Task 10；漏斗→Task 8；KPI 卡→Task 6；趋势/失败分布→Task 7；账号表→Task 9；钻取抽屉→Task 11；页面与空态/错误处理→Task 12；路由/导航/重定向→Task 13；i18n→Task 5；旧代码删除→Task 14。spec 中「采集健康」区块合并进头部徽章与横幅（Task 12）。
- **类型一致性**：`MonitoringTTFTSummary`（Task 4）↔ ProtectionFunnel props（Task 8）↔ View（Task 12）一致；`PerformanceSeriesDefinition` 保持原名导出供 View 与 Drawer 使用。
- **已知取舍**：后端 handler 快乐路径依赖既有 sqlmock 体系过重，Task 3 仅覆盖参数校验与降级路径，快乐路径由 Task 14 手动冒烟兜底；TTFT 漏斗不随平台筛选变化（后端不支持），UI 上有 `funnel.platformNote` 说明。
