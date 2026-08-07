//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestAPIKeyUsageDaily_AddAtomicallyIncrementsCurrentBucket(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	store := newAPIKeyUsageDailyStore(tx)
	apiKeyID := time.Now().UnixNano()

	require.NoError(t, store.Add(ctx, apiKeyID, 1.25))
	require.NoError(t, store.Add(ctx, apiKeyID, 0.75))
	require.NoError(t, store.Add(ctx, apiKeyID, -0.25))

	var actualCost float64
	require.NoError(t, scanSingleRow(ctx, tx, `
		SELECT actual_cost
		FROM api_key_usage_daily
		WHERE api_key_id = $1 AND bucket_date = CURRENT_DATE
	`, []any{apiKeyID}, &actualCost))
	require.InDelta(t, 1.75, actualCost, 1e-9)
}

func TestAPIKeyUsageDaily_CreateRetryAndCompatibleQuery(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	today := timezone.Today()
	_, err := tx.ExecContext(ctx, `
		UPDATE api_key_usage_daily_state
		SET ready = TRUE, coverage_start = $1::date, cursor = $2::date, updated_at = NOW()
		WHERE id = 1
	`, today.AddDate(0, 0, -(apiKeyUsageDailyDefaultDays-1)), today.AddDate(0, 0, 1))
	require.NoError(t, err)

	user := mustCreateUser(t, client, &service.User{Email: "api-key-daily-" + uuid.NewString() + "@example.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-api-key-daily-" + uuid.NewString(), Name: "daily"})
	account := mustCreateAccount(t, client, &service.Account{Name: "api-key-daily-" + uuid.NewString()})

	yesterday := &service.UsageLog{
		UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
		RequestID: uuid.NewString(), Model: "test-model",
		TotalCost: 2, ActualCost: 2, CreatedAt: today.AddDate(0, 0, -1).Add(time.Hour),
	}
	inserted, err := repo.Create(dbent.NewTxContext(ctx, tx), yesterday)
	require.NoError(t, err)
	require.True(t, inserted)

	requestID := uuid.NewString()
	todayLog := &service.UsageLog{
		UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
		RequestID: requestID, Model: "test-model",
		TotalCost: 1.25, ActualCost: 1.25, CreatedAt: today.Add(time.Hour),
	}
	inserted, err = repo.Create(dbent.NewTxContext(ctx, tx), todayLog)
	require.NoError(t, err)
	require.True(t, inserted)

	retry := *todayLog
	retry.ID = 0
	inserted, err = repo.Create(dbent.NewTxContext(ctx, tx), &retry)
	require.NoError(t, err)
	require.False(t, inserted)

	var dailyTotal float64
	require.NoError(t, scanSingleRow(ctx, tx, `
		SELECT COALESCE(SUM(actual_cost), 0)
		FROM api_key_usage_daily
		WHERE api_key_id = $1
	`, []any{apiKey.ID}, &dailyTotal))
	require.InDelta(t, 3.25, dailyTotal, 1e-9)

	stats, err := repo.GetBatchAPIKeyUsageStats(ctx, []int64{apiKey.ID}, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.InDelta(t, 3.25, stats[apiKey.ID].TotalActualCost, 1e-9)
	require.InDelta(t, 1.25, stats[apiKey.ID].TodayActualCost, 1e-9)
}

func TestAPIKeyUsageDaily_NotReadyUsesLegacyQuery(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	today := timezone.Today()

	_, err := tx.ExecContext(ctx, `
		UPDATE api_key_usage_daily_state
		SET ready = FALSE, coverage_start = NULL, cursor = NULL, updated_at = NOW()
		WHERE id = 1
	`)
	require.NoError(t, err)

	user := mustCreateUser(t, client, &service.User{Email: "api-key-daily-fallback-" + uuid.NewString() + "@example.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-api-key-daily-fallback-" + uuid.NewString(), Name: "fallback"})
	account := mustCreateAccount(t, client, &service.Account{Name: "api-key-daily-fallback-" + uuid.NewString()})

	_, err = repo.Create(dbent.NewTxContext(ctx, tx), &service.UsageLog{
		UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
		RequestID: uuid.NewString(), Model: "test-model",
		TotalCost: 1.5, ActualCost: 1.5, CreatedAt: today.Add(time.Hour),
	})
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
		UPDATE api_key_usage_daily
		SET actual_cost = 99
		WHERE api_key_id = $1 AND bucket_date = CURRENT_DATE
	`, apiKey.ID)
	require.NoError(t, err)

	stats, err := repo.GetBatchAPIKeyUsageStats(ctx, []int64{apiKey.ID}, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.InDelta(t, 1.5, stats[apiKey.ID].TotalActualCost, 1e-9)
	require.InDelta(t, 1.5, stats[apiKey.ID].TodayActualCost, 1e-9)
}

func TestAPIKeyUsageDaily_BestEffortSingleFallbackWritesBucket(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()

	user := mustCreateUser(t, client, &service.User{Email: "api-key-daily-best-effort-" + uuid.NewString() + "@example.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-api-key-daily-best-effort-" + uuid.NewString(), Name: "fallback"})
	account := mustCreateAccount(t, client, &service.Account{Name: "api-key-daily-best-effort-" + uuid.NewString()})
	log := &service.UsageLog{
		UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
		RequestID: uuid.NewString(), Model: "test-model",
		TotalCost: 0.75, ActualCost: 0.75, CreatedAt: timezone.Today().Add(time.Hour),
	}

	require.NoError(t, execUsageLogInsertNoResult(ctx, tx, prepareUsageLogInsert(log)))
	require.NoError(t, execUsageLogInsertNoResult(ctx, tx, prepareUsageLogInsert(log)))

	var actualCost float64
	require.NoError(t, scanSingleRow(ctx, tx, `
		SELECT actual_cost
		FROM api_key_usage_daily
		WHERE api_key_id = $1 AND bucket_date = CURRENT_DATE
	`, []any{apiKey.ID}, &actualCost))
	require.InDelta(t, 0.75, actualCost, 1e-9)
}
