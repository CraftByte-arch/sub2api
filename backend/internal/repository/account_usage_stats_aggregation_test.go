package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

func TestBuildAccountUsageStatsResponsePreservesLegacyShapeAndWeightedDuration(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(48 * time.Hour)
	rows := []accountUsageStatsRow{
		{
			dimensionType: accountUsageStatsDimensionTotal, date: "2026-08-01",
			requests: 1, inputTokens: 10, outputTokens: 20,
			standardCost: 1, accountCost: 2, userCost: 3,
			durationSumMs: 100, durationCount: 1,
		},
		{
			dimensionType: accountUsageStatsDimensionTotal, date: "2026-08-02",
			requests: 3, inputTokens: 30, outputTokens: 60,
			standardCost: 4, accountCost: 5, userCost: 6,
			durationSumMs: 2700, durationCount: 3,
		},
		{
			dimensionType: accountUsageStatsDimensionModel, dimensionValue: "model-b",
			requests: 3, inputTokens: 30, outputTokens: 60, accountCost: 5,
		},
		{
			dimensionType: accountUsageStatsDimensionModel, dimensionValue: "model-a",
			requests: 1, inputTokens: 10, outputTokens: 20, accountCost: 2,
		},
		{
			dimensionType: accountUsageStatsDimensionInboundEndpoint, dimensionValue: "/v1/messages",
			requests: 4, inputTokens: 40, outputTokens: 80,
		},
		{
			dimensionType: accountUsageStatsDimensionUpstreamEndpoint, dimensionValue: "/v1/responses",
			requests: 4, inputTokens: 40, outputTokens: 80,
		},
	}

	response := buildAccountUsageStatsResponse(rows, start, end)

	require.Len(t, response.History, 2)
	require.Equal(t, int64(4), response.Summary.TotalRequests)
	require.Equal(t, int64(120), response.Summary.TotalTokens)
	require.InDelta(t, 700, response.Summary.AvgDurationMs, 1e-9)
	require.Equal(t, "model-b", response.Models[0].Model)
	require.Equal(t, "/v1/messages", response.Endpoints[0].Endpoint)
	require.Equal(t, "/v1/responses", response.UpstreamEndpoints[0].Endpoint)
}

func TestAccountUsageStatsGetFallsBackForInvalidRange(t *testing.T) {
	called := false
	legacy := func(_ context.Context, _ int64, _, _ time.Time) (*usagestats.AccountUsageStatsResponse, error) {
		called = true
		return &usagestats.AccountUsageStatsResponse{}, nil
	}

	store := newAccountUsageStatsStore(nil)
	now := time.Now()
	_, err := store.Get(context.Background(), 1, now, now, legacy)

	require.NoError(t, err)
	require.True(t, called)
}

func TestAccountUsageStatsWholeDayRange(t *testing.T) {
	loc := timezone.Location()
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 30)

	require.True(t, accountUsageStatsWholeDayRange(start, end))
	require.False(t, accountUsageStatsWholeDayRange(start.Add(time.Second), end))
	require.False(t, accountUsageStatsWholeDayRange(start, end.Add(-time.Second)))
}

func TestAccountUsageStatsGetUsesDailyTableForWholeDays(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	start := timezone.Today().AddDate(0, 0, -1)
	end := timezone.Today().AddDate(0, 0, 1)
	mock.ExpectQuery("FROM account_usage_stats_daily_state").
		WithArgs(start).
		WillReturnRows(sqlmock.NewRows([]string{"ready"}).AddRow(true))
	mock.ExpectQuery("FROM account_usage_stats_daily").
		WithArgs(int64(42), start, end).
		WillReturnRows(sqlmock.NewRows([]string{
			"dimension_type", "dimension_value", "date", "requests",
			"input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
			"standard_cost", "account_cost", "user_cost", "duration_sum_ms", "duration_count",
		}).AddRow(
			accountUsageStatsDimensionTotal, "", start.Format("2006-01-02"), int64(2),
			int64(10), int64(20), int64(0), int64(0),
			1.0, 2.0, 1.5, int64(600), int64(2),
		))

	legacyCalled := false
	response, err := newAccountUsageStatsStore(db).Get(
		context.Background(),
		42,
		start,
		end,
		func(context.Context, int64, time.Time, time.Time) (*usagestats.AccountUsageStatsResponse, error) {
			legacyCalled = true
			return nil, nil
		},
	)

	require.NoError(t, err)
	require.False(t, legacyCalled)
	require.Equal(t, int64(2), response.Summary.TotalRequests)
	require.InDelta(t, 300, response.Summary.AvgDurationMs, 1e-9)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountUsageStatsGetFallsBackForPartialDay(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	start := timezone.Today().Add(time.Hour)
	end := timezone.Today().AddDate(0, 0, 1)
	want := &usagestats.AccountUsageStatsResponse{}
	legacyCalled := false
	response, err := newAccountUsageStatsStore(db).Get(
		context.Background(),
		42,
		start,
		end,
		func(context.Context, int64, time.Time, time.Time) (*usagestats.AccountUsageStatsResponse, error) {
			legacyCalled = true
			return want, nil
		},
	)

	require.NoError(t, err)
	require.True(t, legacyCalled)
	require.Same(t, want, response)
	require.NoError(t, mock.ExpectationsWereMet())
}
