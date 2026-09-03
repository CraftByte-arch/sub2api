package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestGetTodayStatsBatchOnlyLoadsCacheStatsForAccountsWithTodayRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/admin/accounts/today-stats/batch":
			writeEnvelope(t, w, map[string]any{"stats": map[string]any{
				"1": map[string]any{"requests": 4, "tokens": 1800, "cost": 0.75},
				"2": map[string]any{"requests": 0, "tokens": 0, "cost": 0},
			}})
		case "GET /api/v1/admin/accounts/1/stats":
			if r.URL.Query().Get("days") != "1" {
				t.Fatalf("unexpected days query: %s", r.URL.RawQuery)
			}
			writeEnvelope(t, w, map[string]any{"models": []map[string]any{
				{"input_tokens": 500, "cache_creation_tokens": 300, "cache_read_tokens": 200},
				{"input_tokens": 100, "cache_creation_tokens": 0, "cache_read_tokens": 400},
			}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	stats, err := client.GetTodayStatsBatch(context.Background(), []int64{1, 2, 3})
	if err != nil {
		t.Fatalf("GetTodayStatsBatch() error = %v", err)
	}
	cache := stats["1"].Cache
	if cache == nil || cache.InputTokens != 600 || cache.CacheCreationTokens != 300 || cache.CacheReadTokens != 600 || cache.PromptTokens != 1500 || cache.HitRate != 40 {
		t.Fatalf("unexpected account 1 cache projection: %#v", cache)
	}
	for _, accountID := range []string{"2", "3"} {
		cache := stats[accountID].Cache
		if cache == nil || cache.PromptTokens != 0 || cache.HitRate != 0 {
			t.Fatalf("idle account %s did not receive known-zero cache projection: %#v", accountID, cache)
		}
	}
}

func TestGetTodayStatsBatchKeepsActiveCacheFetchFailuresNonFatal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/admin/accounts/today-stats/batch":
			writeEnvelope(t, w, map[string]any{"stats": map[string]any{
				"1": map[string]any{"requests": 4, "tokens": 1800, "cost": 0.75},
				"2": map[string]any{"requests": 2, "tokens": 400, "cost": 0.25},
			}})
		case "GET /api/v1/admin/accounts/1/stats":
			writeEnvelope(t, w, map[string]any{"models": []map[string]any{{"input_tokens": 1}}})
		case "GET /api/v1/admin/accounts/2/stats":
			http.Error(w, "temporarily unavailable", http.StatusBadGateway)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	stats, err := newTestClient(t, server.URL).GetTodayStatsBatch(context.Background(), []int64{1, 2})
	if err != nil {
		t.Fatalf("GetTodayStatsBatch() error = %v", err)
	}
	if stats["1"].Cache == nil {
		t.Fatal("successful active account cache projection is nil")
	}
	if stats["2"].Cache != nil {
		t.Fatalf("failed active enrichment must remain unavailable: %#v", stats["2"].Cache)
	}
}

func TestGetTodayStatsBatchCachesSuccessfulActiveCacheStats(t *testing.T) {
	var accountStatsCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/admin/accounts/today-stats/batch":
			writeEnvelope(t, w, map[string]any{"stats": map[string]any{
				"7": map[string]any{"requests": 1, "tokens": 30, "cost": 0.01},
			}})
		case "GET /api/v1/admin/accounts/7/stats":
			accountStatsCalls.Add(1)
			writeEnvelope(t, w, map[string]any{"models": []map[string]any{
				{"input_tokens": 0, "cache_creation_tokens": 0, "cache_read_tokens": 0},
			}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	for range 2 {
		stats, err := client.GetTodayStatsBatch(context.Background(), []int64{7})
		if err != nil {
			t.Fatalf("GetTodayStatsBatch() error = %v", err)
		}
		if stats["7"].Cache == nil || stats["7"].Cache.PromptTokens != 0 || stats["7"].Cache.HitRate != 0 {
			t.Fatalf("known zero cache projection missing: %#v", stats["7"].Cache)
		}
	}
	if calls := accountStatsCalls.Load(); calls != 1 {
		t.Fatalf("account stats calls = %d, want 1", calls)
	}
}
