//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestGroupUsageHourlyAggregation_InsertedLogAndRetry(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	_, err := tx.ExecContext(ctx, `UPDATE group_usage_aggregation_state SET ready = FALSE, cursor = NULL WHERE id = 1`)
	require.NoError(t, err)

	group := mustCreateGroup(t, client, &service.Group{Name: "usage-hourly-" + uuid.NewString()})
	user := mustCreateUser(t, client, &service.User{Email: "usage-hourly-" + uuid.NewString() + "@example.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-usage-hourly-" + uuid.NewString(), Name: "usage hourly"})
	account := mustCreateAccount(t, client, &service.Account{Name: "usage-hourly-" + uuid.NewString()})
	requestID := uuid.NewString()
	now := time.Now().UTC()

	log := &service.UsageLog{
		UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
		RequestID: requestID, Model: "test-model", GroupID: &group.ID,
		TotalCost: 1.25, ActualCost: 1.25, CreatedAt: now,
	}
	inserted, err := repo.Create(dbent.NewTxContext(ctx, tx), log)
	require.NoError(t, err)
	require.True(t, inserted)

	retry := *log
	retry.ID = 0
	inserted, err = repo.Create(dbent.NewTxContext(ctx, tx), &retry)
	require.NoError(t, err)
	require.False(t, inserted)

	var hourlyCost float64
	err = scanSingleRow(ctx, tx, `
		SELECT actual_cost
		FROM group_usage_hourly
		WHERE group_id = $1 AND bucket_start = $2
	`, []any{group.ID, utcHour(now)}, &hourlyCost)
	require.NoError(t, err)
	require.InDelta(t, 1.25, hourlyCost, 1e-9)

	summary, err := repo.GetAllGroupUsageSummary(ctx, utcHour(now))
	require.NoError(t, err)
	require.Equal(t, usagestats.GroupUsageSummary{GroupID: group.ID, TodayCost: 1.25, TotalCost: 1.25}, findGroupUsageSummary(t, summary, group.ID))

	_, err = tx.ExecContext(ctx, `UPDATE group_usage_aggregation_state SET ready = TRUE, cursor = NOW() WHERE id = 1`)
	require.NoError(t, err)
	summary, err = repo.GetAllGroupUsageSummary(ctx, utcHour(now))
	require.NoError(t, err)
	require.Equal(t, usagestats.GroupUsageSummary{GroupID: group.ID, TodayCost: 1.25, TotalCost: 1.25}, findGroupUsageSummary(t, summary, group.ID))
}

func TestGroupUsageHourlyAggregation_ExcludesSoftDeletedGroup(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	_, err := tx.ExecContext(ctx, `UPDATE group_usage_aggregation_state SET ready = TRUE, cursor = NOW() WHERE id = 1`)
	require.NoError(t, err)

	group := mustCreateGroup(t, client, &service.Group{Name: "usage-hourly-deleted-" + uuid.NewString()})
	require.NoError(t, newGroupUsageAggregation(tx).Add(ctx, group.ID, 3.5))
	_, err = tx.ExecContext(ctx, `UPDATE groups SET deleted_at = NOW() WHERE id = $1`, group.ID)
	require.NoError(t, err)

	summary, err := repo.GetAllGroupUsageSummary(ctx, time.Now().UTC().Truncate(time.Hour))
	require.NoError(t, err)
	for _, row := range summary {
		require.NotEqual(t, group.ID, row.GroupID)
	}
}

func TestGroupUsageHourlyAggregation_CleanupPreservesCutoffHour(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	_, err := tx.ExecContext(ctx, `UPDATE group_usage_aggregation_state SET ready = TRUE, cursor = NOW() WHERE id = 1`)
	require.NoError(t, err)

	group := mustCreateGroup(t, client, &service.Group{Name: "usage-hourly-retention-" + uuid.NewString()})
	user := mustCreateUser(t, client, &service.User{Email: "usage-hourly-retention-" + uuid.NewString() + "@example.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-usage-hourly-retention-" + uuid.NewString(), Name: "usage hourly retention"})
	account := mustCreateAccount(t, client, &service.Account{Name: "usage-hourly-retention-" + uuid.NewString()})

	hour := utcHour(time.Now().Add(-3 * time.Hour))
	for _, entry := range []struct {
		createdAt time.Time
		cost      float64
	}{
		{createdAt: hour.Add(10 * time.Minute), cost: 1.25},
		{createdAt: hour.Add(45 * time.Minute), cost: 2.5},
	} {
		_, err := repo.Create(dbent.NewTxContext(ctx, tx), &service.UsageLog{
			UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
			RequestID: uuid.NewString(), Model: "test-model", GroupID: &group.ID,
			TotalCost: entry.cost, ActualCost: entry.cost, CreatedAt: entry.createdAt,
		})
		require.NoError(t, err)
	}

	cutoff := hour.Add(30 * time.Minute)
	_, err = tx.ExecContext(ctx, `DELETE FROM usage_logs WHERE group_id = $1 AND created_at < $2`, group.ID, cutoff)
	require.NoError(t, err)
	require.NoError(t, newGroupUsageAggregation(tx).CleanupBefore(ctx, cutoff))

	var hourlyCost float64
	err = scanSingleRow(ctx, tx, `
		SELECT actual_cost
		FROM group_usage_hourly
		WHERE group_id = $1 AND bucket_start = $2
	`, []any{group.ID, hour}, &hourlyCost)
	require.NoError(t, err)
	require.InDelta(t, 2.5, hourlyCost, 1e-9)
}

func findGroupUsageSummary(t *testing.T, summaries []usagestats.GroupUsageSummary, groupID int64) usagestats.GroupUsageSummary {
	t.Helper()
	for _, summary := range summaries {
		if summary.GroupID == groupID {
			return summary
		}
	}
	t.Fatalf("group usage summary missing group %d", groupID)
	return usagestats.GroupUsageSummary{}
}
