package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/pkg/userusageanalytics"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserUsageAnalyticsRangeIsSafe(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(30 * 24 * time.Hour)
	require.True(t, userUsageAnalyticsRangeIsSafe(start, end))
	require.False(t, userUsageAnalyticsRangeIsSafe(start.Add(30*time.Minute), end))
	require.False(t, userUsageAnalyticsRangeIsSafe(start, end.Add(15*time.Minute)))
	require.False(t, userUsageAnalyticsRangeIsSafe(end, start))
}

func TestBuildUserUsageAnalyticsWherePreservesLegacyRequestTypeAndBillingSemantics(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	billingType := int8(service.BillingTypeSubscription)
	stream := false

	requestTypes := []struct {
		name      string
		value     service.RequestType
		condition string
	}{
		{name: "unknown", value: service.RequestTypeUnknown, condition: "request_type = $7"},
		{name: "sync", value: service.RequestTypeSync, condition: "(request_type = $7 OR (request_type = 0 AND stream = FALSE AND openai_ws_mode = FALSE))"},
		{name: "stream", value: service.RequestTypeStream, condition: "(request_type = $7 OR (request_type = 0 AND stream = TRUE AND openai_ws_mode = FALSE))"},
		{name: "ws", value: service.RequestTypeWSV2, condition: "(request_type = $7 OR (request_type = 0 AND openai_ws_mode = TRUE))"},
		{name: "cyber", value: service.RequestTypeCyberBlocked, condition: "request_type = $7"},
	}

	for _, tt := range requestTypes {
		t.Run(tt.name, func(t *testing.T) {
			value := int16(tt.value)
			whereClause, args := buildUserUsageAnalyticsWhere(start, end, UsageLogFilters{
				UserID:            10,
				APIKeyID:          20,
				GroupID:           30,
				Model:             "gpt-5.4",
				ModelFilterSource: usagestats.ModelSourceRequested,
				RequestType:       &value,
				Stream:            &stream,
				BillingType:       &billingType,
				BillingMode:       " image ",
			})

			require.Contains(t, whereClause, tt.condition)
			require.NotContains(t, whereClause, "stream = $8")
			require.Contains(t, whereClause, "billing_mode = $9")
			require.Equal(t, []any{
				int64(10), start, end, int64(20), int64(30), "gpt-5.4",
				int16(tt.value), int16(billingType), "image",
			}, args)
		})
	}
}

