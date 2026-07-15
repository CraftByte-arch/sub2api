package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFirstTokenTimeoutSettingsAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settingRepo := newFirstTokenTimeoutHandlerSettingRepo()
	policy := service.NewFirstTokenTimeoutPolicy(settingRepo, nil)
	h := NewFirstTokenTimeoutHandler(policy, &firstTokenTimeoutHandlerStatsRepo{}, nil)
	router := newFirstTokenTimeoutHandlerRouter(h)

	t.Run("default and enabled settings are readable", func(t *testing.T) {
		response := firstTokenTimeoutHandlerRequest(t, router, http.MethodGet, "/settings", "")
		require.Equal(t, http.StatusOK, response.Code)
		data := firstTokenTimeoutHandlerData(t, response)
		require.Equal(t, map[string]any{"enabled": false, "timeout_seconds": float64(30)}, data["saved"])
		require.Equal(t, map[string]any{"enabled": false, "timeout_seconds": float64(30)}, data["effective"])
		require.NotEmpty(t, data["loaded_at"])

		response = firstTokenTimeoutHandlerRequest(t, router, http.MethodPut, "/settings", `{"enabled":true,"timeout_seconds":12}`)
		require.Equal(t, http.StatusOK, response.Code)
		data = firstTokenTimeoutHandlerData(t, response)
		require.Equal(t, map[string]any{"enabled": true, "timeout_seconds": float64(12)}, data["saved"])
		require.Equal(t, map[string]any{"enabled": true, "timeout_seconds": float64(12)}, data["effective"])
		require.NotEmpty(t, data["loaded_at"])

		response = firstTokenTimeoutHandlerRequest(t, router, http.MethodGet, "/settings", "")
		require.Equal(t, http.StatusOK, response.Code)
		data = firstTokenTimeoutHandlerData(t, response)
		require.Equal(t, map[string]any{"enabled": true, "timeout_seconds": float64(12)}, data["saved"])
	})

	t.Run("disabled settings retain the saved timeout", func(t *testing.T) {
		response := firstTokenTimeoutHandlerRequest(t, router, http.MethodPut, "/settings", `{"enabled":false,"timeout_seconds":12}`)
		require.Equal(t, http.StatusOK, response.Code)
		data := firstTokenTimeoutHandlerData(t, response)
		require.Equal(t, map[string]any{"enabled": false, "timeout_seconds": float64(12)}, data["saved"])
		require.Equal(t, map[string]any{"enabled": false, "timeout_seconds": float64(12)}, data["effective"])
	})

	t.Run("invalid payloads are rejected", func(t *testing.T) {
		tests := []struct {
			name string
			body string
		}{
			{name: "zero timeout", body: `{"enabled":true,"timeout_seconds":0}`},
			{name: "timeout above maximum", body: `{"enabled":true,"timeout_seconds":301}`},
			{name: "non integer timeout", body: `{"enabled":true,"timeout_seconds":1.5}`},
			{name: "non bool enabled", body: `{"enabled":"true","timeout_seconds":12}`},
			{name: "numeric enabled", body: `{"enabled":1,"timeout_seconds":12}`},
			{name: "bool timeout", body: `{"enabled":true,"timeout_seconds":true}`},
			{name: "null enabled", body: `{"enabled":null,"timeout_seconds":12}`},
			{name: "null timeout", body: `{"enabled":true,"timeout_seconds":null}`},
			{name: "missing enabled", body: `{"timeout_seconds":12}`},
			{name: "missing timeout", body: `{"enabled":true}`},
			{name: "unknown field", body: `{"enabled":true,"timeout_seconds":12,"internal":true}`},
			{name: "trailing json", body: `{"enabled":true,"timeout_seconds":12} {}`},
			{name: "enabled case variant", body: `{"Enabled":true,"timeout_seconds":12}`},
			{name: "timeout case variant", body: `{"enabled":true,"Timeout_Seconds":12}`},
			{name: "duplicate enabled", body: `{"enabled":true,"enabled":false,"timeout_seconds":12}`},
			{name: "duplicate timeout", body: `{"enabled":true,"timeout_seconds":12,"timeout_seconds":13}`},
		}
		for _, testCase := range tests {
			t.Run(testCase.name, func(t *testing.T) {
				response := firstTokenTimeoutHandlerRequest(t, router, http.MethodPut, "/settings", testCase.body)
				require.Equal(t, http.StatusBadRequest, response.Code, "body=%s response=%s", testCase.body, response.Body.Body.String())
				require.NotContains(t, response.Body.Body.String(), "database")
				require.NotContains(t, response.Body.Body.String(), "redis")
				require.NotContains(t, response.Body.Body.String(), "sql")
			})
		}
	})
}

