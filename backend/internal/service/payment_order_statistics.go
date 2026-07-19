package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	appTimezone "github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const (
	orderStatisticsDateLayout     = "2006-01-02"
	orderStatisticsDefaultDays    = 30
	orderStatisticsMaxDays        = 366
	OrderStatisticsDetailPageSize = 20
	orderStatisticsCurrency       = "CNY"
	orderStatisticsRangeError     = "INVALID_ORDER_STATISTICS_RANGE"
	orderStatisticsTimezoneError  = "INVALID_ORDER_STATISTICS_TIMEZONE"
)

var orderStatisticsTypes = []string{
	payment.OrderTypeBalance,
	payment.OrderTypeUsageCard,
	payment.OrderTypeSubscription,
}

type OrderStatisticsQuery struct {
	StartDate string
	EndDate   string
	Timezone  string
}

type orderStatisticsWindow struct {
	StartDate      string
	EndDate        string
	Timezone       string
	Location       *time.Location
	StartLocal     time.Time
	EndLocal       time.Time
	StartInclusive time.Time
	EndExclusive   time.Time
}

type OrderStatisticsSummary struct {
	TotalPaidAmount   float64 `json:"total_paid_amount"`
	OrderCount        int     `json:"order_count"`
	AveragePaidAmount float64 `json:"average_paid_amount"`
}

type OrderTypeStatistics struct {
	OrderType         string  `json:"order_type"`
	TotalPaidAmount   float64 `json:"total_paid_amount"`
	OrderCount        int     `json:"order_count"`
	AveragePaidAmount float64 `json:"average_paid_amount"`
}

type DailyOrderStatistics struct {
	Date              string  `json:"date"`
	TotalPaidAmount   float64 `json:"total_paid_amount"`
	OrderCount        int     `json:"order_count"`
	AveragePaidAmount float64 `json:"average_paid_amount"`
}

type OrderStatisticsResponse struct {
	StartDate string                 `json:"start_date"`
	EndDate   string                 `json:"end_date"`
	Timezone  string                 `json:"timezone"`
	Currency  string                 `json:"currency"`
	Summary   OrderStatisticsSummary `json:"summary"`
	ByType    []OrderTypeStatistics  `json:"by_type"`
	Daily     []DailyOrderStatistics `json:"daily"`
}

type OrderStatisticsDetailsQuery struct {
	OrderStatisticsQuery
	Page      int
	OrderType string
	Date      string
}

type OrderStatisticsDetail struct {
	OutTradeNo  string    `json:"out_trade_no"`
	OrderType   string    `json:"order_type"`
	PayAmount   float64   `json:"pay_amount"`
	Status      string    `json:"status"`
	PaymentType string    `json:"payment_type"`
	PaidAt      time.Time `json:"paid_at"`
}

type orderStatisticsRow struct {
	ID        int64
	PayAmount float64
	OrderType string
	PaidAt    time.Time
}

type orderStatisticsAccumulator struct {
	totalCents int64
	count      int
}

func parseOrderStatisticsWindow(query OrderStatisticsQuery, now time.Time) (orderStatisticsWindow, error) {
	location, timezoneName, err := resolveOrderStatisticsLocation(strings.TrimSpace(query.Timezone))
	if err != nil {
		return orderStatisticsWindow{}, err
	}

	startDate := strings.TrimSpace(query.StartDate)
	endDate := strings.TrimSpace(query.EndDate)
	if (startDate == "") != (endDate == "") {
		return orderStatisticsWindow{}, infraerrors.BadRequest(orderStatisticsRangeError, "start_date and end_date must be provided together")
	}

	var startLocal, endLocal time.Time
	if startDate == "" {
		localNow := now.In(location)
		endLocal = time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
		startLocal = endLocal.AddDate(0, 0, -(orderStatisticsDefaultDays - 1))
	} else {
		startLocal, err = time.ParseInLocation(orderStatisticsDateLayout, startDate, location)
		if err != nil {
			return orderStatisticsWindow{}, infraerrors.BadRequest(orderStatisticsRangeError, "invalid start_date, expected YYYY-MM-DD")
		}
		endLocal, err = time.ParseInLocation(orderStatisticsDateLayout, endDate, location)
		if err != nil {
			return orderStatisticsWindow{}, infraerrors.BadRequest(orderStatisticsRangeError, "invalid end_date, expected YYYY-MM-DD")
		}
	}

	if endLocal.Before(startLocal) {
		return orderStatisticsWindow{}, infraerrors.BadRequest(orderStatisticsRangeError, "end_date must not be before start_date")
	}
	if inclusiveCalendarDays(startLocal, endLocal) > orderStatisticsMaxDays {
		return orderStatisticsWindow{}, infraerrors.BadRequest(orderStatisticsRangeError, "date range must not exceed 366 calendar days")
	}

	return orderStatisticsWindow{
		StartDate:      startLocal.Format(orderStatisticsDateLayout),
		EndDate:        endLocal.Format(orderStatisticsDateLayout),
		Timezone:       timezoneName,
		Location:       location,
		StartLocal:     startLocal,
		EndLocal:       endLocal,
		StartInclusive: startLocal,
		EndExclusive:   endLocal.AddDate(0, 0, 1),
	}, nil
}

