//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestAccountUsageStatsCutoverMarksOnlyHistoricalChangedDays(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	today := timezone.Today()
	closedDay := today.AddDate(0, 0, -1)
	olderClosedDay := today.AddDate(0, 0, -2)
	_, err := tx.ExecContext(ctx, `DELETE FROM account_usage_stats_dirty_days`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
		UPDATE account_usage_stats_daily_state
		SET ready = TRUE,
			coverage_start = $1::DATE,
			cursor = CURRENT_DATE,
			closed_before = CURRENT_DATE,
			updated_at = NOW()
		WHERE id = 1
	`, olderClosedDay.AddDate(0, 0, -1))
	require.NoError(t, err)

	user := mustCreateUser(t, client, &service.User{Email: "account-cutover-" + uuid.NewString() + "@example.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-account-cutover-" + uuid.NewString(), Name: "cutover"})
	account := mustCreateAccount(t, client, &service.Account{Name: "account-cutover-" + uuid.NewString()})

	current := &service.UsageLog{
		UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
		RequestID: uuid.NewString(), Model: "current-model", RequestedModel: "current-model",
		InputTokens: 1, OutputTokens: 2, TotalCost: 0.01, ActualCost: 0.02,
		CreatedAt: today.Add(time.Hour),
	}
	inserted, err := repo.Create(ctx, current)
	require.NoError(t, err)
	require.True(t, inserted)
	require.Empty(t, accountUsageStatsDirtyDays(t, tx))
	require.Zero(t, accountUsageStatsAggregateRows(t, ctx, tx, account.ID))

	historical := &service.UsageLog{
		UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
		RequestID: uuid.NewString(), Model: "historical-model", RequestedModel: "historical-model",
		InputTokens: 3, OutputTokens: 4, TotalCost: 0.03, ActualCost: 0.04,
		CreatedAt: closedDay.Add(time.Hour),
	}
	inserted, err = repo.Create(ctx, historical)
	require.NoError(t, err)
	require.True(t, inserted)
	require.Equal(t, []string{closedDay.Format("2006-01-02")}, accountUsageStatsDirtyDays(t, tx))
	require.Zero(t, accountUsageStatsAggregateRows(t, ctx, tx, account.ID))

	_, err = tx.ExecContext(ctx, `
		UPDATE usage_logs
		SET created_at = $1,
			requested_model = 'moved-historical-model'
		WHERE id = $2
	`, olderClosedDay.Add(2*time.Hour), historical.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		olderClosedDay.Format("2006-01-02"),
		closedDay.Format("2006-01-02"),
	}, accountUsageStatsDirtyDays(t, tx))

	require.NoError(t, repo.Delete(ctx, historical.ID))
	require.Equal(t, []string{
		olderClosedDay.Format("2006-01-02"),
		closedDay.Format("2006-01-02"),
	}, accountUsageStatsDirtyDays(t, tx))
	require.Zero(t, accountUsageStatsAggregateRows(t, ctx, tx, account.ID))

	var oldTriggerCount, dirtyTriggerCount int
	require.NoError(t, scanSingleRow(ctx, tx, `
		SELECT
			COUNT(*) FILTER (WHERE tgname IN (
				'trg_account_usage_stats_daily_insert',
				'trg_account_usage_stats_daily_delete'
			))::INT,
			COUNT(*) FILTER (WHERE tgname IN (
				'trg_account_usage_stats_dirty_insert',
				'trg_account_usage_stats_dirty_delete',
				'trg_account_usage_stats_dirty_update'
			))::INT
		FROM pg_trigger
		WHERE tgrelid = 'usage_logs'::regclass AND NOT tgisinternal
	`, nil, &oldTriggerCount, &dirtyTriggerCount))
	require.Zero(t, oldTriggerCount)
	require.Equal(t, 3, dirtyTriggerCount)
}

func accountUsageStatsDirtyDays(t *testing.T, tx sqlExecutor) []string {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(), `
		SELECT bucket_date::TEXT
		FROM account_usage_stats_dirty_days
		ORDER BY bucket_date
	`)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	days := make([]string, 0)
	for rows.Next() {
		var day string
		require.NoError(t, rows.Scan(&day))
		days = append(days, day)
	}
	require.NoError(t, rows.Err())
	return days
}

func accountUsageStatsAggregateRows(t *testing.T, ctx context.Context, tx sqlExecutor, accountID int64) int {
	t.Helper()
	var count int
	require.NoError(t, scanSingleRow(ctx, tx, `
		SELECT COUNT(*)::INT
		FROM account_usage_stats_daily
		WHERE account_id = $1
	`, []any{accountID}, &count))
	return count
}
