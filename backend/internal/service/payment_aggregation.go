package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	appTimezone "github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const (
	adminPaymentAggregationDateLayout      = "2006-01-02"
	adminPaymentAggregationDefaultDays     = 30
	adminPaymentAggregationMaxDays         = 366
	adminPaymentAggregationDefaultPageSize = 20
	adminPaymentAggregationMaxPageSize     = 100
)

var adminPaymentAggregationPaidStatuses = []string{
	OrderStatusPaid,
	OrderStatusRecharging,
	OrderStatusCompleted,
}

var adminPaymentAggregationStatuses = []string{
	OrderStatusPending,
	OrderStatusPaid,
	OrderStatusRecharging,
	OrderStatusCompleted,
	OrderStatusExpired,
	OrderStatusCancelled,
	OrderStatusFailed,
	OrderStatusRefundRequested,
	OrderStatusRefunding,
	OrderStatusRefundPending,
	OrderStatusPartiallyRefunded,
	OrderStatusRefunded,
	OrderStatusRefundFailed,
}

// AdminPaymentAggregationQuery describes the filters for the admin payment
// aggregation page. An empty status means paid orders; ALL includes every
// order status.
type AdminPaymentAggregationQuery struct {
	StartDate   string
	EndDate     string
	UserKeyword string
	Status      string
	Granularity string
	Page        int
	PageSize    int
}

type adminPaymentAggregationWindow struct {
	StartDate      string
	EndDate        string
	Timezone       string
	Location       *time.Location
	StartLocal     time.Time
	EndLocal       time.Time
	StartInclusive time.Time
	EndExclusive   time.Time
}

type AdminPaymentAggregationSummary struct {
	TotalAmount   CurrencyAmounts `json:"total_amount"`
	AverageAmount CurrencyAmounts `json:"average_amount"`
	OrderCount    int             `json:"order_count"`
	UserCount     int             `json:"user_count"`
}

type AdminPaymentAggregationUser struct {
	UserID        int64   `json:"user_id"`
	UserEmail     string  `json:"user_email,omitempty"`
	UserName      string  `json:"user_name,omitempty"`
	Currency      string  `json:"currency"`
	TotalAmount   float64 `json:"total_amount"`
	OrderCount    int     `json:"order_count"`
	AverageAmount float64 `json:"average_amount"`
}

type AdminPaymentAggregationBucket struct {
	Period      string          `json:"period"`
	PeriodStart string          `json:"period_start"`
	PeriodEnd   string          `json:"period_end"`
	Amount      CurrencyAmounts `json:"amount"`
	OrderCount  int             `json:"order_count"`
	UserCount   int             `json:"user_count"`
}

type AdminPaymentAggregationResponse struct {
	StartDate   string                          `json:"start_date"`
	EndDate     string                          `json:"end_date"`
	Timezone    string                          `json:"timezone"`
	Granularity string                          `json:"granularity"`
	Summary     AdminPaymentAggregationSummary  `json:"summary"`
	Timeline    []AdminPaymentAggregationBucket `json:"timeline"`
	Users       []AdminPaymentAggregationUser   `json:"users"`
	Total       int                             `json:"total"`
	Page        int                             `json:"page"`
	PageSize    int                             `json:"page_size"`
	Pages       int                             `json:"pages"`
}

type adminPaymentAggregationRow struct {
	UserID    int64
	UserEmail string
	UserName  string
	Currency  string
	PayAmount float64
	EventAt   time.Time
}

type adminPaymentAggregationUserKey struct {
	UserID   int64
	Currency string
}

type adminPaymentAggregationUserAccumulator struct {
	UserID     int64
	UserEmail  string
	UserName   string
	Currency   string
	TotalCents int64
	OrderCount int
}

type adminPaymentAggregationTimelineAccumulator struct {
	Start       time.Time
	End         time.Time
	AmountCents map[string]int64
	OrderCount  int
	UserIDs     map[int64]struct{}
}

