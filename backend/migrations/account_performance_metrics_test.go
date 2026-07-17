package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountPerformanceMetricsMigration(t *testing.T) {
	content, err := FS.ReadFile("182_account_performance_metrics.sql")
	require.NoError(t, err)

	sql := strings.ToLower(strings.Join(strings.Fields(string(content)), " "))
	sql = strings.ReplaceAll(sql, "( ", "(")
	sql = strings.ReplaceAll(sql, " )", ")")

	for _, table := range []string{
		"account_performance_minute",
		"account_performance_hourly",
	} {
		tableSQL := accountPerformanceTableBlock(t, sql, table)
		tableSectionSQL := accountPerformanceTableSection(t, sql, table)
		require.Contains(t, tableSQL, "primary key (bucket_start, account_id, platform, group_id, model, protocol, outcome)")

		for _, fragment := range []string{
			"bucket_start timestamptz not null",
			"account_id bigint not null",
			"platform varchar(32) not null default ''",
			"group_id bigint not null default 0",
			"model varchar(255) not null default ''",
			"protocol varchar(32) not null default ''",
			"outcome varchar(32) not null",
			"attempt_count bigint not null",
			"success_count bigint not null",
			"client_canceled_count bigint not null",
			"ttft_timeout_count bigint not null",
			"rate_limit_count bigint not null",
			"auth_count bigint not null",
			"upstream_4xx_count bigint not null",
			"upstream_5xx_count bigint not null",
			"transport_count bigint not null",
			"protocol_count bigint not null",
			"other_failure_count bigint not null",
			"failover_count bigint not null",
			"ttft_sample_count bigint not null",
			"ttft_sum_ms bigint not null",
			"duration_sample_count bigint not null",
			"duration_sum_ms bigint not null",
			"ttft_le_1000_ms bigint not null",
			"ttft_le_2500_ms bigint not null",
			"ttft_le_5000_ms bigint not null",
			"ttft_le_10000_ms bigint not null",
			"ttft_le_30000_ms bigint not null",
			"ttft_gt_30000_ms bigint not null",
			"duration_le_1000_ms bigint not null",
			"duration_le_2500_ms bigint not null",
			"duration_le_5000_ms bigint not null",
			"duration_le_10000_ms bigint not null",
			"duration_le_30000_ms bigint not null",
			"duration_gt_30000_ms bigint not null",
			"created_at timestamptz not null default now()",
			"updated_at timestamptz not null default now()",
			"check (outcome in ('success', 'ttft_timeout', 'rate_limit', 'auth', 'upstream_4xx', 'upstream_5xx', 'transport', 'protocol', 'other_failure', 'client_canceled'))",
		} {
			require.Contains(t, tableSQL, fragment)
		}

		for _, invariant := range []string{
			"success_count + client_canceled_count + ttft_timeout_count + rate_limit_count + auth_count + upstream_4xx_count + upstream_5xx_count + transport_count + protocol_count + other_failure_count = attempt_count",
			"failover_count <= attempt_count",
			"ttft_sample_count <= success_count",
			"duration_sample_count <= success_count",
			"ttft_sample_count = 0 and ttft_sum_ms = 0",
			"duration_sample_count = 0 and duration_sum_ms = 0",
			"ttft_le_1000_ms <= ttft_le_2500_ms and ttft_le_2500_ms <= ttft_le_5000_ms and ttft_le_5000_ms <= ttft_le_10000_ms and ttft_le_10000_ms <= ttft_le_30000_ms",
			"duration_le_1000_ms <= duration_le_2500_ms and duration_le_2500_ms <= duration_le_5000_ms and duration_le_5000_ms <= duration_le_10000_ms and duration_le_10000_ms <= duration_le_30000_ms",
			"ttft_le_30000_ms + ttft_gt_30000_ms = ttft_sample_count",
			"duration_le_30000_ms + duration_gt_30000_ms = duration_sample_count",
		} {
			require.Contains(t, tableSQL, invariant)
		}

		for _, outcomeCounter := range []string{
			"outcome = 'success' and success_count = attempt_count",
			"outcome = 'client_canceled' and client_canceled_count = attempt_count",
			"outcome = 'ttft_timeout' and ttft_timeout_count = attempt_count",
			"outcome = 'rate_limit' and rate_limit_count = attempt_count",
			"outcome = 'auth' and auth_count = attempt_count",
			"outcome = 'upstream_4xx' and upstream_4xx_count = attempt_count",
			"outcome = 'upstream_5xx' and upstream_5xx_count = attempt_count",
			"outcome = 'transport' and transport_count = attempt_count",
			"outcome = 'protocol' and protocol_count = attempt_count",
			"outcome = 'other_failure' and other_failure_count = attempt_count",
		} {
			require.Contains(t, tableSQL, outcomeCounter)
		}

		for _, index := range []string{
			"create index if not exists idx_" + table + "_bucket on " + table + " (bucket_start)",
			"create index if not exists idx_" + table + "_account_bucket on " + table + " (account_id, bucket_start)",
			"create index if not exists idx_" + table + "_platform_bucket on " + table + " (platform, bucket_start)",
			"create index if not exists idx_" + table + "_group_bucket on " + table + " (group_id, bucket_start)",
			"create index if not exists idx_" + table + "_model_bucket on " + table + " (model, bucket_start)",
			"create index if not exists idx_" + table + "_filter on " + table + " (bucket_start, platform, group_id, model, protocol)",
		} {
			require.Contains(t, tableSectionSQL, index)
		}
	}

	require.Contains(t, sql, "down migration: drop table if exists account_performance_minute")
	require.Contains(t, sql, "down migration: drop table if exists account_performance_hourly")
}

func accountPerformanceTableBlock(t *testing.T, sql, table string) string {
	t.Helper()

	start := strings.Index(sql, "create table if not exists "+table+" (")
	require.GreaterOrEqual(t, start, 0)
	end := strings.Index(sql[start:], ");")
	require.GreaterOrEqual(t, end, 0)

	return sql[start : start+end+2]
}

func accountPerformanceTableSection(t *testing.T, sql, table string) string {
	t.Helper()

	start := strings.Index(sql, "create table if not exists "+table+" (")
	require.GreaterOrEqual(t, start, 0)
	nextTable := strings.Index(sql[start+1:], "create table if not exists ")
	if nextTable < 0 {
		return sql[start:]
	}
	return sql[start : start+1+nextTable]
}
