package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFirstTokenTimeoutStatsHourlyMigration(t *testing.T) {
	content, err := FS.ReadFile("175_first_token_timeout_stats_hourly.sql")
	require.NoError(t, err)

	sql := strings.ToLower(strings.Join(strings.Fields(string(content)), " "))
	sql = strings.ReplaceAll(sql, "( ", "(")
	sql = strings.ReplaceAll(sql, " )", ")")
	require.Contains(t, sql, "create table if not exists first_token_timeout_stats_hourly")
	require.Contains(t, sql, "primary key (bucket_start, scope, account_id, protocol, platform, model, timeout_seconds, outcome, failure_kind)")

	for _, fragment := range []string{
		"bucket_start timestamptz not null",
		"scope varchar(16) not null",
		"account_id bigint not null default 0",
		"protocol varchar(32) not null",
		"platform varchar(32) not null default ''",
		"model varchar(255) not null default ''",
		"timeout_seconds smallint not null",
		"outcome varchar(32) not null",
		"failure_kind varchar(32) not null default ''",
		"sample_count bigint not null",
		"ttft_sample_count bigint not null",
		"ttft_sum_ms bigint not null",
		"ttft_max_ms integer not null",
		"ttft_affected_count bigint not null",
		"updated_at timestamptz not null default now()",
		"check (scope in ('attempt', 'request'))",
		"check (timeout_seconds between 1 and 300)",
		"check (sample_count >= 0)",
		"check (ttft_sample_count >= 0)",
		"check (ttft_sum_ms >= 0)",
		"check (ttft_max_ms >= 0)",
		"check (ttft_affected_count >= 0)",
		"scope = 'request' and account_id = 0 and platform = ''",
		"scope = 'attempt' and account_id > 0 and platform <> ''",
		"create index if not exists idx_first_token_timeout_stats_scope_bucket on first_token_timeout_stats_hourly (scope, bucket_start)",
		"create index if not exists idx_first_token_timeout_stats_scope_account_bucket on first_token_timeout_stats_hourly (scope, account_id, bucket_start)",
		"down migration: drop table if exists first_token_timeout_stats_hourly",
	} {
		require.Contains(t, sql, fragment)
	}

	require.Contains(t, sql, "'recovered_after_ttft'")
	require.Contains(t, sql, "'ttft_exhausted'")
	require.Contains(t, sql, "'stream_idle_timeout'")
	require.NotContains(t, sql, "references accounts")
	require.NotContains(t, sql, "foreign key")
}