func TestFirstTokenTimeoutUpdateSettingsHidesPolicyErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settingRepo := newFirstTokenTimeoutHandlerSettingRepo()
	settingRepo.setErr = errors.New("database dsn postgres://secret")
	policy := service.NewFirstTokenTimeoutPolicy(settingRepo, nil)
	router := newFirstTokenTimeoutHandlerRouter(NewFirstTokenTimeoutHandler(policy, &firstTokenTimeoutHandlerStatsRepo{}, nil))

	response := firstTokenTimeoutHandlerRequest(t, router, http.MethodPut, "/settings", `{"enabled":true,"timeout_seconds":12}`)
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.NotContains(t, response.Body.Body.String(), "database")
	require.NotContains(t, response.Body.Body.String(), "postgres")
	require.NotContains(t, response.Body.Body.String(), "secret")
}

func TestFirstTokenTimeoutSettingsRejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	policy := service.NewFirstTokenTimeoutPolicy(newFirstTokenTimeoutHandlerSettingRepo(), nil)
	router := newFirstTokenTimeoutHandlerRouter(NewFirstTokenTimeoutHandler(policy, &firstTokenTimeoutHandlerStatsRepo{}, nil))

	body := `{"enabled":true,"timeout_seconds":` + strings.Repeat("1", 4*1024) + `}`
	response := firstTokenTimeoutHandlerRequest(t, router, http.MethodPut, "/settings", body)
	require.Equal(t, http.StatusRequestEntityTooLarge, response.Code, response.Body.Body.String())
}

func TestFirstTokenTimeoutOverviewAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	bucket := time.Date(2026, 7, 15, 3, 0, 0, 0, time.UTC)
	statsRepo := &firstTokenTimeoutHandlerStatsRepo{
		overview: &service.FirstTokenStatsOverview{
			Summary: service.FirstTokenStatsSummary{
				ControlledRequests:     9,
				ClientCanceledRequests: 2,
				AttemptTTFTTimeoutRate: service.FirstTokenStatsRatio{
					Numerator: 2, Denominator: 8, Rate: 0.25,
				},
				RecoveryRate: service.FirstTokenStatsRatio{
					Numerator: 1, Denominator: 2, Rate: 0.5,
				},
				FinalTTFTFailureRate: service.FirstTokenStatsRatio{
					Numerator: 0, Denominator: 0, Rate: math.NaN(),
				},
				OtherFinalFailureRate: service.FirstTokenStatsRatio{
					Numerator: 1, Denominator: 9, Rate: 1.0 / 9.0,
				},
			},
			Trend: []service.FirstTokenStatsTrendPoint{{
				BucketStart: bucket,
				AttemptTTFTTimeoutRate: service.FirstTokenStatsRatio{
					Numerator: 1, Denominator: 4, Rate: 0.25,
				},
				RecoveryRate: service.FirstTokenStatsRatio{
					Numerator: 0, Denominator: 0, Rate: math.Inf(1),
				},
				FinalTTFTFailureRate:  service.FirstTokenStatsRatio{Numerator: 0, Denominator: 4, Rate: 0},
				OtherFinalFailureRate: service.FirstTokenStatsRatio{Numerator: 1, Denominator: 4, Rate: 0.25},
			}},
			OtherFailures: []service.FirstTokenStatsFailureDistribution{{
				FailureKind: service.FirstTokenStatsFailureTransport,
				SampleCount: 3,
			}},
		},
	}
	recorder := service.NewFirstTokenTimeoutStatsRecorder(statsRepo)
	recorder.Record(service.FirstTokenStatsDelta{SampleCount: 3})
	h := NewFirstTokenTimeoutHandler(service.NewFirstTokenTimeoutPolicy(newFirstTokenTimeoutHandlerSettingRepo(), nil), statsRepo, recorder)
	router := newFirstTokenTimeoutHandlerRouter(h)

	response := firstTokenTimeoutHandlerRequest(t, router, http.MethodGet, "/overview", "")
	require.Equal(t, http.StatusOK, response.Code)
	filter := statsRepo.lastOverviewFilter()
	require.Equal(t, service.FirstTokenStatsRange24Hours, filter.Range)
	require.Empty(t, filter.Protocol)
	require.Empty(t, filter.Model)
	require.False(t, filter.End.IsZero())

	data := firstTokenTimeoutHandlerData(t, response)
	summary := data["summary"].(map[string]any)
	require.Equal(t, float64(9), summary["controlled_requests"])
	require.Equal(t, float64(2), summary["client_canceled_requests"])
	require.Equal(t, map[string]any{"numerator": float64(2), "denominator": float64(8), "rate": 0.25}, summary["attempt_ttft_timeout_rate"])
	require.Equal(t, map[string]any{"numerator": float64(0), "denominator": float64(0), "rate": float64(0)}, summary["final_ttft_failure_rate"])
	trend := data["trend"].([]any)
	require.Len(t, trend, 1)
	require.Equal(t, map[string]any{"numerator": float64(0), "denominator": float64(0), "rate": float64(0)}, trend[0].(map[string]any)["recovery_rate"])
	require.Equal(t, []any{map[string]any{"failure_kind": service.FirstTokenStatsFailureTransport, "sample_count": float64(3)}}, data["other_failures"])
	completeness := data["completeness"].(map[string]any)
	require.Equal(t, service.FirstTokenStatsCompletenessDegraded, completeness["status"])
	require.Equal(t, float64(3), completeness["dropped_samples"])
	require.Contains(t, completeness, "last_successful_flush_at")
}

