package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountUsageStatsWritePathCutoverMigrationIsBoundedAndIndependent(t *testing.T) {
	content, err := FS.ReadFile("235_account_usage_stats_write_path_cutover.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "SET LOCAL lock_timeout = '1s'")
	require.Contains(t, sql, "SET LOCAL statement_timeout = '10s'")
	require.Contains(t, sql, "DROP TRIGGER IF EXISTS trg_account_usage_stats_daily_insert")
	require.Contains(t, sql, "DROP TRIGGER IF EXISTS trg_account_usage_stats_daily_delete")
	require.Contains(t, sql, "DROP FUNCTION IF EXISTS apply_account_usage_stats_daily_delta()")
	require.Contains(t, sql, "CREATE TRIGGER trg_account_usage_stats_dirty_insert")
	require.Contains(t, sql, "CREATE TRIGGER trg_account_usage_stats_dirty_delete")
	require.Contains(t, sql, "CREATE TRIGGER trg_account_usage_stats_dirty_update")
	require.Contains(t, sql, "REFERENCING NEW TABLE AS account_usage_stats_new_rows")
	require.Contains(t, sql, "REFERENCING OLD TABLE AS account_usage_stats_old_rows")
	require.Contains(t, sql, "affected_date >= CURRENT_DATE")
	require.Contains(t, sql, "FOR KEY SHARE")
	require.Contains(t, sql, "ON CONFLICT (bucket_date) DO NOTHING")
	require.NotContains(t, sql, "trg_user_dashboard_route_daily")
	require.NotContains(t, sql, "trg_user_usage_analytics_hourly")
	require.NotContains(t, sql, "usage_logs_group_rollup")
}
