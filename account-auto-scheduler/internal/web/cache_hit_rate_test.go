package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

func TestOverviewExposesTodayAccountCacheStats(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		groups:        []model.UpstreamGroup{{ID: 7, Name: "Primary"}},
		accounts: []model.UpstreamAccount{{
			ID: 9, Name: "cache-key", Platform: "openai", Type: "apikey", Status: "active", Schedulable: true, GroupIDs: []int64{7},
		}},
		today: map[string]model.WindowStats{"9": {
			Requests: 4,
			Tokens:   1800,
			Cost:     0.75,
			Cache: &model.AccountCacheStats{
				InputTokens: 600, CacheCreationTokens: 300, CacheReadTokens: 600, PromptTokens: 1500, HitRate: 40,
			},
		}},
	}
	server := newConsoleTestServer(t, backend, nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodGet, "/api/overview", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Accounts []struct {
			TodayUsage model.WindowStats `json:"today_usage"`
		} `json:"accounts"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Accounts) != 1 || payload.Accounts[0].TodayUsage.Cache == nil {
		t.Fatalf("cache projection missing from overview: %#v", payload)
	}
	cache := payload.Accounts[0].TodayUsage.Cache
	if cache.CacheReadTokens != 600 || cache.PromptTokens != 1500 || cache.HitRate != 40 {
		t.Fatalf("unexpected cache projection: %#v", cache)
	}
}

func TestGroupsAppRendersCompactCacheHitRateAndUnavailableState(t *testing.T) {
	server := NewServer(nil, &fakeAdminCore{}, Options{}, nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/app.js", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, required := range []string{
		"usage.cache", "cache.hit_rate", "cache.cache_read_tokens", "cache.prompt_tokens",
		"缓存命中 —", "缓存命中 ${hitRate.toFixed(1)}%", "缓存读取 ${formatCompact(cacheReadTokens)} / 提示词 ${formatCompact(promptTokens)} tokens",
		"今日成本 · ${escapeHTML(cacheLabel)}",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("groups app is missing cache-hit UI behavior %q", required)
		}
	}
}