func parseAdminPaymentAggregationWindow(query AdminPaymentAggregationQuery, now time.Time) (adminPaymentAggregationWindow, error) {
	location := appTimezone.Location()
	startDate := strings.TrimSpace(query.StartDate)
	endDate := strings.TrimSpace(query.EndDate)
	if (startDate == "") != (endDate == "") {
		return adminPaymentAggregationWindow{}, fmt.Errorf("start_date and end_date must be provided together")
	}

	var startLocal, endLocal time.Time
	if startDate == "" {
		localNow := now.In(location)
		endLocal = time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
		startLocal = endLocal.AddDate(0, 0, -(adminPaymentAggregationDefaultDays - 1))
	} else {
		var err error
		startLocal, err = time.ParseInLocation(adminPaymentAggregationDateLayout, startDate, location)
		if err != nil {
			return adminPaymentAggregationWindow{}, fmt.Errorf("invalid start_date, expected YYYY-MM-DD")
		}
		endLocal, err = time.ParseInLocation(adminPaymentAggregationDateLayout, endDate, location)
		if err != nil {
			return adminPaymentAggregationWindow{}, fmt.Errorf("invalid end_date, expected YYYY-MM-DD")
		}
	}
	if endLocal.Before(startLocal) {
		return adminPaymentAggregationWindow{}, fmt.Errorf("end_date must not be before start_date")
	}
	if inclusiveCalendarDays(startLocal, endLocal) > adminPaymentAggregationMaxDays {
		return adminPaymentAggregationWindow{}, fmt.Errorf("date range must not exceed 366 calendar days")
	}

	return adminPaymentAggregationWindow{
		StartDate:      startLocal.Format(adminPaymentAggregationDateLayout),
		EndDate:        endLocal.Format(adminPaymentAggregationDateLayout),
		Timezone:       appTimezone.Name(),
		Location:       location,
		StartLocal:     startLocal,
		EndLocal:       endLocal,
		StartInclusive: startLocal,
		EndExclusive:   endLocal.AddDate(0, 0, 1),
	}, nil
}

func normalizeAdminPaymentAggregationGranularity(granularity string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(granularity))
	if normalized == "" {
		return "day", nil
	}
	switch normalized {
	case "day", "week", "month":
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported granularity, expected day, week, or month")
	}
}

func adminPaymentAggregationStatusPredicates(status string) ([]predicate.PaymentOrder, error) {
	normalized := strings.ToUpper(strings.TrimSpace(status))
	if normalized == "" {
		return []predicate.PaymentOrder{paymentorder.StatusIn(adminPaymentAggregationPaidStatuses...)}, nil
	}
	if normalized == "ALL" {
		return nil, nil
	}
	for _, supported := range adminPaymentAggregationStatuses {
		if normalized == supported {
			return []predicate.PaymentOrder{paymentorder.StatusEQ(normalized)}, nil
		}
	}
	return nil, fmt.Errorf("unsupported status")
}

func adminPaymentAggregationTimePredicate(window adminPaymentAggregationWindow) predicate.PaymentOrder {
	// Paid orders are grouped by paid_at. Unpaid orders selected through the
	// status filter have no paid_at, so their order creation time is used.
	return paymentorder.Or(
		paymentorder.And(
			paymentorder.PaidAtNotNil(),
			paymentorder.PaidAtGTE(window.StartInclusive.UTC()),
			paymentorder.PaidAtLT(window.EndExclusive.UTC()),
		),
		paymentorder.And(
			paymentorder.PaidAtIsNil(),
			paymentorder.CreatedAtGTE(window.StartInclusive.UTC()),
			paymentorder.CreatedAtLT(window.EndExclusive.UTC()),
		),
	)
}

