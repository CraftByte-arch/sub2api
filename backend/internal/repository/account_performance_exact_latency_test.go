package repository

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountPerformanceExactOverviewLatencyUsesUsageLogPercentilesAndCoverage(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	start := time.Date(2026, 7, 18, 0, 42, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	mock.ExpectQuery("(?s)WITH aggregate_source.*account_performance_hourly.*account_performance_minute.*coverage.*bucket_width.*usage_logs.*upstream_endpoint.*inbound_endpoint.*percentile_cont").
		WillReturnRows(sqlmock.NewRows([]string{"row_kind", "bucket_start", "p50_ttft_ms", "p95_ttft_ms", "p95_duration_ms", "ttft_samples", "duration_samples"}).
			AddRow(0, nil, 3516.0, 31454.0, 62768.0, int64(10), int64(10)).
			AddRow(1, driver.Value(start), 3516.0, 31454.0, 62768.0, int64(10), int64(10)))

	repo := &accountPerformanceRepository{db: db}
	result, err := repo.QueryExactOverviewLatency(context.Background(), service.AccountPerformanceOverviewFilter{
		Start:    start,
		End:      end,
		Protocol: "responses",
	})
	require.NoError(t, err)
	require.EqualValues(t, 3516, result.Summary.P50TTFTMS)
	require.EqualValues(t, 31454, result.Summary.P95TTFTMS)
	require.EqualValues(t, 62768, result.Summary.P95DurationMS)
	require.EqualValues(t, 10, result.Summary.TTFTSampleCount)
	require.EqualValues(t, 10, result.Summary.DurationSamples)
	require.Equal(t, service.AccountPerformanceExactLatency{
		P50TTFTMS: 3516, P95TTFTMS: 31454, P95DurationMS: 62768, TTFTSampleCount: 10, DurationSamples: 10,
	}, result.Trend[start])
	require.NoError(t, mock.ExpectationsWereMet())
}