func TestFirstTokenTimeoutOverviewValidatesAndForwardsFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	statsRepo := &firstTokenTimeoutHandlerStatsRepo{overview: &service.FirstTokenStatsOverview{}}
	router := newFirstTokenTimeoutHandlerRouter(NewFirstTokenTimeoutHandler(
		service.NewFirstTokenTimeoutPolicy(newFirstTokenTimeoutHandlerSettingRepo(), nil), statsRepo, nil,
	))

	allowed := []struct {
		rangeValue string
		wantRange  service.FirstTokenStatsRange
		protocol   service.FirstTokenProtocol
	}{
		{rangeValue: "24h", wantRange: service.FirstTokenStatsRange24Hours, protocol: service.ProtocolResponses},
		{rangeValue: "7d", wantRange: service.FirstTokenStatsRange7Days, protocol: service.ProtocolChatCompletions},
		{rangeValue: "30d", wantRange: service.FirstTokenStatsRange30Days, protocol: service.ProtocolAnthropicMessages},
		{rangeValue: "90d", wantRange: service.FirstTokenStatsRange90Days, protocol: service.ProtocolResponses},
	}
	for _, testCase := range allowed {
		path := "/overview?range=" + testCase.rangeValue + "&protocol=" + string(testCase.protocol) + "&model=gpt-5&timezone=Asia%2FShanghai"
		response := firstTokenTimeoutHandlerRequest(t, router, http.MethodGet, path, "")
		require.Equal(t, http.StatusOK, response.Code)
		filter := statsRepo.lastOverviewFilter()
		require.Equal(t, testCase.wantRange, filter.Range)
		require.Equal(t, string(testCase.protocol), filter.Protocol)
		require.Equal(t, "gpt-5", filter.Model)
	}

	invalid := []string{
		"/overview?range=1h",
		"/overview?range=24h&range=90d",
		"/overview?model=&model=gpt-5",
		"/overview?protocol=openai",
		"/overview?account=42",
		"/overview?account_id=42",
		"/overview?platform=openai",
		"/overview?platfrom=openai",
		"/overview?timezone=Asia%2FShanghai&timezone=UTC",
	}
	for _, path := range invalid {
		response := firstTokenTimeoutHandlerRequest(t, router, http.MethodGet, path, "")
		require.Equal(t, http.StatusBadRequest, response.Code, "path=%s response=%s", path, response.Body.Body.String())
	}
}

