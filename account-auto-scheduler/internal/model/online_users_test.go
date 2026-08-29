package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestOnlineUsersSnapshotJSONContract(t *testing.T) {
	cost := 0.125
	tokens := int64(2048)
	requests := int64(3)
	snapshot := OnlineUsersSnapshot{
		Count:         1,
		WindowMinutes: 10,
		QueriedAt:     time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
		Source:        "ops",
		Users: []OnlineUser{{
			ID:            7,
			DisplayName:   "alice",
			Email:         "alice@example.com",
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
	for _, field := range []string{`"count":1`, `"window_minutes":10`, `"display_name":"alice"`, `"today_cost":0.125`, `"today_tokens":2048`, `"today_requests":3`} {
		if !strings.Contains(text, field) {
			t.Fatalf("JSON is missing %s: %s", field, text)
		}
	}

	var decoded OnlineUsersSnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Count != 1 || len(decoded.Users) != 1 || decoded.Users[0].TodayCost == nil || *decoded.Users[0].TodayCost != cost {
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
