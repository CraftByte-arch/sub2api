//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestAccountUsageStatsAggregateTriggerMatchesLegacyAndHandlesDelete(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	base := timezone.Today().AddDate(0, 0, -1)
	end := timezone.Today().AddDate(0, 0, 1)
	_, err := tx.ExecContext(ctx, `
		UPDATE account_usage_stats_daily_state
		SET ready = TRUE, coverage_start = $1::date, cursor = $2::date, updated_at = NOW()
		WHERE id = 1
	`, base.AddDate(0, 0, -1), end)
	require.NoError(t, err)

	user := mustCreateUser(t, client, &service.User{Email: "account-stats-" + uuid.NewString() + "@example.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-account-stats-" + uuid.NewString(), Name: "stats"})
	account := mustCreateAccount(t, client, &service.Account{Name: "account-stats-" + uuid.NewString()})

	durationA, durationB := 100, 400
	inboundA, inboundB := "/v1/messages", "/v1/responses"
	upstreamA, upstreamB := "/v1/messages", "/v1/chat/completions"
	accountRate := 2.0
	accountStatsCost := 0.4
	first := &service.UsageLog{
		UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
		RequestID: uuid.NewString(), Model: "billing-a", RequestedModel: "requested-a",
		InputTokens: 10, OutputTokens: 20, CacheCreationTokens: 3, CacheReadTokens: 4,
		TotalCost: 0.5, ActualCost: 0.3, AccountRateMultiplier: &accountRate, AccountStatsCost: &accountStatsCost,
		DurationMs: &durationA, InboundEndpoint: &inboundA, UpstreamEndpoint: &upstreamA,
		CreatedAt: base.Add(15 * time.Minute),
	}
	inserted, err := repo.Create(ctx, first)
	require.NoError(t, err)
	require.True(t, inserted)

	second := &service.UsageLog{
		UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
		RequestID: uuid.NewString(), Model: "requested-b",
		InputTokens: 30, OutputTokens: 40, CacheCreationTokens: 5, CacheReadTokens: 6,
		TotalCost: 0.7, ActualCost: 0.6,
		DurationMs: &durationB, InboundEndpoint: &inboundB, UpstreamEndpoint: &upstreamB,
		CreatedAt: base.AddDate(0, 0, 1).Add(30 * time.Minute),
	}
	inserted, err = repo.Create(ctx, second)
	require.NoError(t, err)
	require.True(t, inserted)

	retry := *second
	retry.ID = 0
	inserted, err = repo.Create(ctx, &retry)
	require.NoError(t, err)
	require.False(t, inserted)

	start := base
	legacy, err := repo.GetAccountUsageStats(ctx, account.ID, start, end)
	require.NoError(t, err)
	aggregated, err := repo.GetAccountUsageStatsAggregated(ctx, account.ID, start, end)
	require.NoError(t, err)
	requireAccountUsageStatsEquivalent(t, legacy, aggregated)
	require.InDelta(t, 250, aggregated.Summary.AvgDurationMs, 1e-9)

	require.NoError(t, repo.Delete(ctx, first.ID))
	legacy, err = repo.GetAccountUsageStats(ctx, account.ID, start, end)
	require.NoError(t, err)
	aggregated, err = repo.GetAccountUsageStatsAggregated(ctx, account.ID, start, end)
	require.NoError(t, err)
	requireAccountUsageStatsEquivalent(t, legacy, aggregated)
}

func requireAccountUsageStatsEquivalent(t *testing.T, expected, actual *usagestats.AccountUsageStatsResponse) {
	t.Helper()
	require.Equal(t, expected.History, actual.History)
	require.Equal(t, expected.Summary, actual.Summary)
	require.ElementsMatch(t, expected.Models, actual.Models)
	require.ElementsMatch(t, expected.Endpoints, actual.Endpoints)
	require.ElementsMatch(t, expected.UpstreamEndpoints, actual.UpstreamEndpoints)
}
