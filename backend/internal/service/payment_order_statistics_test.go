//go:build unit

package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
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

func TestPaymentServiceGetUserOrderStatisticsFiltersAndAggregates(t *testing.T) {
	client, service := newPaymentStatisticsTestService(t)
	ctx := context.Background()
	user := createPaymentStatisticsUser(t, client, "owner@example.com")
	otherUser := createPaymentStatisticsUser(t, client, "other@example.com")

	createPaymentStatisticsOrder(t, client, user, paymentStatisticsOrderSpec{
		OutTradeNo: "paid-balance", Status: OrderStatusPaid, OrderType: payment.OrderTypeBalance,
		PayAmount: 10.10, PaidAt: timePointer(time.Date(2026, 7, 19, 16, 30, 0, 0, time.UTC)),
	})
	createPaymentStatisticsOrder(t, client, user, paymentStatisticsOrderSpec{
		OutTradeNo: "recharging-card", Status: OrderStatusRecharging, OrderType: payment.OrderTypeUsageCard,
		PayAmount: 20.20, PaidAt: timePointer(time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC)),
	})
	createPaymentStatisticsOrder(t, client, user, paymentStatisticsOrderSpec{
		OutTradeNo: "completed-subscription", Status: OrderStatusCompleted, OrderType: payment.OrderTypeSubscription,
		PayAmount: 30.30, PaidAt: timePointer(time.Date(2026, 7, 20, 17, 0, 0, 0, time.UTC)),
	})
	createPaymentStatisticsOrder(t, client, user, paymentStatisticsOrderSpec{
		OutTradeNo: "pending", Status: OrderStatusPending, OrderType: payment.OrderTypeBalance,
		PayAmount: 40, PaidAt: timePointer(time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC)),
	})
	createPaymentStatisticsOrder(t, client, user, paymentStatisticsOrderSpec{
		OutTradeNo: "missing-paid-at", Status: OrderStatusPaid, OrderType: payment.OrderTypeBalance, PayAmount: 50,
	})
	createPaymentStatisticsOrder(t, client, user, paymentStatisticsOrderSpec{
		OutTradeNo: "unsupported-type", Status: OrderStatusPaid, OrderType: "future_type",
		PayAmount: 60, PaidAt: timePointer(time.Date(2026, 7, 20, 3, 0, 0, 0, time.UTC)),
	})
	createPaymentStatisticsOrder(t, client, user, paymentStatisticsOrderSpec{
		OutTradeNo: "outside-range", Status: OrderStatusPaid, OrderType: payment.OrderTypeBalance,
		PayAmount: 70, PaidAt: timePointer(time.Date(2026, 6, 30, 15, 59, 59, 0, time.UTC)),
	})
	createPaymentStatisticsOrder(t, client, otherUser, paymentStatisticsOrderSpec{
		OutTradeNo: "other-user", Status: OrderStatusCompleted, OrderType: payment.OrderTypeBalance,
		PayAmount: 999, PaidAt: timePointer(time.Date(2026, 7, 20, 4, 0, 0, 0, time.UTC)),
	})

	stats, err := service.GetUserOrderStatistics(ctx, user.ID, OrderStatisticsQuery{
		StartDate: "2026-07-01",
		EndDate:   "2026-07-21",
		Timezone:  "Asia/Shanghai",
	})

	require.NoError(t, err)
	require.Equal(t, "2026-07-01", stats.StartDate)
	require.Equal(t, "2026-07-21", stats.EndDate)
	require.Equal(t, "Asia/Shanghai", stats.Timezone)
	require.Equal(t, "CNY", stats.Currency)
	require.Equal(t, 3, stats.Summary.OrderCount)
	require.Equal(t, 60.60, stats.Summary.TotalPaidAmount)
	require.Equal(t, 20.20, stats.Summary.AveragePaidAmount)
	require.Equal(t, []DailyOrderStatistics{
		{Date: "2026-07-21", TotalPaidAmount: 30.30, OrderCount: 1, AveragePaidAmount: 30.30},
		{Date: "2026-07-20", TotalPaidAmount: 30.30, OrderCount: 2, AveragePaidAmount: 15.15},
	}, stats.Daily)

	for _, aggregate := range stats.ByType {
		items, total, err := service.GetUserOrderStatisticsDetails(ctx, user.ID, OrderStatisticsDetailsQuery{
			OrderStatisticsQuery: OrderStatisticsQuery{
				StartDate: "2026-07-01",
				EndDate:   "2026-07-21",
				Timezone:  "Asia/Shanghai",
			},
			Page:      1,
			OrderType: aggregate.OrderType,
		})
		require.NoError(t, err)
		require.Equal(t, aggregate.OrderCount, total)
		require.Len(t, items, aggregate.OrderCount)
	}

	for _, aggregate := range stats.Daily {
		items, total, err := service.GetUserOrderStatisticsDetails(ctx, user.ID, OrderStatisticsDetailsQuery{
			OrderStatisticsQuery: OrderStatisticsQuery{
				StartDate: "2026-07-01",
				EndDate:   "2026-07-21",
				Timezone:  "Asia/Shanghai",
			},
			Page: 1,
			Date: aggregate.Date,
		})
		require.NoError(t, err)
		require.Equal(t, aggregate.OrderCount, total)
		require.Len(t, items, aggregate.OrderCount)
	}
}

