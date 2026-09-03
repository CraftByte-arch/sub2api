package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

type fakeAccountCacheStatsConsole struct {
	snapshot model.AccountCacheStatsSnapshot
	err      error
	calls    int
}

func (f *fakeAccountCacheStatsConsole) GetTodayAccountCacheStats(context.Context, []int64) (model.AccountCacheStatsSnapshot, error) {
	f.calls++
	return f.snapshot, f.err
}

func TestOverviewUsesConfiguredBulkCacheStatsWithoutHTTPFallback(t *testing.T) {
	backend := cacheStatsOverviewBackend()
	bulk := &fakeAccountCacheStatsConsole{snapshot: model.AccountCacheStatsSnapshot{
		Configured: true,
		Ready:      true,
		QueriedAt:  time.Now().UTC(),
		Stats: map[int64]model.AccountCacheStats{
			1: {InputTokens: 600, CacheCreationTokens: 300, CacheReadTokens: 600, PromptTokens: 1500, HitRate: 40},
			2: {},
		},
	}}
	server := newConsoleTestServer(t, backend, nil)
	server.accountCacheStats = bulk

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodGet, "/api/overview", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if bulk.calls != 1 || backend.cacheFallbackCalls != 0 {
		t.Fatalf("configured bulk path used unexpected fallback: bulk=%d fallback=%d", bulk.calls, backend.cacheFallbackCalls)
	}
	accounts := decodeOverviewCacheAccounts(t, response)
	if accounts[1].Cache == nil || accounts[1].Cache.HitRate != 40 || accounts[2].Cache == nil || accounts[2].Cache.PromptTokens != 0 {
		t.Fatalf("bulk cache projection missing: %#v", accounts)
	}
}

func TestOverviewDoesNotFallbackAfterConfiguredCacheDatabaseFailure(t *testing.T) {
	backend := cacheStatsOverviewBackend()
	backend.cacheFallback = func(_ context.Context, _ []int64, stats map[string]model.WindowStats) {
		usage := stats["1"]
		usage.Cache = &model.AccountCacheStats{PromptTokens: 123, HitRate: 99}
		stats["1"] = usage
	}
	bulk := &fakeAccountCacheStatsConsole{snapshot: model.AccountCacheStatsSnapshot{Configured: true, Ready: false}}
	server := newConsoleTestServer(t, backend, nil)
	server.accountCacheStats = bulk

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodGet, "/api/overview", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	accounts := decodeOverviewCacheAccounts(t, response)
	if bulk.calls != 1 || backend.cacheFallbackCalls != 0 || accounts[1].Cache != nil {
		t.Fatalf("configured failure recreated the N+1 fallback: bulk=%d fallback=%d accounts=%#v", bulk.calls, backend.cacheFallbackCalls, accounts)
	}
}

func TestOverviewUsesLegacyFallbackOnlyWhenDatabaseIsUnconfigured(t *testing.T) {
	backend := cacheStatsOverviewBackend()
	backend.cacheFallback = func(_ context.Context, _ []int64, stats map[string]model.WindowStats) {
		usage := stats["1"]
		usage.Cache = &model.AccountCacheStats{InputTokens: 10, CacheReadTokens: 10, PromptTokens: 20, HitRate: 50}
		stats["1"] = usage
	}
	bulk := &fakeAccountCacheStatsConsole{snapshot: model.AccountCacheStatsSnapshot{Configured: false, Ready: false}}
	server := newConsoleTestServer(t, backend, nil)
	server.accountCacheStats = bulk

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodGet, "/api/overview", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	accounts := decodeOverviewCacheAccounts(t, response)
	if bulk.calls != 1 || backend.cacheFallbackCalls != 1 || accounts[1].Cache == nil || accounts[1].Cache.HitRate != 50 {
		t.Fatalf("unconfigured database did not retain legacy compatibility: bulk=%d fallback=%d accounts=%#v", bulk.calls, backend.cacheFallbackCalls, accounts)
	}
}

func cacheStatsOverviewBackend() *fakeConsoleCore {
	return &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		groups:        []model.UpstreamGroup{{ID: 7, Name: "Primary"}},
		accounts: []model.UpstreamAccount{
			{ID: 1, Name: "one", Platform: "openai", Type: "apikey", Status: "active", Schedulable: true, GroupIDs: []int64{7}},
			{ID: 2, Name: "two", Platform: "openai", Type: "apikey", Status: "active", Schedulable: true, GroupIDs: []int64{7}},
		},
		today: map[string]model.WindowStats{
			"1": {Requests: 1, Tokens: 100, Cost: 0.01},
		},
	}
}

func decodeOverviewCacheAccounts(t *testing.T, response *httptest.ResponseRecorder) map[int64]model.WindowStats {
	t.Helper()
	var payload struct {
		Accounts []struct {
			ID         int64             `json:"id"`
			TodayUsage model.WindowStats `json:"today_usage"`
		} `json:"accounts"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	result := make(map[int64]model.WindowStats, len(payload.Accounts))
	for _, account := range payload.Accounts {
		result[account.ID] = account.TodayUsage
	}
	return result
}