func TestFirstTokenTimeoutOverviewHidesRepositoryErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	statsRepo := &firstTokenTimeoutHandlerStatsRepo{overviewErr: errors.New("sql SELECT secret")}
	router := newFirstTokenTimeoutHandlerRouter(NewFirstTokenTimeoutHandler(
		service.NewFirstTokenTimeoutPolicy(newFirstTokenTimeoutHandlerSettingRepo(), nil), statsRepo, nil,
	))

	response := firstTokenTimeoutHandlerRequest(t, router, http.MethodGet, "/overview", "")
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.NotContains(t, response.Body.Body.String(), "sql")
	require.NotContains(t, response.Body.Body.String(), "SELECT")
	require.NotContains(t, response.Body.Body.String(), "secret")
}

func TestFirstTokenTimeoutStatsRejectsUnsafeFilterBoundaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	statsRepo := &firstTokenTimeoutHandlerStatsRepo{
		overviewErr: errors.New("repository should not receive invalid overview filters"),
		accountsErr: errors.New("repository should not receive invalid account filters"),
	}
	router := newFirstTokenTimeoutHandlerRouter(NewFirstTokenTimeoutHandler(
		service.NewFirstTokenTimeoutPolicy(newFirstTokenTimeoutHandlerSettingRepo(), nil), statsRepo, nil,
	))

	tests := []struct {
		name   string
		path   string
		values url.Values
	}{
		{
			name:   "overview model exceeds repository limit",
			path:   "/overview",
			values: url.Values{"model": {strings.Repeat("m", 256)}},
		},
		{
			name:   "overview model contains NUL",
			path:   "/overview",
			values: url.Values{"model": {"gpt\x00-5"}},
		},
		{
			name:   "accounts model exceeds repository limit",
			path:   "/accounts",
			values: url.Values{"model": {strings.Repeat("m", 256)}},
		},
		{
			name:   "accounts platform exceeds repository limit",
			path:   "/accounts",
			values: url.Values{"platform": {strings.Repeat("p", 33)}},
		},
		{
			name:   "accounts search exceeds repository limit",
			path:   "/accounts",
			values: url.Values{"search": {strings.Repeat("s", 256)}},
		},
		{
			name:   "accounts model contains NUL",
			path:   "/accounts",
			values: url.Values{"model": {"gpt\x00-5"}},
		},
		{
			name:   "accounts platform contains NUL",
			path:   "/accounts",
			values: url.Values{"platform": {"open\x00ai"}},
		},
		{
			name:   "accounts search contains NUL",
			path:   "/accounts",
			values: url.Values{"search": {"alice\x00bob"}},
		},
		{
			name: "accounts page overflows SQL offset",
			path: "/accounts",
			values: url.Values{
				"page":      {strconv.FormatInt(1<<63-1, 10)},
				"page_size": {"100"},
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			response := firstTokenTimeoutHandlerRequest(t, router, http.MethodGet, testCase.path+"?"+testCase.values.Encode(), "")
			require.Equal(t, http.StatusBadRequest, response.Code, "response=%s", response.Body.Body.String())
		})
	}
}

func TestFirstTokenTimeoutAccountsAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	statsRepo := &firstTokenTimeoutHandlerStatsRepo{
		accounts: &service.FirstTokenStatsAccountPage{
			Items: []service.FirstTokenStatsAccount{{
				AccountID:         42,
				AccountName:       "Alice",
				Platform:          "openai",
				Samples:           20,
				SuccessCount:      16,
				TTFTTimeoutCount:  2,
				TTFTTimeoutRate:   service.FirstTokenStatsRatio{Numerator: 2, Denominator: 20, Rate: 0.1},
				OtherFailureCount: 2,
				OtherFailureRate:  service.FirstTokenStatsRatio{Numerator: 2, Denominator: 20, Rate: 0.1},
				AvgTTFTMS:         125.5,
				LowSample:         false,
			}},
			Total: 101, Page: 2, PageSize: 50, Pages: 3,
		},
	}
	router := newFirstTokenTimeoutHandlerRouter(NewFirstTokenTimeoutHandler(
		service.NewFirstTokenTimeoutPolicy(newFirstTokenTimeoutHandlerSettingRepo(), nil), statsRepo, nil,
	))

	path := "/accounts?range=7d&protocol=chat_completions&model=gpt-5&platform=openai&account_id=42&search=alice&sort=ttft_timeout_rate&order=asc&page=2&page_size=50&timezone=Asia%2FShanghai"
	response := firstTokenTimeoutHandlerRequest(t, router, http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, response.Code)
	filter := statsRepo.lastAccountFilter()
	require.Equal(t, service.FirstTokenStatsRange7Days, filter.Range)
	require.Equal(t, string(service.ProtocolChatCompletions), filter.Protocol)
	require.Equal(t, "gpt-5", filter.Model)
	require.Equal(t, "openai", filter.Platform)
	require.Equal(t, int64(42), filter.AccountID)
	require.Equal(t, "alice", filter.Search)
	require.Equal(t, service.FirstTokenStatsAccountSortTTFTTimeoutRate, filter.SortBy)
	require.Equal(t, "asc", filter.SortOrder)
	require.Equal(t, 2, filter.Page)
	require.Equal(t, 50, filter.PageSize)

	data := firstTokenTimeoutHandlerData(t, response)
	require.Equal(t, float64(101), data["total"])
	require.Equal(t, float64(2), data["page"])
	require.Equal(t, float64(50), data["page_size"])
	items := data["items"].([]any)
	require.Len(t, items, 1)
	require.Equal(t, map[string]any{"numerator": float64(2), "denominator": float64(20), "rate": 0.1}, items[0].(map[string]any)["ttft_timeout_rate"])
}

func TestFirstTokenTimeoutAccountsDefaultsAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	statsRepo := &firstTokenTimeoutHandlerStatsRepo{accounts: &service.FirstTokenStatsAccountPage{}}
	router := newFirstTokenTimeoutHandlerRouter(NewFirstTokenTimeoutHandler(
		service.NewFirstTokenTimeoutPolicy(newFirstTokenTimeoutHandlerSettingRepo(), nil), statsRepo, nil,
	))

	response := firstTokenTimeoutHandlerRequest(t, router, http.MethodGet, "/accounts", "")
	require.Equal(t, http.StatusOK, response.Code)
	filter := statsRepo.lastAccountFilter()
	require.Equal(t, service.FirstTokenStatsRange24Hours, filter.Range)
	require.Equal(t, service.FirstTokenStatsAccountSortSamples, filter.SortBy)
	require.Equal(t, "desc", filter.SortOrder)
	require.Equal(t, 1, filter.Page)
	require.Equal(t, 20, filter.PageSize)

	invalid := []string{
		"/accounts?range=1h",
		"/accounts?range=24h&range=90d",
		"/accounts?search=alice&search=bob",
		"/accounts?protocol=openai",
		"/accounts?sort=unknown",
		"/accounts?order=sideways",
		"/accounts?page=0",
		"/accounts?page=-1",
		"/accounts?page_size=0",
		"/accounts?page_size=25",
		"/accounts?page_size=101",
		"/accounts?account_id=0",
		"/accounts?account_id=-1",
		"/accounts?timezone=Asia%2FShanghai&timezone=UTC",
		"/accounts?account_id=abc",
		"/accounts?account=42",
		"/accounts?serach=alice",
	}
	for _, path := range invalid {
		response = firstTokenTimeoutHandlerRequest(t, router, http.MethodGet, path, "")
		require.Equal(t, http.StatusBadRequest, response.Code, "path=%s response=%s", path, response.Body.Body.String())
	}
}

func TestFirstTokenTimeoutAccountsHidesRepositoryErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	statsRepo := &firstTokenTimeoutHandlerStatsRepo{accountsErr: errors.New("redis or SQL secret")}
	router := newFirstTokenTimeoutHandlerRouter(NewFirstTokenTimeoutHandler(
		service.NewFirstTokenTimeoutPolicy(newFirstTokenTimeoutHandlerSettingRepo(), nil), statsRepo, nil,
	))

	response := firstTokenTimeoutHandlerRequest(t, router, http.MethodGet, "/accounts", "")
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.NotContains(t, response.Body.Body.String(), "redis")
	require.NotContains(t, response.Body.Body.String(), "SQL")
	require.NotContains(t, response.Body.Body.String(), "secret")
}