func resolveOrderStatisticsLocation(name string) (*time.Location, string, error) {
	if name == "" {
		return appTimezone.Location(), appTimezone.Name(), nil
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, "", infraerrors.BadRequest(orderStatisticsTimezoneError, "invalid IANA timezone")
	}
	return location, name, nil
}

func inclusiveCalendarDays(start, end time.Time) int {
	startUTC := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	endUTC := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	return int(endUTC.Sub(startUTC)/(24*time.Hour)) + 1
}

func aggregateOrderStatistics(rows []orderStatisticsRow, location *time.Location) OrderStatisticsResponse {
	if location == nil {
		location = time.UTC
	}

	typeBuckets := make(map[string]*orderStatisticsAccumulator, len(orderStatisticsTypes))
	for _, orderType := range orderStatisticsTypes {
		typeBuckets[orderType] = &orderStatisticsAccumulator{}
	}
	dailyBuckets := make(map[string]*orderStatisticsAccumulator)
	total := orderStatisticsAccumulator{}

	for _, row := range rows {
		typeBucket, supported := typeBuckets[row.OrderType]
		if !supported {
			continue
		}
		cents := amountToCents(row.PayAmount)
		total.totalCents += cents
		total.count++
		typeBucket.totalCents += cents
		typeBucket.count++

		date := row.PaidAt.In(location).Format(orderStatisticsDateLayout)
		dailyBucket := dailyBuckets[date]
		if dailyBucket == nil {
			dailyBucket = &orderStatisticsAccumulator{}
			dailyBuckets[date] = dailyBucket
		}
		dailyBucket.totalCents += cents
		dailyBucket.count++
	}

	byType := make([]OrderTypeStatistics, 0, len(orderStatisticsTypes))
	for _, orderType := range orderStatisticsTypes {
		bucket := typeBuckets[orderType]
		byType = append(byType, OrderTypeStatistics{
			OrderType:         orderType,
			TotalPaidAmount:   centsToAmount(bucket.totalCents),
			OrderCount:        bucket.count,
			AveragePaidAmount: centsToAmount(averageCents(bucket.totalCents, bucket.count)),
		})
	}

	dates := make([]string, 0, len(dailyBuckets))
	for date := range dailyBuckets {
		dates = append(dates, date)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	daily := make([]DailyOrderStatistics, 0, len(dates))
	for _, date := range dates {
		bucket := dailyBuckets[date]
		daily = append(daily, DailyOrderStatistics{
			Date:              date,
			TotalPaidAmount:   centsToAmount(bucket.totalCents),
			OrderCount:        bucket.count,
			AveragePaidAmount: centsToAmount(averageCents(bucket.totalCents, bucket.count)),
		})
	}

	return OrderStatisticsResponse{
		Currency: orderStatisticsCurrency,
		Summary: OrderStatisticsSummary{
			TotalPaidAmount:   centsToAmount(total.totalCents),
			OrderCount:        total.count,
			AveragePaidAmount: centsToAmount(averageCents(total.totalCents, total.count)),
		},
		ByType: byType,
		Daily:  daily,
	}
}

func amountToCents(amount float64) int64 {
	return int64(math.Round(amount * 100))
}

func centsToAmount(cents int64) float64 {
	return float64(cents) / 100
}

func averageCents(totalCents int64, count int) int64 {
	if count == 0 {
		return 0
	}
	return int64(math.Round(float64(totalCents) / float64(count)))
}

func (s *PaymentService) GetUserOrderStatistics(ctx context.Context, userID int64, query OrderStatisticsQuery) (*OrderStatisticsResponse, error) {
	window, err := parseOrderStatisticsWindow(query, time.Now())
	if err != nil {
		return nil, err
	}

	orders, err := s.entClient.PaymentOrder.Query().
		Where(paidOrderStatisticsPredicates(userID, window)...).
		Select(
			paymentorder.FieldID,
			paymentorder.FieldPayAmount,
			paymentorder.FieldOrderType,
			paymentorder.FieldPaidAt,
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query user order statistics: %w", err)
	}

	rows := make([]orderStatisticsRow, 0, len(orders))
	for _, order := range orders {
		if order.PaidAt == nil {
			continue
		}
		rows = append(rows, orderStatisticsRow{
			ID:        order.ID,
			PayAmount: order.PayAmount,
			OrderType: order.OrderType,
			PaidAt:    *order.PaidAt,
		})
	}

	result := aggregateOrderStatistics(rows, window.Location)
	result.StartDate = window.StartDate
	result.EndDate = window.EndDate
	result.Timezone = window.Timezone
	return &result, nil
}

func (s *PaymentService) GetUserOrderStatisticsDetails(ctx context.Context, userID int64, query OrderStatisticsDetailsQuery) ([]OrderStatisticsDetail, int, error) {
	if query.Page <= 0 {
		return nil, 0, infraerrors.BadRequest(orderStatisticsRangeError, "page must be a positive integer")
	}
	window, err := parseOrderStatisticsWindow(query.OrderStatisticsQuery, time.Now())
	if err != nil {
		return nil, 0, err
	}

	orderType := strings.TrimSpace(query.OrderType)
	date := strings.TrimSpace(query.Date)
	if (orderType == "") == (date == "") {
		return nil, 0, infraerrors.BadRequest(orderStatisticsRangeError, "exactly one of order_type or date is required")
	}

	predicates := paidOrderStatisticsPredicates(userID, window)
	if orderType != "" {
		if !isSupportedOrderStatisticsType(orderType) {
			return nil, 0, infraerrors.BadRequest(orderStatisticsRangeError, "unsupported order_type")
		}
		predicates = append(predicates, paymentorder.OrderTypeEQ(orderType))
	} else {
		dateStart, parseErr := time.ParseInLocation(orderStatisticsDateLayout, date, window.Location)
		if parseErr != nil {
			return nil, 0, infraerrors.BadRequest(orderStatisticsRangeError, "invalid date, expected YYYY-MM-DD")
		}
		if dateStart.Before(window.StartLocal) || dateStart.After(window.EndLocal) {
			return nil, 0, infraerrors.BadRequest(orderStatisticsRangeError, "date must be within the selected range")
		}
		predicates = paidOrderStatisticsPredicates(userID, orderStatisticsWindow{
			StartInclusive: dateStart,
			EndExclusive:   dateStart.AddDate(0, 0, 1),
		})
	}

	baseQuery := s.entClient.PaymentOrder.Query().Where(predicates...)
	total, err := baseQuery.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count user order statistics details: %w", err)
	}
	orders, err := baseQuery.
		Select(
			paymentorder.FieldID,
			paymentorder.FieldOutTradeNo,
			paymentorder.FieldOrderType,
			paymentorder.FieldPayAmount,
			paymentorder.FieldStatus,
			paymentorder.FieldPaymentType,
			paymentorder.FieldPaidAt,
		).
		Order(
			dbent.Desc(paymentorder.FieldPaidAt),
			dbent.Desc(paymentorder.FieldID),
		).
		Limit(OrderStatisticsDetailPageSize).
		Offset((query.Page - 1) * OrderStatisticsDetailPageSize).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("query user order statistics details: %w", err)
	}

	items := make([]OrderStatisticsDetail, 0, len(orders))
	for _, order := range orders {
		if order.PaidAt == nil {
			continue
		}
		items = append(items, OrderStatisticsDetail{
			OutTradeNo:  order.OutTradeNo,
			OrderType:   order.OrderType,
			PayAmount:   order.PayAmount,
			Status:      order.Status,
			PaymentType: order.PaymentType,
			PaidAt:      *order.PaidAt,
		})
	}
	return items, total, nil
}

func paidOrderStatisticsPredicates(userID int64, window orderStatisticsWindow) []predicate.PaymentOrder {
	return []predicate.PaymentOrder{
		paymentorder.UserIDEQ(userID),
		paymentorder.StatusIn(OrderStatusPaid, OrderStatusRecharging, OrderStatusCompleted),
		paymentorder.OrderTypeIn(orderStatisticsTypes...),
		paymentorder.PaidAtNotNil(),
		paymentorder.PaidAtGTE(window.StartInclusive.UTC()),
		paymentorder.PaidAtLT(window.EndExclusive.UTC()),
	}
}

func isSupportedOrderStatisticsType(orderType string) bool {
	for _, supported := range orderStatisticsTypes {
		if orderType == supported {
			return true
		}
	}
	return false
}
