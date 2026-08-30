package repository

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// userUsageAnalyticsRepository decorates the concrete legacy repository so
// all unmodified and optional methods remain promoted automatically. Only the
// ordinary-user usage-page methods below can take the aggregate fast path.
type userUsageAnalyticsRepository struct {
	*usageLogRepository
	analytics *userUsageAnalyticsStore
}

var _ service.UsageLogRepository = (*userUsageAnalyticsRepository)(nil)

func newUserUsageAnalyticsRepository(repo *usageLogRepository, sqlq sqlExecutor) *userUsageAnalyticsRepository {
	analytics := newUserUsageAnalyticsStore(sqlq)
	analytics.StartAutomaticBackfill()
	return &userUsageAnalyticsRepository{
		usageLogRepository: repo,
		analytics:          analytics,
	}
}

func (r *userUsageAnalyticsRepository) ListWithFilters(
	ctx context.Context,
	params pagination.PaginationParams,
	filters UsageLogFilters,
) ([]service.UsageLog, *pagination.PaginationResult, error) {
	if r == nil || r.usageLogRepository == nil {
		return nil, nil, fmt.Errorf("user usage analytics repository is not configured")
	}
	if r.analytics == nil {
		return r.usageLogRepository.ListWithFilters(ctx, params, filters)
	}
	total, handled, err := r.analytics.Total(ctx, filters)
	if err != nil {
		if !userUsageAnalyticsShouldFallBack(ctx, "list_total", err) {
			return nil, nil, err
		}
		return r.usageLogRepository.ListWithFilters(ctx, params, filters)
	}
	if !handled {
		return r.usageLogRepository.ListWithFilters(ctx, params, filters)
	}

	whereClause, args := buildLegacyUsageLogWhereForAnalytics(filters)
	limitPos := len(args) + 1
	offsetPos := len(args) + 2
	listArgs := append(append([]any{}, args...), params.Limit(), params.Offset())
	query := fmt.Sprintf(
		"SELECT %s FROM usage_logs %s ORDER BY %s LIMIT $%d OFFSET $%d",
		usageLogSelectColumns,
		whereClause,
		usageLogOrderBy(params),
		limitPos,
		offsetPos,
	)
	logs, err := r.queryUsageLogs(ctx, query, listArgs...)
	if err != nil {
		return nil, nil, err
	}
	if err := r.hydrateUsageLogAssociations(ctx, logs); err != nil {
		return nil, nil, err
	}
	return logs, paginationResultFromTotal(total, params), nil
}

func (r *userUsageAnalyticsRepository) GetStatsWithFilters(ctx context.Context, filters UsageLogFilters) (*usagestats.UsageStats, error) {
	if r == nil || r.usageLogRepository == nil {
		return nil, fmt.Errorf("user usage analytics repository is not configured")
	}
	if r.analytics == nil {
		return r.usageLogRepository.GetStatsWithFilters(ctx, filters)
	}
	stats, handled, err := r.analytics.Stats(ctx, filters)
	if err != nil {
		if !userUsageAnalyticsShouldFallBack(ctx, "stats", err) {
			return nil, err
		}
		return r.usageLogRepository.GetStatsWithFilters(ctx, filters)
	}
	if handled {
		return stats, nil
	}
	return r.usageLogRepository.GetStatsWithFilters(ctx, filters)
}

func (r *userUsageAnalyticsRepository) GetUsageTrendWithUsageFilters(
	ctx context.Context,
	startTime time.Time,
	endTime time.Time,
	granularity string,
	filters UsageLogFilters,
) ([]usagestats.TrendDataPoint, error) {
	if r == nil || r.usageLogRepository == nil {
		return nil, fmt.Errorf("user usage analytics repository is not configured")
	}
	if r.analytics == nil {
		return r.usageLogRepository.GetUsageTrendWithUsageFilters(ctx, startTime, endTime, granularity, filters)
	}
	trend, handled, err := r.analytics.Trend(ctx, startTime, endTime, granularity, filters)
	if err != nil {
		if !userUsageAnalyticsShouldFallBack(ctx, "trend", err) {
			return nil, err
		}
		return r.usageLogRepository.GetUsageTrendWithUsageFilters(ctx, startTime, endTime, granularity, filters)
	}
	if handled {
		return trend, nil
	}
	return r.usageLogRepository.GetUsageTrendWithUsageFilters(ctx, startTime, endTime, granularity, filters)
}

