//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserDashboardRouteDailyTriggerAndAggregateRead(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "user-dashboard-route-daily@test.com"})
	group := mustCreateGroup(t, client, &service.Group{Name: "user-dashboard-route-daily", Platform: service.PlatformOpenAI})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-user-dashboard-route-daily", Name: "dashboard"})
	account := mustCreateAccount(t, client, &service.Account{Name: "user-dashboard-route-daily", Platform: service.PlatformAnthropic})
	duration := 300
	groupID := group.ID
	usage := &service.UsageLog{
		UserID:              user.ID,
		APIKeyID:            apiKey.ID,
		AccountID:           account.ID,
		GroupID:             &groupID,
		RequestID:           "req-user-dashboard-route-daily",
		Model:               "gpt-5",
		InputTokens:         10,
		OutputTokens:        20,
		CacheCreationTokens: 5,
		CacheReadTokens:     2,
		TotalCost:           1.25,
		ActualCost:          1.0,
		DurationMs:          &duration,
		CreatedAt:           time.Now(),
	}
	_, err := repo.Create(ctx, usage)
	require.NoError(t, err)
	today := timezone.Today()
	require.NoError(t, repo.userDashboardStats.rebuildDay(ctx, tx, today, today.AddDate(0, 0, 1)))
	require.NoError(t, repo.userDashboardStats.verifyDay(ctx, tx, today, today.AddDate(0, 0, 1)))

	_, err = tx.ExecContext(ctx, `
		UPDATE user_dashboard_route_daily_state
		SET ready = TRUE, coverage_start = CURRENT_DATE, cursor = CURRENT_DATE + 1
		WHERE id = 1
	`)
	require.NoError(t, err)

	stats, handled, err := repo.userDashboardStats.Get(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, int64(1), stats.TotalRequests)
	require.Equal(t, int64(37), stats.TotalTokens)
	require.InDelta(t, 300, stats.AverageDurationMs, 1e-9)
	require.Len(t, stats.ByPlatform, 1)
	require.Equal(t, service.PlatformOpenAI, stats.ByPlatform[0].Platform)

	_, err = tx.ExecContext(ctx, `
		UPDATE usage_logs
		SET input_tokens = 20, actual_cost = 0
		WHERE id = $1
	`, usage.ID)
	require.NoError(t, err)

	stats, handled, err = repo.userDashboardStats.Get(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, int64(1), stats.TotalRequests)
	require.Equal(t, int64(47), stats.TotalTokens)
	require.Empty(t, stats.ByPlatform)

	require.NoError(t, repo.Delete(ctx, usage.ID))
	stats, handled, err = repo.userDashboardStats.Get(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, handled)
	require.Zero(t, stats.TotalRequests)
	require.Zero(t, stats.TotalTokens)
}
