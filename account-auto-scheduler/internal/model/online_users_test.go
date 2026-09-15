package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestOnlineUsersSnapshotJSONContract(t *testing.T) {
	through := time.Date(2026, 8, 29, 11, 59, 0, 0, time.UTC)
	cost := 0.125
	tokens := int64(2048)
	requests := int64(3)
	snapshot := OnlineUsersSnapshot{
		Ready:                 true,
		Count:                 1,
		WindowMinutes:         10,
		QueriedAt:             time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
		DataThrough:           &through,
		AggregationLagSeconds: 60,
		Source:                "aggregate_db",
		GroupCounts:           map[int64]int{42: 1},
		GroupCountsAvailable:  true,
		Users: []OnlineUser{{
			ID:            7,
			DisplayName:   "alice",
			Email:         "alice@example.com",
			GroupIDs:      []int64{42},
			LastCallAt:    time.Date(2026, 8, 29, 11, 59, 0, 0, time.UTC),
			TodayCost:     &cost,
			TodayTokens:   &tokens,
			TodayRequests: &requests,
		}},
	}

	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, field := range []string{
		`"ready":true`, `"count":1`, `"window_minutes":10`,
		`"source":"aggregate_db"`, `"aggregation_lag_seconds":60`,
		`"group_counts":{"42":1}`, `"group_counts_available":true`,
		`"display_name":"alice"`, `"group_ids":[42]`,
		`"today_cost":0.125`, `"today_tokens":2048`, `"today_requests":3`,
	} {
		if !strings.Contains(text, field) {
			t.Fatalf("JSON is missing %s: %s", field, text)
		}
	}

	var decoded OnlineUsersSnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Ready || decoded.Count != 1 || decoded.GroupCounts[42] != 1 || !decoded.GroupCountsAvailable || len(decoded.Users) != 1 || len(decoded.Users[0].GroupIDs) != 1 || decoded.Users[0].TodayCost == nil || *decoded.Users[0].TodayCost != cost {
		t.Fatalf("decoded snapshot does not preserve the contract: %#v", decoded)
	}
}

func TestOnlineUserJSONUsesNullForUnavailableUsage(t *testing.T) {
	raw, err := json.Marshal(OnlineUser{
		ID:          9,
		DisplayName: "用户 #9",
		LastCallAt:  time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"today_cost", "today_tokens", "today_requests"} {
		if value[field] != nil {
			t.Fatalf("%s should be JSON null when unavailable: %s", field, raw)
		}
	}
}

func TestOnlineUsersSummarySnapshotConversionsAreDeepCopies(t *testing.T) {
	through := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	cost := 1.25
	tokens := int64(1200)
	requests := int64(4)
	summary := OnlineUsersSummary{
		Ready:                true,
		Count:                1,
		WindowMinutes:        10,
		DataThrough:          &through,
		GroupCounts:          map[int64]int{10: 1},
		GroupCountsAvailable: true,
	}
	snapshot := summary.WithUsers([]OnlineUser{{
		ID:            1,
		GroupIDs:      []int64{10},
		TodayCost:     &cost,
		TodayTokens:   &tokens,
		TodayRequests: &requests,
	}})

	summary.GroupCounts[10] = 99
	through = through.Add(time.Hour)
	if snapshot.GroupCounts[10] != 1 || snapshot.DataThrough == nil || snapshot.DataThrough.Hour() != 9 {
		t.Fatalf("snapshot shares summary storage: %#v", snapshot)
	}

	projected := snapshot.Summary()
	snapshot.GroupCounts[10] = 88
	snapshot.Users[0].GroupIDs[0] = 88
	*snapshot.Users[0].TodayCost = 88
	if projected.GroupCounts[10] != 1 {
		t.Fatalf("summary projection shares snapshot map: %#v", projected)
	}

	cloned := projected.WithUsers(snapshot.Users)
	snapshot.Users[0].GroupIDs[0] = 77
	*snapshot.Users[0].TodayCost = 77
	if cloned.Users[0].GroupIDs[0] != 88 || cloned.Users[0].TodayCost == nil || *cloned.Users[0].TodayCost != 88 {
		t.Fatalf("user projection was not deeply copied: %#v", cloned.Users[0])
	}
}
