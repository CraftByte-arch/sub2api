package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountPerformanceIncrementalRollupMigrationDefinesDirtyHourQueue(t *testing.T) {
	content, err := FS.ReadFile("233_account_performance_incremental_rollup.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS account_performance_rollup_dirty_hours")
	require.Contains(t, sql, "PRIMARY KEY")
	require.Contains(t, sql, "CREATE OR REPLACE FUNCTION mark_account_performance_rollup_dirty_hours")
	require.Contains(t, sql, "REFERENCING NEW TABLE AS account_performance_rollup_changed_rows")
	require.Contains(t, sql, "AFTER INSERT ON account_performance_minute")
	require.Contains(t, sql, "AFTER UPDATE ON account_performance_minute")
	require.Contains(t, sql, "FOR EACH STATEMENT")
	require.Contains(t, sql, "SELECT DISTINCT date_trunc('hour', bucket_start)")
	require.Contains(t, sql, "ON CONFLICT (bucket_start)")
	require.Contains(t, sql, "SET LOCAL max_parallel_workers_per_gather = 0")
	require.Contains(t, sql, "FROM account_performance_minute")
}
