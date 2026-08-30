package repository

import (
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func userUsageAnalyticsRangeFromFilters(filters UsageLogFilters) (time.Time, time.Time, bool) {
	if filters.StartTime == nil || filters.EndTime == nil {
		return time.Time{}, time.Time{}, false
	}
	return *filters.StartTime, *filters.EndTime, true
}

func userUsageAnalyticsRangeIsSafe(startTime, endTime time.Time) bool {
	if startTime.IsZero() || endTime.IsZero() || !endTime.After(startTime) {
		return false
	}
	return startTime.Equal(startTime.Truncate(time.Hour)) && endTime.Equal(endTime.Truncate(time.Hour))
}

func userUsageAnalyticsFiltersAreSupported(filters UsageLogFilters) bool {
	if filters.UserID <= 0 || filters.AccountID > 0 {
		return false
	}
	if strings.TrimSpace(filters.RequestID) != "" || filters.UpstreamModelMismatch != nil {
		return false
	}
	if usagestats.NormalizeModelSource(filters.ModelFilterSource) != usagestats.ModelSourceRequested {
		return false
	}
	return service.BillingMode(strings.TrimSpace(filters.BillingMode)).IsValidUsageFilter()
}

func buildUserUsageAnalyticsWhere(startTime, endTime time.Time, filters UsageLogFilters) (string, []any) {
	conditions := make([]string, 0, 10)
	args := make([]any, 0, 10)
	appendArg := func(condition string, value any) {
		conditions = append(conditions, fmt.Sprintf(condition, len(args)+1))
		args = append(args, value)
	}

	appendArg("user_id = $%d", filters.UserID)
	appendArg("bucket_start >= $%d", startTime)
	appendArg("bucket_start < $%d", endTime)
	if filters.APIKeyID > 0 {
		appendArg("api_key_id = $%d", filters.APIKeyID)
	}
	if filters.GroupID > 0 {
		appendArg("group_id = $%d", filters.GroupID)
	}
	if strings.TrimSpace(filters.Model) != "" {
		appendArg("requested_model = $%d", filters.Model)
	}
	if filters.RequestType != nil {
		normalized := service.RequestTypeFromInt16(*filters.RequestType)
		placeholder := len(args) + 1
		switch normalized {
		case service.RequestTypeSync:
			conditions = append(conditions, fmt.Sprintf("(request_type = $%d OR (request_type = 0 AND stream = FALSE AND openai_ws_mode = FALSE))", placeholder))
		case service.RequestTypeStream:
			conditions = append(conditions, fmt.Sprintf("(request_type = $%d OR (request_type = 0 AND stream = TRUE AND openai_ws_mode = FALSE))", placeholder))
		case service.RequestTypeWSV2:
			conditions = append(conditions, fmt.Sprintf("(request_type = $%d OR (request_type = 0 AND openai_ws_mode = TRUE))", placeholder))
		default:
			conditions = append(conditions, fmt.Sprintf("request_type = $%d", placeholder))
		}
		args = append(args, int16(normalized))
	} else if filters.Stream != nil {
		appendArg("stream = $%d", *filters.Stream)
	}
	if filters.BillingType != nil {
		appendArg("billing_type = $%d", int16(*filters.BillingType))
	}
	if billingMode := strings.TrimSpace(filters.BillingMode); billingMode != "" {
		appendArg("billing_mode = $%d", billingMode)
	}

	return "WHERE " + strings.Join(conditions, " AND "), args
}
