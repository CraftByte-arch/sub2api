package migrations

import (
	"strings"
	"testing"
)

func TestImageStudioSettlementPayloadMigration(t *testing.T) {
	contents, err := FS.ReadFile("173_image_studio_settlement_payload.sql")
	if err != nil {
		t.Fatalf("read settlement payload migration: %v", err)
	}

	sql := strings.ToLower(string(contents))
	if !strings.Contains(sql, "add column if not exists settlement_payload jsonb") {
		t.Fatalf("migration must add an idempotent JSONB settlement_payload column: %s", contents)
	}
	if !strings.Contains(sql, "not null default '{}'::jsonb") {
		t.Fatalf("settlement_payload must be non-null with an empty-object default: %s", contents)
	}
}
