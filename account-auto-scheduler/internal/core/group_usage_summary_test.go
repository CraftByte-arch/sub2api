package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

func TestGetGroupUsageSummaryUsesExistingRollupEndpoint(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/admin/groups/usage-summary" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "admin-secret" {
			t.Fatal("missing admin API key")
		}
		writeEnvelope(t, w, []map[string]any{
			{"group_id": 7, "today_cost": 12.5, "yesterday_cost": 4.25, "total_cost": 99.75},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	snapshot, err := client.GetGroupUsageSummary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Ready || snapshot.Stale || snapshot.Source != "sub2api_group_usage_rollup" || len(snapshot.Items) != 1 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if snapshot.Items[0] != (model.GroupUsageSummary{GroupID: 7, TodayCost: 12.5, YesterdayCost: 4.25, TotalCost: 99.75}) {
		t.Fatalf("unexpected summary item: %#v", snapshot.Items[0])
	}

	// The overview polls more often than the rollup needs to change. A second
	// read within the sidecar TTL must reuse the in-memory result.
	second, err := client.GetGroupUsageSummary(context.Background())
	if err != nil || len(second.Items) != 1 || requests != 1 {
		t.Fatalf("summary cache was not used: requests=%d second=%#v err=%v", requests, second, err)
	}
}

func TestNormalizeGroupUsageSummariesDropsInvalidIDsAndNonFiniteValues(t *testing.T) {
	items := normalizeGroupUsageSummaries([]model.GroupUsageSummary{
		{GroupID: 0, TodayCost: 1},
		{GroupID: 9, TodayCost: 2, YesterdayCost: 3, TotalCost: 4},
	})
	if len(items) != 1 || items[0].GroupID != 9 || items[0].TodayCost != 2 {
		t.Fatalf("unexpected normalized summaries: %#v", items)
	}
}

func TestGroupUsageSummarySnapshotJSONContract(t *testing.T) {
	snapshot := model.GroupUsageSummarySnapshot{
		Ready: true, QueriedAt: time.Date(2026, 9, 4, 8, 0, 0, 0, time.UTC),
		Source: "sub2api_group_usage_rollup",
		Items:  []model.GroupUsageSummary{{GroupID: 1, TodayCost: 0}},
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded model.GroupUsageSummarySnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Ready || decoded.Source != snapshot.Source || len(decoded.Items) != 1 || decoded.Items[0].GroupID != 1 {
		t.Fatalf("snapshot JSON contract changed: %#v", decoded)
	}
}