func TestUserUsageAnalyticsTotalRequiresMarkedReadyCoverage(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	filters := UsageLogFilters{
		UserID:            42,
		ModelFilterSource: usagestats.ModelSourceRequested,
		StartTime:         &start,
		EndTime:           &end,
	}

	t.Run("unmarked", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		total, handled, err := newUserUsageAnalyticsStore(db).Total(context.Background(), filters)
		require.NoError(t, err)
		require.False(t, handled)
		require.Zero(t, total)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not ready", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		mock.ExpectQuery("FROM user_usage_analytics_hourly_state").
			WillReturnRows(sqlmock.NewRows([]string{"ready", "coverage_start"}).AddRow(false, nil))

		total, handled, err := newUserUsageAnalyticsStore(db).Total(userusageanalytics.Mark(context.Background()), filters)
		require.NoError(t, err)
		require.False(t, handled)
		require.Zero(t, total)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("coverage gap", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		mock.ExpectQuery("FROM user_usage_analytics_hourly_state").
			WillReturnRows(sqlmock.NewRows([]string{"ready", "coverage_start"}).AddRow(true, start.Add(time.Hour)))

		total, handled, err := newUserUsageAnalyticsStore(db).Total(userusageanalytics.Mark(context.Background()), filters)
		require.NoError(t, err)
		require.False(t, handled)
		require.Zero(t, total)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("ready and covered", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		mock.ExpectQuery("FROM user_usage_analytics_hourly_state").
			WillReturnRows(sqlmock.NewRows([]string{"ready", "coverage_start"}).AddRow(true, start.Add(-time.Hour)))
		mock.ExpectQuery("SELECT COALESCE\\(SUM\\(requests\\), 0\\)::BIGINT").
			WithArgs(int64(42), start, end).
			WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(int64(123)))

		total, handled, err := newUserUsageAnalyticsStore(db).Total(userusageanalytics.Mark(context.Background()), filters)
		require.NoError(t, err)
		require.True(t, handled)
		require.Equal(t, int64(123), total)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestUserUsageAnalyticsUnsupportedFiltersFallBackBeforeStateRead(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	mismatch := true

	tests := []UsageLogFilters{
		{UserID: 1, AccountID: 2, ModelFilterSource: usagestats.ModelSourceRequested, StartTime: &start, EndTime: &end},
		{UserID: 1, RequestID: "req", ModelFilterSource: usagestats.ModelSourceRequested, StartTime: &start, EndTime: &end},
		{UserID: 1, UpstreamModelMismatch: &mismatch, ModelFilterSource: usagestats.ModelSourceRequested, StartTime: &start, EndTime: &end},
		{UserID: 1, ModelFilterSource: usagestats.ModelSourceUpstream, StartTime: &start, EndTime: &end},
		{UserID: 1, BillingMode: "invalid", ModelFilterSource: usagestats.ModelSourceRequested, StartTime: &start, EndTime: &end},
		{UserID: 1, ModelFilterSource: usagestats.ModelSourceRequested, StartTime: timePointer(start.Add(30 * time.Minute)), EndTime: &end},
	}

	for _, filters := range tests {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		store := newUserUsageAnalyticsStore(db)
		_, handled, queryErr := store.Total(userusageanalytics.Mark(context.Background()), filters)
		require.NoError(t, queryErr)
		require.False(t, handled)
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func TestUserUsageAnalyticsRepositoryPreservesOptionalMethodSet(t *testing.T) {
	decorated := newUsageAggregationRepository(&usageLogRepository{}, sqlExecutor((*sql.DB)(nil)))

	_, ok := any(decorated).(interface {
		GetUsageTrendWithUsageFilters(context.Context, time.Time, time.Time, string, UsageLogFilters) ([]usagestats.TrendDataPoint, error)
		GetModelStatsWithUsageFiltersBySource(context.Context, time.Time, time.Time, UsageLogFilters, string) ([]usagestats.ModelStat, error)
		GetGroupStatsWithUsageFilters(context.Context, time.Time, time.Time, UsageLogFilters) ([]usagestats.GroupStat, error)
	})
	require.True(t, ok)
}

func TestUserUsageAnalyticsBackfillResumesFromStoredCursorAndMarksReady(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	store := newUserUsageAnalyticsStore(db)
	currentHour := time.Now().UTC().Truncate(time.Hour)
	currentHourEnd := currentHour.Add(time.Hour)

	// First step persists a resumable cursor and exits before any rebuild. This
	// is the state left behind if the process stops immediately afterwards.
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT pg_try_advisory_xact_lock").
		WithArgs(usageAggregationMaintenanceLockID).
		WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
	mock.ExpectQuery("SELECT pg_try_advisory_xact_lock").
		WithArgs(userUsageAnalyticsBackfillLockID).
		WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
	mock.ExpectExec("SET LOCAL max_parallel_workers_per_gather = 0").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SET LOCAL statement_timeout").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("FROM user_usage_analytics_hourly_state").
		WillReturnRows(sqlmock.NewRows([]string{"ready", "coverage_start", "cursor", "updated_at"}).
			AddRow(false, nil, nil, time.Now().Add(-time.Hour)))
	mock.ExpectQuery("SELECT MIN\\(created_at\\) FROM usage_logs").
		WillReturnRows(sqlmock.NewRows([]string{"min"}).AddRow(currentHour))
	mock.ExpectExec("SET ready = FALSE, coverage_start = \\$1, cursor = \\$1").
		WithArgs(currentHour).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	complete, err := store.backfillStep(context.Background())
	require.NoError(t, err)
	require.False(t, complete)

	// A later process resumes from that cursor. With historical coverage already
	// caught up, it performs the locked current-hour replacement, verifies it,
	// and atomically enables aggregate reads.
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT pg_try_advisory_xact_lock").
		WithArgs(usageAggregationMaintenanceLockID).
		WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
	mock.ExpectQuery("SELECT pg_try_advisory_xact_lock").
		WithArgs(userUsageAnalyticsBackfillLockID).
		WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
	mock.ExpectExec("SET LOCAL max_parallel_workers_per_gather = 0").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SET LOCAL statement_timeout").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("FROM user_usage_analytics_hourly_state").
		WillReturnRows(sqlmock.NewRows([]string{"ready", "coverage_start", "cursor", "updated_at"}).
			AddRow(false, currentHour, currentHour, time.Now().Add(-3*time.Minute)))
	mock.ExpectQuery("SELECT MIN\\(created_at\\) FROM usage_logs").
		WillReturnRows(sqlmock.NewRows([]string{"min"}).AddRow(currentHour))
	mock.ExpectExec("LOCK TABLE user_usage_analytics_hourly IN SHARE ROW EXCLUSIVE MODE").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM user_usage_analytics_hourly").
		WithArgs(currentHour, currentHourEnd).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO user_usage_analytics_hourly").
		WithArgs(currentHour, currentHourEnd).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("WITH raw AS").
		WithArgs(currentHour, currentHourEnd).
		WillReturnRows(sqlmock.NewRows([]string{"mismatch"}).AddRow(false))
	mock.ExpectExec("SET ready = TRUE, coverage_start = \\$1, cursor = \\$2").
		WithArgs(currentHour, currentHourEnd).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	complete, err = store.backfillStep(context.Background())
	require.NoError(t, err)
	require.True(t, complete)
	require.NoError(t, mock.ExpectationsWereMet())
}
