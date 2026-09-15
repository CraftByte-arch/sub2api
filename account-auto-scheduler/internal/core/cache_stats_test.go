package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetTodayStatsBatchOnlyLoadsCacheStatsForAccountsWithTodayRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/admin/accounts/today-stats/batch":
			writeEnvelope(t, w, map[string]any{"stats": map[string]any{
				"1": map[string]any{"requests": 4, "tokens": 1800, "cost": 0.75},
				"2": map[string]any{"requests": 0, "tokens": 0, "cost": 0},
			}})
		case "GET /api/v1/admin/usage/stats":
			if r.URL.Query().Get("account_id") != "1" || r.URL.Query().Get("period") != "today" {
				t.Fatalf("unexpected usage stats query: %s", r.URL.RawQuery)
			}
			writeEnvelope(t, w, map[string]any{
				"total_input_tokens":          600,
				"total_cache_creation_tokens": 300,
				"total_cache_read_tokens":     600,
			})
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
	if cache == nil || cache.InputTokens != 600 || cache.CacheCreationTokens != 300 || cache.CacheReadTokens != 600 || cache.PromptTokens != 1200 || cache.HitRate != 50 {
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
		case "GET /api/v1/admin/usage/stats":
			switch r.URL.Query().Get("account_id") {
			case "1":
				writeEnvelope(t, w, map[string]any{"total_input_tokens": 1})
			case "2":
				http.Error(w, "temporarily unavailable", http.StatusBadGateway)
			default:
				t.Fatalf("unexpected account_id: %s", r.URL.RawQuery)
			}
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
	var usageStatsCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/admin/accounts/today-stats/batch":
			writeEnvelope(t, w, map[string]any{"stats": map[string]any{
				"7": map[string]any{"requests": 1, "tokens": 30, "cost": 0.01},
			}})
		case "GET /api/v1/admin/usage/stats":
			usageStatsCalls.Add(1)
			if r.URL.Query().Get("account_id") != "7" || r.URL.Query().Get("period") != "today" {
				t.Fatalf("unexpected usage stats query: %s", r.URL.RawQuery)
			}
			writeEnvelope(t, w, map[string]any{
				"total_input_tokens":          0,
				"total_cache_creation_tokens": 0,
				"total_cache_read_tokens":     0,
			})
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
	if calls := usageStatsCalls.Load(); calls != 1 {
		t.Fatalf("usage stats calls = %d, want 1", calls)
	}
}

func TestTodayAccountCacheStatsFailedReadsBackOff(t *testing.T) {
	var usageStatsCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/admin/usage/stats" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		usageStatsCalls.Add(1)
		http.Error(w, "temporarily unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	for range 2 {
		if stats, err := client.getTodayAccountCacheStats(context.Background(), 9); err == nil || stats != nil {
			t.Fatalf("failed cache enrichment = (%#v, %v), want unavailable error", stats, err)
		}
	}
	if calls := usageStatsCalls.Load(); calls != 1 {
		t.Fatalf("usage stats calls after failure = %d, want 1 due to retry backoff", calls)
	}
}

func TestTodayAccountCacheStatsCoalescesConcurrentReads(t *testing.T) {
	var usageStatsCalls atomic.Int64
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/admin/usage/stats" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if r.URL.Query().Get("account_id") != "11" || r.URL.Query().Get("period") != "today" {
			t.Fatalf("unexpected usage stats query: %s", r.URL.RawQuery)
		}
		if usageStatsCalls.Add(1) == 1 {
			close(entered)
		}
		<-release
		writeEnvelope(t, w, map[string]any{"total_input_tokens": 100, "total_cache_read_tokens": 25})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	type result struct {
		statsPresent bool
		err          error
	}
	results := make(chan result, 2)
	var callers sync.WaitGroup
	callers.Add(2)
	for range 2 {
		go func() {
			defer callers.Done()
			stats, err := client.getTodayAccountCacheStats(context.Background(), 11)
			results <- result{statsPresent: stats != nil, err: err}
		}()
	}

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first usage stats request did not start")
	}
	// Give the second caller a chance to join the in-flight request before the
	// server is released. A duplicate request would increment the counter.
	time.Sleep(25 * time.Millisecond)
	close(release)
	callers.Wait()
	close(results)
	for result := range results {
		if result.err != nil || !result.statsPresent {
			t.Fatalf("coalesced cache enrichment result = %#v", result)
		}
	}
	if calls := usageStatsCalls.Load(); calls != 1 {
		t.Fatalf("concurrent usage stats calls = %d, want 1", calls)
	}
}
