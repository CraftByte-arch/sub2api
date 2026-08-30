package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserUsageAnalyticsHourlyMigrationDefinesExactStatementAggregation(t *testing.T) {
	content, err := FS.ReadFile("232_user_usage_analytics_hourly.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS user_usage_analytics_hourly")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS user_usage_analytics_hourly_state")
	require.Contains(t, sql, "PRIMARY KEY (")
	require.Contains(t, sql, "openai_ws_mode")
	require.Contains(t, sql, "COALESCE(NULLIF(TRIM(rows.requested_model), ''), rows.model)")
	require.Contains(t, sql, "WHEN rows.billing_mode IS NULL OR rows.billing_mode = ''")
	require.Contains(t, sql, "COALESCE(NULLIF(TRIM(rows.inbound_endpoint), ''), 'unknown')")
	require.Contains(t, sql, "COALESCE(rows.account_stats_cost, rows.total_cost) * COALESCE(rows.account_rate_multiplier, 1)")
	require.Contains(t, sql, "REFERENCING NEW TABLE AS user_usage_analytics_delta_rows")
	require.Contains(t, sql, "REFERENCING OLD TABLE AS user_usage_analytics_delta_rows")
	require.Contains(t, sql, "trg_user_usage_analytics_hourly_update_1_old")
	require.Contains(t, sql, "trg_user_usage_analytics_hourly_update_2_new")
	require.Contains(t, sql, "target.requests <= 0")
	require.NotContains(t, strings.ToUpper(sql), "CREATE INDEX CONCURRENTLY")
}