func TestPaymentServiceGetUserOrderStatisticsDetailsUsesFixedStablePagination(t *testing.T) {
	client, service := newPaymentStatisticsTestService(t)
	ctx := context.Background()
	user := createPaymentStatisticsUser(t, client, "pagination@example.com")
	paidAt := time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC)

	for index := 0; index < 22; index++ {
		createPaymentStatisticsOrder(t, client, user, paymentStatisticsOrderSpec{
			OutTradeNo: fmt.Sprintf("detail-%02d", index),
			Status:     OrderStatusCompleted,
			OrderType:  payment.OrderTypeBalance,
			PayAmount:  float64(index) + 0.25,
			PaidAt:     timePointer(paidAt),
		})
	}

	query := OrderStatisticsDetailsQuery{
		OrderStatisticsQuery: OrderStatisticsQuery{
			StartDate: "2026-07-20",
			EndDate:   "2026-07-20",
			Timezone:  "Asia/Shanghai",
		},
		Page:      1,
		OrderType: payment.OrderTypeBalance,
	}
	firstPage, total, err := service.GetUserOrderStatisticsDetails(ctx, user.ID, query)
	require.NoError(t, err)
	require.Equal(t, 22, total)
	require.Len(t, firstPage, OrderStatisticsDetailPageSize)
	require.Equal(t, "detail-21", firstPage[0].OutTradeNo)
	require.Equal(t, "detail-02", firstPage[len(firstPage)-1].OutTradeNo)
	require.Equal(t, OrderStatusCompleted, firstPage[0].Status)
	require.Equal(t, payment.TypeAlipay, firstPage[0].PaymentType)
	require.Equal(t, paidAt, firstPage[0].PaidAt)

	query.Page = 2
	secondPage, total, err := service.GetUserOrderStatisticsDetails(ctx, user.ID, query)
	require.NoError(t, err)
	require.Equal(t, 22, total)
	require.Equal(t, []string{"detail-01", "detail-00"}, []string{
		secondPage[0].OutTradeNo,
		secondPage[1].OutTradeNo,
	})
}

func TestPaymentServiceGetUserOrderStatisticsDetailsRejectsInvalidSelectors(t *testing.T) {
	_, service := newPaymentStatisticsTestService(t)
	ctx := context.Background()
	base := OrderStatisticsQuery{
		StartDate: "2026-07-01",
		EndDate:   "2026-07-20",
		Timezone:  "Asia/Shanghai",
	}
	tests := []struct {
		name  string
		query OrderStatisticsDetailsQuery
	}{
		{name: "neither selector", query: OrderStatisticsDetailsQuery{OrderStatisticsQuery: base, Page: 1}},
		{name: "both selectors", query: OrderStatisticsDetailsQuery{OrderStatisticsQuery: base, Page: 1, OrderType: payment.OrderTypeBalance, Date: "2026-07-20"}},
		{name: "unsupported type", query: OrderStatisticsDetailsQuery{OrderStatisticsQuery: base, Page: 1, OrderType: "future_type"}},
		{name: "invalid date", query: OrderStatisticsDetailsQuery{OrderStatisticsQuery: base, Page: 1, Date: "2026-02-30"}},
		{name: "date before range", query: OrderStatisticsDetailsQuery{OrderStatisticsQuery: base, Page: 1, Date: "2026-06-30"}},
		{name: "date after range", query: OrderStatisticsDetailsQuery{OrderStatisticsQuery: base, Page: 1, Date: "2026-07-21"}},
		{name: "invalid page", query: OrderStatisticsDetailsQuery{OrderStatisticsQuery: base, Page: 0, OrderType: payment.OrderTypeBalance}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := service.GetUserOrderStatisticsDetails(ctx, 1, tt.query)
			require.Error(t, err)
		})
	}
}

type paymentStatisticsOrderSpec struct {
	OutTradeNo string
	Status     string
	OrderType  string
	PayAmount  float64
	PaidAt     *time.Time
}

func newPaymentStatisticsTestService(t *testing.T) (*dbent.Client, *PaymentService) {
	t.Helper()
	databaseName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	database, err := sql.Open("sqlite", "file:"+databaseName+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	_, err = database.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	driver := entsql.OpenDB(dialect.SQLite, database)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(driver)))
	t.Cleanup(func() { _ = client.Close() })
	return client, &PaymentService{entClient: client}
}

func createPaymentStatisticsUser(t *testing.T, client *dbent.Client, email string) *dbent.User {
	t.Helper()
	user, err := client.User.Create().
		SetEmail(email).
		SetPasswordHash("hash").
		SetUsername(strings.TrimSuffix(email, "@example.com")).
		Save(context.Background())
	require.NoError(t, err)
	return user
}

func createPaymentStatisticsOrder(t *testing.T, client *dbent.Client, user *dbent.User, spec paymentStatisticsOrderSpec) *dbent.PaymentOrder {
	t.Helper()
	builder := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(spec.PayAmount).
		SetPayAmount(spec.PayAmount).
		SetFeeRate(0).
		SetRechargeCode("TEST-" + spec.OutTradeNo).
		SetOutTradeNo(spec.OutTradeNo).
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("TRADE-" + spec.OutTradeNo).
		SetOrderType(spec.OrderType).
		SetStatus(spec.Status).
		SetExpiresAt(time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)).
		SetClientIP("127.0.0.1").
		SetSrcHost("example.test")
	if spec.PaidAt != nil {
		builder.SetPaidAt(*spec.PaidAt)
	}
	order, err := builder.Save(context.Background())
	require.NoError(t, err)
	return order
}

func timePointer(value time.Time) *time.Time {
	return &value
}
