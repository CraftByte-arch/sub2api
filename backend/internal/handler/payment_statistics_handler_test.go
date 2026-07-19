//go:build unit

package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func TestPaymentStatisticsRequiresAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewPaymentHandler(nil, nil, nil, nil)

	tests := []struct {
		name   string
		target string
		invoke func(*gin.Context)
	}{
		{name: "summary", target: "/api/v1/payment/orders/statistics", invoke: handler.GetOrderStatistics},
		{name: "details", target: "/api/v1/payment/orders/statistics/details?order_type=balance", invoke: handler.GetOrderStatisticsDetails},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, recorder := newPaymentStatisticsContext(tt.target, 0)
			tt.invoke(ctx)
			require.Equal(t, http.StatusUnauthorized, recorder.Code)
		})
	}
}

func TestPaymentStatisticsUsesAuthenticatedUserAndReturnsCNYSummary(t *testing.T) {
	client, handler := newPaymentStatisticsHandlerHarness(t)
	owner := createPaymentStatisticsHandlerUser(t, client, "handler-owner@example.com")
	other := createPaymentStatisticsHandlerUser(t, client, "handler-other@example.com")
	paidAt := time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC)
	createPaymentStatisticsHandlerOrder(t, client, owner, "owner-order", 12.34, paidAt)
	createPaymentStatisticsHandlerOrder(t, client, other, "other-order", 999, paidAt)

	target := "/api/v1/payment/orders/statistics?start_date=2026-07-20&end_date=2026-07-20&timezone=Asia%2FShanghai&user_id=" + strconv.FormatInt(other.ID, 10)
	ctx, recorder := newPaymentStatisticsContext(target, owner.ID)
	handler.GetOrderStatistics(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Code int                             `json:"code"`
		Data service.OrderStatisticsResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.Equal(t, "CNY", envelope.Data.Currency)
	require.Equal(t, 1, envelope.Data.Summary.OrderCount)
	require.Equal(t, 12.34, envelope.Data.Summary.TotalPaidAmount)
}

func TestPaymentStatisticsRejectsInvalidTimezone(t *testing.T) {
	_, handler := newPaymentStatisticsHandlerHarness(t)
	ctx, recorder := newPaymentStatisticsContext(
		"/api/v1/payment/orders/statistics?start_date=2026-07-01&end_date=2026-07-20&timezone=Mars%2FOlympus",
		1,
	)

	handler.GetOrderStatistics(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestPaymentStatisticsDetailsReturnsFixedMinimalPage(t *testing.T) {
	client, handler := newPaymentStatisticsHandlerHarness(t)
	owner := createPaymentStatisticsHandlerUser(t, client, "details-owner@example.com")
	paidAt := time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC)
	createPaymentStatisticsHandlerOrder(t, client, owner, "detail-order", 18.50, paidAt)

	ctx, recorder := newPaymentStatisticsContext(
		"/api/v1/payment/orders/statistics/details?start_date=2026-07-20&end_date=2026-07-20&timezone=Asia%2FShanghai&order_type=balance&page=1&page_size=100",
		owner.ID,
	)
	handler.GetOrderStatisticsDetails(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Items    []map[string]any `json:"items"`
			Total    int              `json:"total"`
			Page     int              `json:"page"`
			PageSize int              `json:"page_size"`
			Pages    int              `json:"pages"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.Equal(t, 1, envelope.Data.Total)
	require.Equal(t, 1, envelope.Data.Page)
	require.Equal(t, service.OrderStatisticsDetailPageSize, envelope.Data.PageSize)
	require.Equal(t, 1, envelope.Data.Pages)
	require.Len(t, envelope.Data.Items, 1)

	keys := make([]string, 0, len(envelope.Data.Items[0]))
	for key := range envelope.Data.Items[0] {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	require.Equal(t, []string{
		"order_type",
		"out_trade_no",
		"paid_at",
		"pay_amount",
		"payment_type",
		"status",
	}, keys)
}

func TestPaymentStatisticsDetailsRejectsInvalidPageAndSelectors(t *testing.T) {
	_, handler := newPaymentStatisticsHandlerHarness(t)
	tests := []string{
		"/api/v1/payment/orders/statistics/details?start_date=2026-07-01&end_date=2026-07-20&timezone=Asia%2FShanghai&order_type=balance&page=0",
		"/api/v1/payment/orders/statistics/details?start_date=2026-07-01&end_date=2026-07-20&timezone=Asia%2FShanghai&order_type=balance&page=abc",
		"/api/v1/payment/orders/statistics/details?start_date=2026-07-01&end_date=2026-07-20&timezone=Asia%2FShanghai&order_type=balance&date=2026-07-20&page=1",
	}

	for _, target := range tests {
		ctx, recorder := newPaymentStatisticsContext(target, 1)
		handler.GetOrderStatisticsDetails(ctx)
		require.Equal(t, http.StatusBadRequest, recorder.Code, target)
	}
}

func newPaymentStatisticsContext(target string, userID int64) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	if userID > 0 {
		ctx.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: userID})
	}
	return ctx, recorder
}

func newPaymentStatisticsHandlerHarness(t *testing.T) (*dbent.Client, *PaymentHandler) {
	t.Helper()
	databaseName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	database, err := sql.Open("sqlite", "file:"+databaseName+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	_, err = database.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	driver := entsql.OpenDB(dialect.SQLite, database)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(driver)))
	t.Cleanup(func() { _ = client.Close() })
	paymentService := service.NewPaymentService(client, payment.NewRegistry(), nil, nil, nil, nil, nil, nil, nil)
	return client, NewPaymentHandler(paymentService, nil, nil, nil)
}

func createPaymentStatisticsHandlerUser(t *testing.T, client *dbent.Client, email string) *dbent.User {
	t.Helper()
	user, err := client.User.Create().
		SetEmail(email).
		SetPasswordHash("hash").
		SetUsername(strings.TrimSuffix(email, "@example.com")).
		Save(context.Background())
	require.NoError(t, err)
	return user
}

func createPaymentStatisticsHandlerOrder(t *testing.T, client *dbent.Client, user *dbent.User, outTradeNo string, amount float64, paidAt time.Time) {
	t.Helper()
	_, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(amount).
		SetPayAmount(amount).
		SetFeeRate(0).
		SetRechargeCode("TEST-" + outTradeNo).
		SetOutTradeNo(outTradeNo).
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("TRADE-" + outTradeNo).
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(service.OrderStatusCompleted).
		SetExpiresAt(time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)).
		SetPaidAt(paidAt).
		SetClientIP("127.0.0.1").
		SetSrcHost("example.test").
		Save(context.Background())
	require.NoError(t, err)
}
