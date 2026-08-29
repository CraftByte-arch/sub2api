package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserDashboardStatsStoreFallsBackUntilReady(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery("FROM user_dashboard_route_daily_state").
		WillReturnRows(sqlmock.NewRows([]string{"ready"}).AddRow(false))

	stats, handled, err := newUserDashboardStatsStore(db).Get(context.Background(), 42)

	require.NoError(t, err)
	require.False(t, handled)
	require.Nil(t, stats)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserDashboardStatsStoreReadsAggregateAndPreservesWeightedDuration(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	const userID int64 = 42
	today := timezone.Today()

	mock.ExpectQuery("FROM user_dashboard_route_daily_state").
		WillReturnRows(sqlmock.NewRows([]string{"ready"}).AddRow(true))
	mock.ExpectQuery("FROM api_keys").
		WithArgs(userID, service.StatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"total_api_keys", "active_api_keys"}).AddRow(int64(3), int64(2)))
	mock.ExpectQuery(`(?s)WITH retention AS .*SELECT daily\.\*.*FROM user_dashboard_route_daily daily`).
		WithArgs(userID, today).
		WillReturnRows(sqlmock.NewRows([]string{
			"total_requests", "total_input_tokens", "total_output_tokens",
			"total_cache_creation_tokens", "total_cache_read_tokens",
			"total_cost", "total_actual_cost", "duration_sum_ms", "duration_count",
			"today_requests", "today_input_tokens", "today_output_tokens",
			"today_cache_creation_tokens", "today_cache_read_tokens",
			"today_cost", "today_actual_cost",
		}).AddRow(
			int64(4), int64(100), int64(50), int64(20), int64(10),
			2.5, 2.0, int64(900), int64(3),
			int64(1), int64(30), int64(10), int64(5), int64(2),
			0.7, 0.5,
		))
	mock.ExpectQuery("FROM usage_logs").
		WithArgs(sqlmock.AnyArg(), userID).
		WillReturnRows(sqlmock.NewRows([]string{"request_count", "token_count"}).AddRow(int64(15), int64(3000)))
	mock.ExpectQuery(`(?s)WITH retention AS .*routes AS .*FROM user_dashboard_route_daily daily`).
		WithArgs(userID, today).
		WillReturnRows(sqlmock.NewRows([]string{
			"platform", "total_requests", "total_tokens", "total_actual_cost",
			"today_requests", "today_tokens", "today_actual_cost",
		}).AddRow("anthropic", int64(3), int64(150), 1.8, int64(1), int64(47), 0.5))

	stats, handled, err := newUserDashboardStatsStore(db).Get(context.Background(), userID)

	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, int64(3), stats.TotalAPIKeys)
	require.Equal(t, int64(2), stats.ActiveAPIKeys)
	require.Equal(t, int64(180), stats.TotalTokens)
	require.Equal(t, int64(47), stats.TodayTokens)
	require.InDelta(t, 300, stats.AverageDurationMs, 1e-9)
	require.Equal(t, int64(3), stats.Rpm)
	require.Equal(t, int64(600), stats.Tpm)
	require.Len(t, stats.ByPlatform, 1)
	require.Equal(t, "anthropic", stats.ByPlatform[0].Platform)
	require.InDelta(t, 1.8, stats.ByPlatform[0].TotalActualCost, 1e-9)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestParseUserDashboardStatsDate(t *testing.T) {
	parsed, ok, err := parseUserDashboardStatsDate(sql.NullString{String: "2026-08-01", Valid: true})
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "2026-08-01", parsed.Format("2006-01-02"))

	parsed, ok, err = parseUserDashboardStatsDate(sql.NullString{})
	require.NoError(t, err)
	require.False(t, ok)
	require.True(t, parsed.IsZero())
}
