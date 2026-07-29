//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAggregateAdminPaymentAggregationGroupsUsersCurrenciesAndPeriods(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	start := time.Date(2026, time.July, 27, 0, 0, 0, 0, location)
	window := adminPaymentAggregationWindow{
		StartDate:  "2026-07-27",
		EndDate:    "2026-07-29",
		Timezone:   "Asia/Shanghai",
		Location:   location,
		StartLocal: start,
		EndLocal:   start.AddDate(0, 0, 2),
	}
	rows := []adminPaymentAggregationRow{
		{UserID: 1, UserEmail: "alice@example.com", Currency: "CNY", PayAmount: 10, EventAt: start.Add(2 * time.Hour)},
		{UserID: 1, UserEmail: "alice@example.com", Currency: "CNY", PayAmount: 5.55, EventAt: start.AddDate(0, 0, 1).Add(2 * time.Hour)},
		{UserID: 2, UserEmail: "bob@example.com", Currency: "CNY", PayAmount: 30, EventAt: start.AddDate(0, 0, 1).Add(3 * time.Hour)},
		{UserID: 1, UserEmail: "alice@example.com", Currency: "USD", PayAmount: 20, EventAt: start.AddDate(0, 0, 2).Add(3 * time.Hour)},
	}

	got := aggregateAdminPaymentAggregation(rows, window, "day", 1, 20)

	require.Equal(t, 4, got.Summary.OrderCount)
	require.Equal(t, 2, got.Summary.UserCount)
	require.Equal(t, CurrencyAmounts{"CNY": 45.55, "USD": 20}, got.Summary.TotalAmount)
	require.Equal(t, CurrencyAmounts{"CNY": 15.18, "USD": 20}, got.Summary.AverageAmount)
	require.Len(t, got.Timeline, 3)
	require.Equal(t, "2026-07-27", got.Timeline[0].Period)
	require.Equal(t, 1, got.Timeline[0].OrderCount)
	require.Equal(t, 3, len(got.Users))
	require.Equal(t, int64(2), got.Users[0].UserID)
	require.Equal(t, "CNY", got.Users[0].Currency)
	require.Equal(t, int64(1), got.Users[1].UserID)
	require.Equal(t, "USD", got.Users[1].Currency)
}

func TestAdminPaymentAggregationPeriodSupportsWeekAndMonth(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	at := time.Date(2026, time.July, 29, 12, 0, 0, 0, location)

	week, weekStart, weekEnd := adminPaymentAggregationPeriod(at, "week", location)
	require.Equal(t, "2026-W31", week)
	require.Equal(t, "2026-07-27", weekStart.Format(adminPaymentAggregationDateLayout))
	require.Equal(t, "2026-08-03", weekEnd.Format(adminPaymentAggregationDateLayout))

	month, monthStart, monthEnd := adminPaymentAggregationPeriod(at, "month", location)
	require.Equal(t, "2026-07", month)
	require.Equal(t, "2026-07-01", monthStart.Format(adminPaymentAggregationDateLayout))
	require.Equal(t, "2026-08-01", monthEnd.Format(adminPaymentAggregationDateLayout))
}
