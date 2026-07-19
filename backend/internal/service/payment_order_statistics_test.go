//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestParseOrderStatisticsWindowDefaultsToThirtyLocalDays(t *testing.T) {
	now := time.Date(2026, 7, 20, 4, 30, 0, 0, time.UTC)

	window, err := parseOrderStatisticsWindow(OrderStatisticsQuery{
		Timezone: "Asia/Shanghai",
	}, now)

	require.NoError(t, err)
	require.Equal(t, "2026-06-21", window.StartDate)
	require.Equal(t, "2026-07-20", window.EndDate)
	require.Equal(t, "Asia/Shanghai", window.Timezone)
	require.Equal(t, "2026-06-21T00:00:00+08:00", window.StartInclusive.Format(time.RFC3339))
	require.Equal(t, "2026-07-21T00:00:00+08:00", window.EndExclusive.Format(time.RFC3339))
}

func TestParseOrderStatisticsWindowAcceptsInclusiveMaximum(t *testing.T) {
	window, err := parseOrderStatisticsWindow(OrderStatisticsQuery{
		StartDate: "2025-07-20",
		EndDate:   "2026-07-20",
		Timezone:  "Asia/Shanghai",
	}, time.Date(2026, 7, 20, 4, 30, 0, 0, time.UTC))

	require.NoError(t, err)
	require.Equal(t, "2025-07-20", window.StartDate)
	require.Equal(t, "2026-07-20", window.EndDate)
}

func TestParseOrderStatisticsWindowRejectsInvalidInput(t *testing.T) {
	now := time.Date(2026, 7, 20, 4, 30, 0, 0, time.UTC)
	tests := []struct {
		name  string
		query OrderStatisticsQuery
	}{
		{name: "missing end", query: OrderStatisticsQuery{StartDate: "2026-07-01", Timezone: "Asia/Shanghai"}},
		{name: "missing start", query: OrderStatisticsQuery{EndDate: "2026-07-20", Timezone: "Asia/Shanghai"}},
		{name: "invalid start", query: OrderStatisticsQuery{StartDate: "2026-02-30", EndDate: "2026-07-20", Timezone: "Asia/Shanghai"}},
		{name: "reverse range", query: OrderStatisticsQuery{StartDate: "2026-07-21", EndDate: "2026-07-20", Timezone: "Asia/Shanghai"}},
		{name: "more than 366 days", query: OrderStatisticsQuery{StartDate: "2025-07-19", EndDate: "2026-07-20", Timezone: "Asia/Shanghai"}},
		{name: "invalid timezone", query: OrderStatisticsQuery{StartDate: "2026-07-01", EndDate: "2026-07-20", Timezone: "Mars/Olympus"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseOrderStatisticsWindow(tt.query, now)
			require.Error(t, err)
		})
	}
}

func TestParseOrderStatisticsWindowUsesAdjacentLocalMidnightsAcrossDST(t *testing.T) {
	tests := []struct {
		name     string
		date     string
		duration time.Duration
	}{
		{name: "spring forward", date: "2026-03-08", duration: 23 * time.Hour},
		{name: "fall back", date: "2026-11-01", duration: 25 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			window, err := parseOrderStatisticsWindow(OrderStatisticsQuery{
				StartDate: tt.date,
				EndDate:   tt.date,
				Timezone:  "America/New_York",
			}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

			require.NoError(t, err)
			require.Equal(t, tt.duration, window.EndExclusive.Sub(window.StartInclusive))
		})
	}
}

func TestAggregateOrderStatisticsUsesIntegerCents(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)

	stats := aggregateOrderStatistics([]orderStatisticsRow{
		{
			ID:        1,
			PayAmount: 10.10,
			OrderType: payment.OrderTypeBalance,
			PaidAt:    time.Date(2026, 7, 19, 16, 30, 0, 0, time.UTC),
		},
		{
			ID:        2,
			PayAmount: 20.20,
			OrderType: payment.OrderTypeUsageCard,
			PaidAt:    time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC),
		},
		{
			ID:        3,
			PayAmount: 0.30,
			OrderType: payment.OrderTypeUsageCard,
			PaidAt:    time.Date(2026, 7, 19, 15, 30, 0, 0, time.UTC),
		},
	}, location)

	require.Equal(t, "CNY", stats.Currency)
	require.Equal(t, 30.60, stats.Summary.TotalPaidAmount)
	require.Equal(t, 3, stats.Summary.OrderCount)
	require.Equal(t, 10.20, stats.Summary.AveragePaidAmount)
	require.Equal(t, []OrderTypeStatistics{
		{
			OrderType:         payment.OrderTypeBalance,
			TotalPaidAmount:   10.10,
			OrderCount:        1,
			AveragePaidAmount: 10.10,
		},
		{
			OrderType:         payment.OrderTypeUsageCard,
			TotalPaidAmount:   20.50,
			OrderCount:        2,
			AveragePaidAmount: 10.25,
		},
		{
			OrderType:         payment.OrderTypeSubscription,
			TotalPaidAmount:   0,
			OrderCount:        0,
			AveragePaidAmount: 0,
		},
	}, stats.ByType)
	require.Equal(t, []DailyOrderStatistics{
		{Date: "2026-07-20", TotalPaidAmount: 30.30, OrderCount: 2, AveragePaidAmount: 15.15},
		{Date: "2026-07-19", TotalPaidAmount: 0.30, OrderCount: 1, AveragePaidAmount: 0.30},
	}, stats.Daily)
}

func TestAggregateOrderStatisticsIgnoresUnsupportedTypesAndKeepsStableEmptyShape(t *testing.T) {
	stats := aggregateOrderStatistics([]orderStatisticsRow{
		{
			ID:        1,
			PayAmount: 99.99,
			OrderType: "future_type",
			PaidAt:    time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		},
	}, time.UTC)

	require.Equal(t, OrderStatisticsSummary{}, stats.Summary)
	require.Len(t, stats.ByType, 3)
	for _, row := range stats.ByType {
		require.Zero(t, row.TotalPaidAmount)
		require.Zero(t, row.OrderCount)
		require.Zero(t, row.AveragePaidAmount)
	}
	require.NotNil(t, stats.Daily)
	require.Empty(t, stats.Daily)
}

func TestAggregateOrderStatisticsAvoidsFloatAccumulationDrift(t *testing.T) {
	rows := make([]orderStatisticsRow, 10)
	for i := range rows {
		rows[i] = orderStatisticsRow{
			ID:        int64(i + 1),
			PayAmount: 0.10,
			OrderType: payment.OrderTypeBalance,
			PaidAt:    time.Date(2026, 7, 20, 0, 0, i, 0, time.UTC),
		}
	}

	stats := aggregateOrderStatistics(rows, time.UTC)

	require.Equal(t, 1.00, stats.Summary.TotalPaidAmount)
	require.Equal(t, 0.10, stats.Summary.AveragePaidAmount)
}
