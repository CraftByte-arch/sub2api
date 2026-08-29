package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

func TestGetOnlineUsersAggregatesOpsAndTodayUsage(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "admin-secret" {
			t.Fatal("missing admin API key")
		}
		switch r.URL.Path {
		case "/api/v1/admin/ops/requests":
			if r.URL.Query().Get("kind") != "all" || r.URL.Query().Get("page_size") != "100" {
				t.Fatalf("unexpected ops query: %s", r.URL.RawQuery)
			}
			if r.URL.Query().Get("start_time") != "2026-08-29T11:50:00Z" || r.URL.Query().Get("end_time") != "2026-08-29T12:00:00Z" {
				t.Fatalf("unexpected ops window: %s", r.URL.RawQuery)
			}
			writeEnvelope(t, w, map[string]any{
				"items": []any{
					map[string]any{"created_at": "2026-08-29T11:59:00Z", "user_id": 1, "group_id": 10},
					map[string]any{"created_at": "2026-08-29T11:55:00Z", "user_id": 1, "group_id": 10},
					map[string]any{"created_at": "2026-08-29T11:53:00Z", "user_id": 2, "group_id": 10},
					map[string]any{"created_at": "2026-08-29T11:52:00Z", "user_id": 2, "group_id": 20},
				},
				"total": 4, "page": 1, "page_size": 100, "pages": 1,
			})
		case "/api/v1/admin/dashboard/user-breakdown":
			if r.URL.Query().Get("start_date") != "2026-08-29" || r.URL.Query().Get("end_date") != "2026-08-29" {
				t.Fatalf("unexpected breakdown date: %s", r.URL.RawQuery)
			}
			writeEnvelope(t, w, map[string]any{"users": []any{
				map[string]any{"user_id": 1, "email": "one@example.com", "requests": 4, "total_tokens": 1200, "actual_cost": 1.25},
			}})
		case "/api/v1/admin/dashboard/users-usage":
			var body struct {
				UserIDs []int64 `json:"user_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(body.UserIDs, []int64{1, 2}) {
				t.Fatalf("unexpected user IDs: %#v", body.UserIDs)
			}
			writeEnvelope(t, w, map[string]any{"stats": map[string]any{
				"1": map[string]any{"today_actual_cost": 1.25},
				"2": map[string]any{"today_actual_cost": 0.5},
			}})
		case "/api/v1/admin/users/2":
			writeEnvelope(t, w, map[string]any{"id": 2, "email": "two@example.com", "username": "two"})
		case "/api/v1/admin/users/1":
			writeEnvelope(t, w, map[string]any{"id": 1, "email": "one@example.com", "username": "one"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	snapshot, err := newTestClient(t, server.URL).getOnlineUsersAt(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Source != "ops" || snapshot.Partial || snapshot.Count != 2 || snapshot.WindowMinutes != 10 || !snapshot.GroupCountsAvailable || snapshot.GroupCountsPartial || snapshot.GroupCounts[10] != 2 || snapshot.GroupCounts[20] != 1 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if len(snapshot.Users) != 2 || snapshot.Users[0].ID != 1 || snapshot.Users[1].ID != 2 {
		t.Fatalf("unexpected user ordering: %#v", snapshot.Users)
	}
	if snapshot.Users[0].DisplayName != "one" || snapshot.Users[1].DisplayName != "two" {
		t.Fatalf("unexpected display names: %#v", snapshot.Users)
	}
	if snapshot.Users[0].TodayCost == nil || *snapshot.Users[0].TodayCost != 1.25 || snapshot.Users[0].TodayTokens == nil || *snapshot.Users[0].TodayTokens != 1200 {
		t.Fatalf("unexpected first usage: %#v", snapshot.Users[0])
	}
	if snapshot.Users[1].TodayCost == nil || *snapshot.Users[1].TodayCost != 0.5 {
		t.Fatalf("unexpected second usage: %#v", snapshot.Users[1])
	}
}

func TestGetOnlineUsersFallsBackToUsageLogs(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/ops/requests":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"NOT_FOUND","message":"ops disabled"}`))
		case "/api/v1/admin/usage":
			writeEnvelope(t, w, map[string]any{
				"items": []any{
					map[string]any{"user_id": 7, "group_id": 30, "created_at": "2026-08-29T11:59:00Z", "user": map[string]any{"id": 7, "email": "fallback@example.com", "username": "fallback"}},
					map[string]any{"user_id": 9, "created_at": "2026-08-29T11:40:00Z", "user": map[string]any{"id": 9, "email": "old@example.com"}},
				},
				"total": 2, "page": 1, "page_size": 1000, "pages": 1,
			})
		case "/api/v1/admin/dashboard/user-breakdown":
			writeEnvelope(t, w, map[string]any{"users": []any{map[string]any{"user_id": 7, "email": "fallback@example.com", "requests": 2, "total_tokens": 10, "actual_cost": 0.2}}})
		case "/api/v1/admin/dashboard/users-usage":
			writeEnvelope(t, w, map[string]any{"stats": map[string]any{"7": map[string]any{"today_actual_cost": 0.2}}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	snapshot, err := newTestClient(t, server.URL).getOnlineUsersAt(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Source != "usage_fallback" || !snapshot.Partial || snapshot.Count != 1 || snapshot.Users[0].ID != 7 {
		t.Fatalf("unexpected fallback snapshot: %#v", snapshot)
	}
	if snapshot.Users[0].DisplayName != "fallback" || snapshot.Users[0].TodayCost == nil {
		t.Fatalf("fallback identity/usage missing: %#v", snapshot.Users[0])
	}
	if !snapshot.GroupCountsAvailable || !snapshot.GroupCountsPartial || snapshot.GroupCounts[30] != 1 {
		t.Fatalf("unexpected fallback group counts: %#v", snapshot)
	}
}

func TestAddOnlineUserGroupTreatsMissingOpsGroupAsUngrouped(t *testing.T) {
	candidate := onlineUserCandidate{}
	addOnlineUserGroup(&candidate, optionalGroupID{}, true)
	counts, available, partial := onlineUserGroupCounts(map[int64]onlineUserCandidate{1: candidate})
	if !available || partial || counts[0] != 1 {
		t.Fatalf("ungrouped Ops request was not counted: %#v available=%v partial=%v", counts, available, partial)
	}
}

func TestAddOnlineUserGroupKeepsMissingFallbackFieldUnavailable(t *testing.T) {
	candidate := onlineUserCandidate{}
	addOnlineUserGroup(&candidate, optionalGroupID{}, false)
	counts, available, partial := onlineUserGroupCounts(map[int64]onlineUserCandidate{1: candidate})
	if available || !partial || len(counts) != 0 {
		t.Fatalf("missing fallback group field should stay unavailable: %#v available=%v partial=%v", counts, available, partial)
	}
}

func TestOnlineUserGroupCountsDeduplicateUsersAndReportMissingGroups(t *testing.T) {
	counts, available, partial := onlineUserGroupCounts(map[int64]onlineUserCandidate{
		1: {groupIDs: map[int64]struct{}{10: {}, 20: {}}, groupInfoSeen: true},
		2: {groupIDs: map[int64]struct{}{10: {}}, groupInfoSeen: true},
		3: {},
	})
	if !available || !partial {
		t.Fatalf("availability flags = available:%v partial:%v", available, partial)
	}
	if counts[10] != 2 || counts[20] != 1 || len(counts) != 2 {
		t.Fatalf("unexpected per-group counts: %#v", counts)
	}
}

func TestOnlineUserGroupCountsAreAvailableWhenNoUsers(t *testing.T) {
	counts, available, partial := onlineUserGroupCounts(nil)
	if !available || partial || len(counts) != 0 {
		t.Fatalf("empty group counts should be an available empty result: %#v available=%v partial=%v", counts, available, partial)
	}
}

func TestGetOnlineUsersKeepsUsersWhenUsageSummariesFail(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/ops/requests":
			writeEnvelope(t, w, map[string]any{
				"items": []any{map[string]any{"created_at": "2026-08-29T11:59:00Z", "user_id": 3}},
				"total": 1, "page": 1, "page_size": 100, "pages": 1,
			})
		case "/api/v1/admin/dashboard/user-breakdown", "/api/v1/admin/dashboard/users-usage":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":"UNAVAILABLE","message":"temporarily unavailable"}`))
		case "/api/v1/admin/users/3":
			writeEnvelope(t, w, map[string]any{"id": 3, "email": "three@example.com"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	snapshot, err := newTestClient(t, server.URL).getOnlineUsersAt(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Partial || snapshot.Count != 1 || snapshot.Users[0].TodayCost != nil || snapshot.Users[0].DisplayName != "three@example.com" {
		t.Fatalf("unexpected partial snapshot: %#v", snapshot)
	}
}

func TestGetOnlineUsersKeepsAvailableCostWhenOneUsageSummaryFails(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/ops/requests":
			writeEnvelope(t, w, map[string]any{
				"items": []any{map[string]any{"created_at": "2026-08-29T11:59:00Z", "user_id": 5, "group_id": 50}},
				"total": 1, "page": 1, "page_size": 100, "pages": 1,
			})
		case "/api/v1/admin/dashboard/user-breakdown":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":"UNAVAILABLE","message":"breakdown unavailable"}`))
		case "/api/v1/admin/dashboard/users-usage":
			writeEnvelope(t, w, map[string]any{"stats": map[string]any{
				"5": map[string]any{"today_actual_cost": 0.75},
			}})
		case "/api/v1/admin/users/5":
			writeEnvelope(t, w, map[string]any{"id": 5, "email": "five@example.com", "username": "five"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	snapshot, err := newTestClient(t, server.URL).getOnlineUsersAt(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Partial || snapshot.Count != 1 || snapshot.Users[0].TodayCost == nil || *snapshot.Users[0].TodayCost != 0.75 {
		t.Fatalf("available batch cost should remain usable: %#v", snapshot)
	}
	if snapshot.Users[0].DisplayName != "five" {
		t.Fatalf("identity enrichment missing: %#v", snapshot.Users[0])
	}
}

func TestOptionalGroupIDDistinguishesUngroupedFromMissingField(t *testing.T) {
	var grouped opsRequestDetail
	if err := json.Unmarshal([]byte(`{"group_id":42}`), &grouped); err != nil {
		t.Fatal(err)
	}
	if !grouped.GroupID.Present || !grouped.GroupID.Valid || grouped.GroupID.ID != 42 {
		t.Fatalf("grouped value not preserved: %#v", grouped.GroupID)
	}

	var ungrouped opsRequestDetail
	if err := json.Unmarshal([]byte(`{"group_id":null}`), &ungrouped); err != nil {
		t.Fatal(err)
	}
	if !ungrouped.GroupID.Present || ungrouped.GroupID.Valid {
		t.Fatalf("null group should be a known ungrouped request: %#v", ungrouped.GroupID)
	}

	var legacy opsRequestDetail
	if err := json.Unmarshal([]byte(`{}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.GroupID.Present {
		t.Fatalf("omitted group field must remain distinguishable: %#v", legacy.GroupID)
	}
}

func TestGetOnlineUsersDoesNotCountStaleOpsRecords(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/admin/ops/requests" {
			writeEnvelope(t, w, map[string]any{
				"items": []any{map[string]any{"created_at": "2026-08-29T11:49:59Z", "user_id": 4}},
				"total": 1, "page": 1, "page_size": 100, "pages": 1,
			})
			return
		}
		t.Fatalf("unexpected path: %s", r.URL.Path)
	}))
	defer server.Close()

	snapshot, err := newTestClient(t, server.URL).getOnlineUsersAt(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Count != 0 || len(snapshot.Users) != 0 {
		t.Fatalf("stale record counted: %#v", snapshot)
	}
}

func TestEnrichOnlineUserIdentitiesHandlesCanceledContext(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	candidates := map[int64]onlineUserCandidate{
		1: {id: 1},
		2: {id: 2},
		3: {id: 3},
	}

	got := newTestClient(t, server.URL).enrichOnlineUserIdentities(ctx, candidates, map[int64]todayUserUsage{})
	if got != len(candidates) {
		t.Fatalf("canceled identity lookups = %d, want %d", got, len(candidates))
	}
}

func TestOnlineUsersSnapshotJSONKeepsUnavailableUsageAsNull(t *testing.T) {
	snapshot := model.OnlineUsersSnapshot{
		Count:         1,
		WindowMinutes: 10,
		QueriedAt:     time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
		Source:        "ops",
		Users: []model.OnlineUser{{
			ID:          1,
			DisplayName: "user",
			LastCallAt:  snapshotTime("2026-08-29T11:59:00Z"),
		}},
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == "" || !containsJSONNull(raw, "today_cost") || !containsJSONNull(raw, "today_tokens") {
		t.Fatalf("unavailable usage must be explicit null: %s", raw)
	}
}

func snapshotTime(raw string) time.Time {
	value, _ := time.Parse(time.RFC3339, raw)
	return value
}

func containsJSONNull(raw []byte, field string) bool {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	users, ok := value["users"].([]any)
	if !ok || len(users) == 0 {
		return false
	}
	row, ok := users[0].(map[string]any)
	return ok && row[field] == nil
}