func (r *userUsageAnalyticsRepository) GetModelStatsWithUsageFiltersBySource(
	ctx context.Context,
	startTime time.Time,
	endTime time.Time,
	filters UsageLogFilters,
	source string,
) ([]usagestats.ModelStat, error) {
	if r == nil || r.usageLogRepository == nil {
		return nil, fmt.Errorf("user usage analytics repository is not configured")
	}
	if r.analytics == nil {
		return r.usageLogRepository.GetModelStatsWithUsageFiltersBySource(ctx, startTime, endTime, filters, source)
	}
	stats, handled, err := r.analytics.ModelStats(ctx, startTime, endTime, filters, source)
	if err != nil {
		if !userUsageAnalyticsShouldFallBack(ctx, "models", err) {
			return nil, err
		}
		return r.usageLogRepository.GetModelStatsWithUsageFiltersBySource(ctx, startTime, endTime, filters, source)
	}
	if handled {
		return stats, nil
	}
	return r.usageLogRepository.GetModelStatsWithUsageFiltersBySource(ctx, startTime, endTime, filters, source)
}

func (r *userUsageAnalyticsRepository) GetGroupStatsWithUsageFilters(
	ctx context.Context,
	startTime time.Time,
	endTime time.Time,
	filters UsageLogFilters,
) ([]usagestats.GroupStat, error) {
	if r == nil || r.usageLogRepository == nil {
		return nil, fmt.Errorf("user usage analytics repository is not configured")
	}
	if r.analytics == nil {
		return r.usageLogRepository.GetGroupStatsWithUsageFilters(ctx, startTime, endTime, filters)
	}
	stats, handled, err := r.analytics.GroupStats(ctx, startTime, endTime, filters)
	if err != nil {
		if !userUsageAnalyticsShouldFallBack(ctx, "groups", err) {
			return nil, err
		}
		return r.usageLogRepository.GetGroupStatsWithUsageFilters(ctx, startTime, endTime, filters)
	}
	if handled {
		return stats, nil
	}
	return r.usageLogRepository.GetGroupStatsWithUsageFilters(ctx, startTime, endTime, filters)
}

func userUsageAnalyticsShouldFallBack(ctx context.Context, operation string, err error) bool {
	if err == nil {
		return false
	}
	if ctx == nil || ctx.Err() != nil {
		return false
	}
	slog.Warn("user usage analytics query failed; falling back to legacy SQL", "operation", operation, "error", err)
	return true
}

// buildLegacyUsageLogWhereForAnalytics intentionally lives beside the
// decorator instead of refactoring the upstream query file. It reuses the
// existing semantic helpers while keeping the merge-conflict surface confined
// to the constructor and route wiring lines.
func buildLegacyUsageLogWhereForAnalytics(filters UsageLogFilters) (string, []any) {
	conditions := make([]string, 0, 9)
	args := make([]any, 0, 9)

	if filters.UserID > 0 {
		conditions = append(conditions, fmt.Sprintf("user_id = $%d", len(args)+1))
		args = append(args, filters.UserID)
	}
	if filters.APIKeyID > 0 {
		conditions = append(conditions, fmt.Sprintf("api_key_id = $%d", len(args)+1))
		args = append(args, filters.APIKeyID)
	}
	if filters.AccountID > 0 {
		conditions = append(conditions, fmt.Sprintf("account_id = $%d", len(args)+1))
		args = append(args, filters.AccountID)
	}
	if filters.GroupID > 0 {
		conditions = append(conditions, fmt.Sprintf("group_id = $%d", len(args)+1))
		args = append(args, filters.GroupID)
	}
	if requestID := strings.TrimSpace(filters.RequestID); requestID != "" {
		conditions = append(conditions, fmt.Sprintf("request_id = $%d", len(args)+1))
		args = append(args, requestID)
	}
	conditions, args = appendUsageLogModelWhereCondition(conditions, args, filters.Model, filters.ModelFilterSource)
	conditions, args = appendRequestTypeOrStreamWhereCondition(conditions, args, filters.RequestType, filters.Stream)
	if filters.BillingType != nil {
		conditions = append(conditions, fmt.Sprintf("billing_type = $%d", len(args)+1))
		args = append(args, int16(*filters.BillingType))
	}
	conditions, args = appendUsageLogBillingModeWhereCondition(conditions, args, filters.BillingMode)
	if filters.UpstreamModelMismatch != nil {
		conditions = append(conditions, upstreamModelMismatchCondition("upstream_model_mismatch", *filters.UpstreamModelMismatch))
	}
	if filters.StartTime != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", len(args)+1))
		args = append(args, *filters.StartTime)
	}
	if filters.EndTime != nil {
		conditions = append(conditions, fmt.Sprintf("created_at < $%d", len(args)+1))
		args = append(args, *filters.EndTime)
	}
	return buildWhere(conditions), args
}
