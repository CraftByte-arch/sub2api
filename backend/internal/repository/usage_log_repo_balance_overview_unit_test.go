//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLogRepositoryGetBalanceOverview(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	latestExpiry := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)WITH user_balances AS.*SUM\(GREATEST\(balance, 0\)\).*available_usage_cards AS.*SUM\(GREATEST\(c\.total_limit_usd - c\.used_usd, 0\)\).*MAX\(c\.expires_at\).*JOIN users u ON u\.id = c\.user_id AND u\.deleted_at IS NULL.*c\.status = \$1.*c\.starts_at <= \$2.*c\.expires_at > \$2.*c\.total_limit_usd NOT IN.*c\.used_usd NOT IN.*c\.total_limit_usd > 0.*c\.used_usd >= 0.*c\.used_usd < c\.total_limit_usd`).
		WithArgs(service.UsageCardStatusActive, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"total_available_balance",
			"total_usage_card_available_balance",
			"usage_card_latest_expires_at",
		}).AddRow(125.75, 38.5, latestExpiry))

	repo := &usageLogRepository{sql: db}
	overview, err := repo.GetBalanceOverview(context.Background())

	require.NoError(t, err)
	require.Equal(t, 125.75, overview.TotalAvailableBalance)
	require.Equal(t, 38.5, overview.TotalUsageCardAvailableBalance)
	require.NotNil(t, overview.UsageCardLatestExpiresAt)
	require.Equal(t, latestExpiry, *overview.UsageCardLatestExpiresAt)
	require.False(t, overview.GeneratedAt.IsZero())
	require.NoError(t, mock.ExpectationsWereMet())
}
