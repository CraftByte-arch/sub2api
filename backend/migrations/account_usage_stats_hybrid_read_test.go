package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountUsageStatsHybridReadMigrationKeepsStageOneTriggers(t *testing.T) {
	content, err := FS.ReadFile("234_account_usage_stats_hybrid_read.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS closed_before DATE")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS account_usage_stats_dirty_days")
	require.Contains(t, sql, "bucket_date DATE PRIMARY KEY")
	require.Contains(t, sql, "LEAST(COALESCE(cursor, CURRENT_DATE), CURRENT_DATE)")
	require.NotContains(t, sql, "DROP TRIGGER IF EXISTS trg_account_usage_stats_daily_insert")
	require.NotContains(t, sql, "DROP TRIGGER IF EXISTS trg_account_usage_stats_daily_delete")
	require.NotContains(t, sql, "DROP FUNCTION IF EXISTS apply_account_usage_stats_daily_delta")
}