// GetAdminPaymentAggregation returns the filtered payment totals, a time
// series, and a paginated user/currency leaderboard.
func (s *PaymentService) GetAdminPaymentAggregation(ctx context.Context, query AdminPaymentAggregationQuery) (*AdminPaymentAggregationResponse, error) {
	window, err := parseAdminPaymentAggregationWindow(query, time.Now())
	if err != nil {
		return nil, infraerrors.BadRequest("INVALID_PAYMENT_AGGREGATION_RANGE", err.Error())
	}
	granularity, err := normalizeAdminPaymentAggregationGranularity(query.Granularity)
	if err != nil {
		return nil, infraerrors.BadRequest("INVALID_PAYMENT_AGGREGATION_GRANULARITY", err.Error())
	}
	statusPredicates, err := adminPaymentAggregationStatusPredicates(query.Status)
	if err != nil {
		return nil, infraerrors.BadRequest("INVALID_PAYMENT_AGGREGATION_STATUS", err.Error())
	}

	predicates := []predicate.PaymentOrder{adminPaymentAggregationTimePredicate(window)}
	predicates = append(predicates, statusPredicates...)
	if keyword := strings.TrimSpace(query.UserKeyword); keyword != "" {
		userPredicates := []predicate.PaymentOrder{
			paymentorder.UserEmailContainsFold(keyword),
			paymentorder.UserNameContainsFold(keyword),
		}
		if userID, parseErr := strconv.ParseInt(keyword, 10, 64); parseErr == nil && userID > 0 {
			userPredicates = append(userPredicates, paymentorder.UserIDEQ(userID))
		}
		predicates = append(predicates, paymentorder.Or(userPredicates...))
	}

	orders, err := s.entClient.PaymentOrder.Query().
		Where(predicates...).
		Select(
			paymentorder.FieldUserID,
			paymentorder.FieldUserEmail,
			paymentorder.FieldUserName,
			paymentorder.FieldPayAmount,
			paymentorder.FieldProviderSnapshot,
			paymentorder.FieldPaidAt,
			paymentorder.FieldCreatedAt,
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query admin payment aggregation: %w", err)
	}

	rows := make([]adminPaymentAggregationRow, 0, len(orders))
	for _, order := range orders {
		if order == nil {
			continue
		}
		eventAt := order.CreatedAt
		if order.PaidAt != nil {
			eventAt = *order.PaidAt
		}
		rows = append(rows, adminPaymentAggregationRow{
			UserID:    order.UserID,
			UserEmail: order.UserEmail,
			UserName:  order.UserName,
			Currency:  PaymentOrderCurrency(order),
			PayAmount: order.PayAmount,
			EventAt:   eventAt,
		})
	}

	return aggregateAdminPaymentAggregation(rows, window, granularity, query.Page, query.PageSize), nil
}

func aggregateAdminPaymentAggregation(rows []adminPaymentAggregationRow, window adminPaymentAggregationWindow, granularity string, page, pageSize int) *AdminPaymentAggregationResponse {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = adminPaymentAggregationDefaultPageSize
	}
	if pageSize > adminPaymentAggregationMaxPageSize {
		pageSize = adminPaymentAggregationMaxPageSize
	}

	totalCents := make(map[string]int64)
	countsByCurrency := make(map[string]int)
	userIDs := make(map[int64]struct{})
	userBuckets := make(map[adminPaymentAggregationUserKey]*adminPaymentAggregationUserAccumulator)
	timelineBuckets := make(map[string]*adminPaymentAggregationTimelineAccumulator)

	for _, row := range rows {
		currency := strings.TrimSpace(row.Currency)
		if currency == "" {
			currency = payment.DefaultPaymentCurrency
		}
		cents := amountToCents(row.PayAmount)
		totalCents[currency] += cents
		countsByCurrency[currency]++
		userIDs[row.UserID] = struct{}{}

		userKey := adminPaymentAggregationUserKey{UserID: row.UserID, Currency: currency}
		userBucket := userBuckets[userKey]
		if userBucket == nil {
			userBucket = &adminPaymentAggregationUserAccumulator{
				UserID:    row.UserID,
				UserEmail: row.UserEmail,
				UserName:  row.UserName,
				Currency:  currency,
			}
			userBuckets[userKey] = userBucket
		}
		userBucket.TotalCents += cents
		userBucket.OrderCount++

		period, periodStart, periodEnd := adminPaymentAggregationPeriod(row.EventAt, granularity, window.Location)
		timelineBucket := timelineBuckets[period]
		if timelineBucket == nil {
			timelineBucket = &adminPaymentAggregationTimelineAccumulator{
				Start:       periodStart,
				End:         periodEnd,
				AmountCents: make(map[string]int64),
				UserIDs:     make(map[int64]struct{}),
			}
			timelineBuckets[period] = timelineBucket
		}
		timelineBucket.AmountCents[currency] += cents
		timelineBucket.OrderCount++
		timelineBucket.UserIDs[row.UserID] = struct{}{}
	}

	users := make([]AdminPaymentAggregationUser, 0, len(userBuckets))
	for _, bucket := range userBuckets {
		users = append(users, AdminPaymentAggregationUser{
			UserID:        bucket.UserID,
			UserEmail:     bucket.UserEmail,
			UserName:      bucket.UserName,
			Currency:      bucket.Currency,
			TotalAmount:   centsToAmount(bucket.TotalCents),
			OrderCount:    bucket.OrderCount,
			AverageAmount: centsToAmount(averageCents(bucket.TotalCents, bucket.OrderCount)),
		})
	}
	sort.Slice(users, func(i, j int) bool {
		if users[i].TotalAmount != users[j].TotalAmount {
			return users[i].TotalAmount > users[j].TotalAmount
		}
		if users[i].Currency != users[j].Currency {
			return users[i].Currency < users[j].Currency
		}
		left := strings.ToLower(users[i].UserEmail)
		right := strings.ToLower(users[j].UserEmail)
		if left != right {
			return left < right
		}
		return users[i].UserID < users[j].UserID
	})

	start := (page - 1) * pageSize
	end := start + pageSize
	if start > len(users) {
		start = len(users)
	}
	if end > len(users) {
		end = len(users)
	}
	pageUsers := users[start:end]
	pages := (len(users) + pageSize - 1) / pageSize
	if pages == 0 {
		pages = 1
	}

	summary := AdminPaymentAggregationSummary{
		TotalAmount:   make(CurrencyAmounts),
		AverageAmount: make(CurrencyAmounts),
		OrderCount:    len(rows),
		UserCount:     len(userIDs),
	}
	for currency, cents := range totalCents {
		summary.TotalAmount[currency] = centsToAmount(cents)
		summary.AverageAmount[currency] = centsToAmount(averageCents(cents, countsByCurrency[currency]))
	}

	timeline := buildAdminPaymentAggregationTimeline(window, granularity, timelineBuckets)
	return &AdminPaymentAggregationResponse{
		StartDate:   window.StartDate,
		EndDate:     window.EndDate,
		Timezone:    window.Timezone,
		Granularity: granularity,
		Summary:     summary,
		Timeline:    timeline,
		Users:       pageUsers,
		Total:       len(users),
		Page:        page,
		PageSize:    pageSize,
		Pages:       pages,
	}
}

