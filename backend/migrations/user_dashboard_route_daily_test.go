package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserDashboardRouteDailyMigrationDefinesDurableStatementAggregation(t *testing.T) {
	content, err := FS.ReadFile("231_user_dashboard_route_daily.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS user_dashboard_route_daily")
	require.Contains(t, sql, "PRIMARY KEY (user_id, bucket_date, group_id, account_id)")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS user_dashboard_route_daily_state")
	require.Contains(t, sql, "REFERENCING NEW TABLE AS user_dashboard_delta_rows")
	require.Contains(t, sql, "REFERENCING OLD TABLE AS user_dashboard_delta_rows")
	require.Contains(t, sql, "trg_user_dashboard_route_daily_update_1_old")
	require.Contains(t, sql, "trg_user_dashboard_route_daily_update_2_new")
	require.Contains(t, sql, "COUNT(*) FILTER (WHERE delta_rows.actual_cost > 0)")
	require.Contains(t, sql, "USING (")
	require.NotContains(t, strings.ToUpper(sql), "CREATE INDEX CONCURRENTLY")
}
