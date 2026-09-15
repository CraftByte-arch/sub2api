package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGroupUserConsumptionSnapshotJSONContract(t *testing.T) {
	cost := 1.25
	requests := int64(12)
	tokens := int64(2048)
	snapshot := GroupUserConsumptionSnapshot{
		GroupID:   42,
		GroupName: "OpenAI",
		Date:      "2026-08-29",
		Limit:     20,
		QueriedAt: time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
		Users: []GroupUserConsumptionRecord{{
			UserID:      7,
			DisplayName: "alice@example.com",
			Email:       "alice@example.com",
			ActualCost:  &cost,
			Requests:    &requests,
			TotalTokens: &tokens,
		}},
	}

	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, field := range []string{`"group_id":42`, `"group_name":"OpenAI"`, `"date":"2026-08-29"`, `"limit":20`, `"actual_cost":1.25`, `"requests":12`, `"total_tokens":2048`} {
		if !strings.Contains(text, field) {
			t.Fatalf("JSON is missing %s: %s", field, text)
		}
	}

	var decoded GroupUserConsumptionSnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.GroupID != 42 || decoded.GroupName != "OpenAI" || len(decoded.Users) != 1 || decoded.Users[0].ActualCost == nil || *decoded.Users[0].ActualCost != cost {
		t.Fatalf("decoded snapshot does not preserve contract: %#v", decoded)
	}
}

func TestGroupUserConsumptionRecordJSONUsesNullForMissingMetrics(t *testing.T) {
	raw, err := json.Marshal(GroupUserConsumptionRecord{
		UserID:      9,
		DisplayName: "用户 #9",
	})
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"actual_cost", "requests", "total_tokens"} {
		if value[field] != nil {
			t.Fatalf("%s should be JSON null when unavailable: %s", field, raw)
		}
	}
}