func adminPaymentAggregationPeriod(at time.Time, granularity string, location *time.Location) (string, time.Time, time.Time) {
	local := at.In(location)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	switch granularity {
	case "week":
		weekday := int(dayStart.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		start := dayStart.AddDate(0, 0, -(weekday - 1))
		end := start.AddDate(0, 0, 7)
		year, week := start.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", year, week), start, end
	case "month":
		start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
		return start.Format("2006-01"), start, start.AddDate(0, 1, 0)
	default:
		return dayStart.Format(adminPaymentAggregationDateLayout), dayStart, dayStart.AddDate(0, 0, 1)
	}
}

func buildAdminPaymentAggregationTimeline(window adminPaymentAggregationWindow, granularity string, buckets map[string]*adminPaymentAggregationTimelineAccumulator) []AdminPaymentAggregationBucket {
	start := window.StartLocal
	if granularity == "week" {
		_, start, _ = adminPaymentAggregationPeriod(start, granularity, window.Location)
	} else if granularity == "month" {
		_, start, _ = adminPaymentAggregationPeriod(start, granularity, window.Location)
	}

	timeline := make([]AdminPaymentAggregationBucket, 0)
	for !start.After(window.EndLocal) {
		period, periodStart, periodEnd := adminPaymentAggregationPeriod(start, granularity, window.Location)
		bucket := buckets[period]
		amount := make(CurrencyAmounts)
		orderCount := 0
		userCount := 0
		if bucket != nil {
			for currency, cents := range bucket.AmountCents {
				amount[currency] = centsToAmount(cents)
			}
			orderCount = bucket.OrderCount
			userCount = len(bucket.UserIDs)
		}
		timeline = append(timeline, AdminPaymentAggregationBucket{
			Period:      period,
			PeriodStart: periodStart.Format(adminPaymentAggregationDateLayout),
			PeriodEnd:   periodEnd.AddDate(0, 0, -1).Format(adminPaymentAggregationDateLayout),
			Amount:      amount,
			OrderCount:  orderCount,
			UserCount:   userCount,
		})
		switch granularity {
		case "week":
			start = start.AddDate(0, 0, 7)
		case "month":
			start = start.AddDate(0, 1, 0)
		default:
			start = start.AddDate(0, 0, 1)
		}
	}
	return timeline
}
