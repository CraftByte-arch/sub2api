package service

import (
	"math"
	"sort"
	"strings"
	"time"

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