func newFirstTokenTimeoutHandlerRouter(h *FirstTokenTimeoutHandler) *gin.Engine {
	router := gin.New()
	router.GET("/settings", h.GetSettings)
	router.PUT("/settings", h.UpdateSettings)
	router.GET("/overview", h.GetOverview)
	router.GET("/accounts", h.GetAccounts)
	return router
}

type firstTokenTimeoutHandlerHTTPResponse struct {
	Code int
	Body *httptest.ResponseRecorder
}

func firstTokenTimeoutHandlerRequest(t *testing.T, router http.Handler, method, path, body string) firstTokenTimeoutHandlerHTTPResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(recorder, request)
	return firstTokenTimeoutHandlerHTTPResponse{Code: recorder.Code, Body: recorder}
}

func firstTokenTimeoutHandlerData(t *testing.T, response firstTokenTimeoutHandlerHTTPResponse) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Body.Bytes(), &envelope))
	require.NotNil(t, envelope.Data)
	return envelope.Data
}

type firstTokenTimeoutHandlerSettingRepo struct {
	mu     sync.RWMutex
	values map[string]string
	setErr error
}

func newFirstTokenTimeoutHandlerSettingRepo() *firstTokenTimeoutHandlerSettingRepo {
	return &firstTokenTimeoutHandlerSettingRepo{values: make(map[string]string)}
}

func (r *firstTokenTimeoutHandlerSettingRepo) Get(_ context.Context, key string) (*service.Setting, error) {
	value, err := r.GetValue(context.Background(), key)
	if err != nil {
		return nil, err
	}
	return &service.Setting{Key: key, Value: value}, nil
}

func (r *firstTokenTimeoutHandlerSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func (r *firstTokenTimeoutHandlerSettingRepo) Set(_ context.Context, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.setErr != nil {
		return r.setErr
	}
	r.values[key] = value
	return nil
}

func (r *firstTokenTimeoutHandlerSettingRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		value, err := r.GetValue(ctx, key)
		if err == nil {
			values[key] = value
		}
	}
	return values, nil
}

func (r *firstTokenTimeoutHandlerSettingRepo) SetMultiple(ctx context.Context, values map[string]string) error {
	for key, value := range values {
		if err := r.Set(ctx, key, value); err != nil {
			return err
		}
	}
	return nil
}

func (r *firstTokenTimeoutHandlerSettingRepo) GetAll(_ context.Context) (map[string]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := make(map[string]string, len(r.values))
	for key, value := range r.values {
		values[key] = value
	}
	return values, nil
}

func (r *firstTokenTimeoutHandlerSettingRepo) Delete(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.values, key)
	return nil
}

type firstTokenTimeoutHandlerStatsRepo struct {
	mu             sync.Mutex
	overview       *service.FirstTokenStatsOverview
	overviewErr    error
	accounts       *service.FirstTokenStatsAccountPage
	accountsErr    error
	overviewFilter service.FirstTokenStatsOverviewFilter
	accountFilter  service.FirstTokenStatsAccountFilter
}

func (r *firstTokenTimeoutHandlerStatsRepo) UpsertBatch(context.Context, []service.FirstTokenStatsDelta) error {
	return nil
}

func (r *firstTokenTimeoutHandlerStatsRepo) QueryOverview(_ context.Context, filter service.FirstTokenStatsOverviewFilter) (*service.FirstTokenStatsOverview, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.overviewFilter = filter
	return r.overview, r.overviewErr
}

func (r *firstTokenTimeoutHandlerStatsRepo) QueryAccounts(_ context.Context, filter service.FirstTokenStatsAccountFilter) (*service.FirstTokenStatsAccountPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accountFilter = filter
	return r.accounts, r.accountsErr
}

func (r *firstTokenTimeoutHandlerStatsRepo) DeleteBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (r *firstTokenTimeoutHandlerStatsRepo) lastOverviewFilter() service.FirstTokenStatsOverviewFilter {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.overviewFilter
}

func (r *firstTokenTimeoutHandlerStatsRepo) lastAccountFilter() service.FirstTokenStatsAccountFilter {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.accountFilter
}
