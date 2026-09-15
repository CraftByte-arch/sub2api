package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetGroupUserConsumptionUsesFixedCurrentDayQueryAndStableTopTwenty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/dashboard/user-breakdown" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		query := r.URL.Query()
		for key, want := range map[string]string{
			"start_date": "2026-08-29",
			"end_date":   "2026-08-29",
			"group_id":   "42",
			"limit":      "20",
			"sort_by":    "actual_cost",
		} {
			if query.Get(key) != want {
				t.Fatalf("query %s = %q, want %q (%s)", key, query.Get(key), want, r.URL.RawQuery)
			}
		}
		rows := make([]map[string]any, 0, 25)
		for id := 1; id <= 25; id++ {
			rows = append(rows, map[string]any{
				"user_id": id, "email": "user@example.com", "actual_cost": float64(id), "requests": id, "total_tokens": id * 100,
			})
		}
		writeEnvelope(t, w, map[string]any{"users": rows})
	}))
	defer server.Close()

	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	snapshot, err := newTestClient(t, server.URL).getGroupUserConsumptionAt(context.Background(), 42, now)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.GroupID != 42 || snapshot.Date != "2026-08-29" || snapshot.Limit != 20 || snapshot.Partial || len(snapshot.Users) != 20 {
		t.Fatalf("unexpected snapshot metadata: %#v", snapshot)
	}
	if snapshot.Users[0].UserID != 25 || snapshot.Users[19].UserID != 6 {
		t.Fatalf("unexpected top-20 ordering: first=%#v last=%#v", snapshot.Users[0], snapshot.Users[19])
	}
}

func TestNormalizeGroupUserBreakdownPreservesMissingMetricsAndSortsDeterministically(t *testing.T) {
	cost := 2.0
	zero := int64(0)
	rows, partial, notice := normalizeGroupUserBreakdown([]groupUserBreakdownRow{
		{UserID: 2, Email: "two@example.com", ActualCost: &cost, Requests: &zero},
		{UserID: 1, Email: "one@example.com", ActualCost: &cost, Requests: &zero, TotalTokens: &zero},
		{UserID: 0, Email: "invalid"},
		{UserID: 1, Email: "duplicate", ActualCost: &cost, Requests: &zero, TotalTokens: &zero},
	})
	if !partial || notice == "" {
		t.Fatalf("missing/invalid data should mark partial: partial=%v notice=%q", partial, notice)
	}
	if len(rows) != 2 || rows[0].UserID != 1 || rows[1].UserID != 2 {
		t.Fatalf("unexpected stable ordering: %#v", rows)
	}
	if rows[0].ActualCost == nil || rows[0].Requests == nil || rows[0].TotalTokens == nil || rows[1].TotalTokens != nil {
		t.Fatalf("unexpected metric pointers: %#v", rows)
	}
}

func TestGetGroupUserConsumptionRejectsInvalidIDBeforeHTTP(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	_, err := newTestClient(t, server.URL).GetGroupUserConsumption(context.Background(), 0)
	if err == nil || err.Error() != "group ID must be positive" {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Fatal("invalid group ID unexpectedly made an HTTP request")
	}
}

func TestGetGroupUserConsumptionPropagatesUpstreamFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":"UNAVAILABLE","message":"breakdown unavailable"}`))
	}))
	defer server.Close()
	_, err := newTestClient(t, server.URL).GetGroupUserConsumption(context.Background(), 42)
	if err == nil || err.Error() == "" {
		t.Fatal("expected upstream failure")
	}
}

func TestGetGroupUserConsumptionAllowsEmptyResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("group_id") != "42" {
			t.Fatalf("unexpected group query: %s", r.URL.RawQuery)
		}
		writeEnvelope(t, w, map[string]any{"users": []any{}})
	}))
	defer server.Close()

	snapshot, err := newTestClient(t, server.URL).getGroupUserConsumptionAt(
		context.Background(), 42, time.Date(2026, 8, 30, 1, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Partial || len(snapshot.Users) != 0 || snapshot.Notice != "" {
		t.Fatalf("empty result should be a complete empty snapshot: %#v", snapshot)
	}
}
