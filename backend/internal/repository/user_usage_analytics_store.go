package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/pkg/userusageanalytics"
)

// userUsageAnalyticsStore is an isolated read/backfill boundary for the
// ordinary-user usage page. It never serves data until state coverage proves
// that the aggregate can exactly represent the requested range.
type userUsageAnalyticsStore struct {
	sql sqlExecutor
	db  *sql.DB

	startOnce sync.Once
}

func newUserUsageAnalyticsStore(sqlq sqlExecutor) *userUsageAnalyticsStore {
	store := &userUsageAnalyticsStore{sql: sqlq}
	if db, ok := sqlq.(*sql.DB); ok {
		store.db = db
	}
	return store
}

func (s *userUsageAnalyticsStore) prepare(
	ctx context.Context,
	startTime time.Time,
	endTime time.Time,
	filters UsageLogFilters,
) (whereClause string, args []any, handled bool, err error) {
	if s == nil || s.sql == nil || !userusageanalytics.IsMarked(ctx) {
		return "", nil, false, nil
	}
	if !userUsageAnalyticsFiltersAreSupported(filters) || !userUsageAnalyticsRangeIsSafe(startTime, endTime) {
		return "", nil, false, nil
	}

	ready, coverageStart, err := s.readCoverage(ctx)
	if err != nil || !ready || startTime.Before(coverageStart) {
		return "", nil, false, err
	}
	whereClause, args = buildUserUsageAnalyticsWhere(startTime, endTime, filters)
	return whereClause, args, true, nil
}

func (s *userUsageAnalyticsStore) readCoverage(ctx context.Context) (bool, time.Time, error) {
	var ready bool
	var coverageStart sql.NullTime
	err := scanSingleRow(ctx, s.sql, `
		SELECT ready, coverage_start
		FROM user_usage_analytics_hourly_state
		WHERE id = 1
	`, nil, &ready, &coverageStart)
	if errors.Is(err, sql.ErrNoRows) {
		return false, time.Time{}, nil
	}
	if err != nil {
		return false, time.Time{}, err
	}
	if !ready || !coverageStart.Valid {
		return false, time.Time{}, nil
	}
	return true, coverageStart.Time, nil
}

func (s *userUsageAnalyticsStore) Total(ctx context.Context, filters UsageLogFilters) (int64, bool, error) {
	startTime, endTime, ok := userUsageAnalyticsRangeFromFilters(filters)
	if !ok {
		return 0, false, nil
	}
	whereClause, args, handled, err := s.prepare(ctx, startTime, endTime, filters)
	if err != nil || !handled {
		return 0, handled, err
	}

	var total int64
	err = scanSingleRow(ctx, s.sql, `
		SELECT COALESCE(SUM(requests), 0)::BIGINT
		FROM user_usage_analytics_hourly
		`+whereClause, args, &total)
	return total, true, err
}

func (s *userUsageAnalyticsStore) Stats(ctx context.Context, filters UsageLogFilters) (_ *usagestats.UsageStats, handled bool, err error) {
	startTime, endTime, ok := userUsageAnalyticsRangeFromFilters(filters)
	if !ok {
		return nil, false, nil
	}
	whereClause, args, handled, err := s.prepare(ctx, startTime, endTime, filters)
	if err != nil || !handled {
		return nil, handled, err
	}

	rows, err := s.sql.QueryContext(ctx, `
		WITH scoped AS (
			SELECT
				inbound_endpoint,
				requests,
				input_tokens,
				output_tokens,
				cache_creation_tokens,
				cache_read_tokens,
				total_cost,
				actual_cost,
				account_cost,
				duration_sum_ms,
				duration_count
			FROM user_usage_analytics_hourly
			`+whereClause+`
		)
		SELECT
			GROUPING(inbound_endpoint) AS inbound_grouped,
			inbound_endpoint,
			COALESCE(SUM(requests), 0)::BIGINT,
			COALESCE(SUM(input_tokens), 0)::BIGINT,
			COALESCE(SUM(output_tokens), 0)::BIGINT,
			COALESCE(SUM(cache_creation_tokens), 0)::BIGINT,
			COALESCE(SUM(cache_read_tokens), 0)::BIGINT,
			COALESCE(SUM(total_cost), 0),
			COALESCE(SUM(actual_cost), 0),
			COALESCE(SUM(account_cost), 0),
			COALESCE(SUM(duration_sum_ms), 0)::BIGINT,
			COALESCE(SUM(duration_count), 0)::BIGINT
		FROM scoped
		GROUP BY GROUPING SETS ((), (inbound_endpoint))
	`, args...)
	if err != nil {
		return nil, true, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	stats := &usagestats.UsageStats{}
	var totalAccountCost float64
	for rows.Next() {
		var (
			inboundGrouped                                                      int
			inboundEndpoint                                                     sql.NullString
			requests, inputTokens, outputTokens, cacheCreationTokens, cacheRead int64
			cost, actualCost, accountCost                                       float64
			durationSumMs, durationCount                                        int64
		)
		if err = rows.Scan(
			&inboundGrouped,
			&inboundEndpoint,
			&requests,
			&inputTokens,
			&outputTokens,
			&cacheCreationTokens,
			&cacheRead,
			&cost,
			&actualCost,
			&accountCost,
			&durationSumMs,
			&durationCount,
		); err != nil {
			return nil, true, err
		}

		if inboundGrouped == 1 {
			stats.TotalRequests = requests
			stats.TotalInputTokens = inputTokens
			stats.TotalOutputTokens = outputTokens
			stats.TotalCacheCreationTokens = cacheCreationTokens
			stats.TotalCacheReadTokens = cacheRead
			stats.TotalCacheTokens = cacheCreationTokens + cacheRead
			stats.TotalCost = cost
			stats.TotalActualCost = actualCost
			totalAccountCost = accountCost
			if durationCount > 0 {
				stats.AverageDurationMs = float64(durationSumMs) / float64(durationCount)
			}
			continue
		}

		stats.Endpoints = append(stats.Endpoints, usagestats.EndpointStat{
			Endpoint:    inboundEndpoint.String,
			Requests:    requests,
			TotalTokens: inputTokens + outputTokens + cacheCreationTokens + cacheRead,
			Cost:        cost,
			ActualCost:  actualCost,
		})
	}
	if err = rows.Err(); err != nil {
		return nil, true, err
	}

	sort.Slice(stats.Endpoints, func(i, j int) bool {
		if stats.Endpoints[i].Requests != stats.Endpoints[j].Requests {
			return stats.Endpoints[i].Requests > stats.Endpoints[j].Requests
		}
		return stats.Endpoints[i].Endpoint < stats.Endpoints[j].Endpoint
	})
	stats.TotalAccountCost = &totalAccountCost
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheTokens
	return stats, true, nil
}

func (s *userUsageAnalyticsStore) Trend(
	ctx context.Context,
	startTime time.Time,
	endTime time.Time,
	granularity string,
	filters UsageLogFilters,
) (results []usagestats.TrendDataPoint, handled bool, err error) {
	if granularity != "hour" && granularity != "day" {
		return nil, false, nil
	}
	whereClause, args, handled, err := s.prepare(ctx, startTime, endTime, filters)
	if err != nil || !handled {
		return nil, handled, err
	}

	query := fmt.Sprintf(`
		SELECT
			TO_CHAR(bucket_start, '%s') AS date,
			COALESCE(SUM(requests), 0)::BIGINT,
			COALESCE(SUM(input_tokens), 0)::BIGINT,
			COALESCE(SUM(output_tokens), 0)::BIGINT,
			COALESCE(SUM(cache_creation_tokens), 0)::BIGINT,
			COALESCE(SUM(cache_read_tokens), 0)::BIGINT,
			COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0)::BIGINT,
			COALESCE(SUM(total_cost), 0),
			COALESCE(SUM(actual_cost), 0)
		FROM user_usage_analytics_hourly
		%s
		GROUP BY date
		ORDER BY date ASC
	`, safeDateFormat(granularity), whereClause)
	rows, err := s.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, true, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			results = nil
		}
	}()
	results, err = scanTrendRows(rows)
	return results, true, err
}

func (s *userUsageAnalyticsStore) ModelStats(
	ctx context.Context,
	startTime time.Time,
	endTime time.Time,
	filters UsageLogFilters,
	modelSource string,
) (results []usagestats.ModelStat, handled bool, err error) {
	if usagestats.NormalizeModelSource(modelSource) != usagestats.ModelSourceRequested {
		return nil, false, nil
	}
	whereClause, args, handled, err := s.prepare(ctx, startTime, endTime, filters)
	if err != nil || !handled {
		return nil, handled, err
	}

	rows, err := s.sql.QueryContext(ctx, `
		SELECT
			requested_model AS model,
			COALESCE(SUM(requests), 0)::BIGINT,
			COALESCE(SUM(input_tokens), 0)::BIGINT,
			COALESCE(SUM(output_tokens), 0)::BIGINT,
			COALESCE(SUM(cache_creation_tokens), 0)::BIGINT,
			COALESCE(SUM(cache_read_tokens), 0)::BIGINT,
			COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0)::BIGINT,
			COALESCE(SUM(total_cost), 0),
			COALESCE(SUM(actual_cost), 0),
			COALESCE(SUM(account_cost), 0)
		FROM user_usage_analytics_hourly
		`+whereClause+`
		GROUP BY requested_model
		ORDER BY SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens) DESC
	`, args...)
	if err != nil {
		return nil, true, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			results = nil
		}
	}()
	results, err = scanModelStatsRows(rows)
	return results, true, err
}

func (s *userUsageAnalyticsStore) GroupStats(
	ctx context.Context,
	startTime time.Time,
	endTime time.Time,
	filters UsageLogFilters,
) (results []usagestats.GroupStat, handled bool, err error) {
	whereClause, args, handled, err := s.prepare(ctx, startTime, endTime, filters)
	if err != nil || !handled {
		return nil, handled, err
	}

	rows, err := s.sql.QueryContext(ctx, `
		WITH scoped AS (
			SELECT
				group_id,
				requests,
				input_tokens,
				output_tokens,
				cache_creation_tokens,
				cache_read_tokens,
				total_cost,
				actual_cost,
				account_cost
			FROM user_usage_analytics_hourly
			`+whereClause+`
		)
		SELECT
			scoped.group_id,
			COALESCE(g.name, '') AS group_name,
			COALESCE(SUM(scoped.requests), 0)::BIGINT,
			COALESCE(SUM(scoped.input_tokens + scoped.output_tokens + scoped.cache_creation_tokens + scoped.cache_read_tokens), 0)::BIGINT,
			COALESCE(SUM(scoped.total_cost), 0),
			COALESCE(SUM(scoped.actual_cost), 0),
			COALESCE(SUM(scoped.account_cost), 0)
		FROM scoped
		LEFT JOIN groups g ON g.id = NULLIF(scoped.group_id, 0)
		GROUP BY scoped.group_id, g.name
		ORDER BY SUM(scoped.input_tokens + scoped.output_tokens + scoped.cache_creation_tokens + scoped.cache_read_tokens) DESC
	`, args...)
	if err != nil {
		return nil, true, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			results = nil
		}
	}()

	results = make([]usagestats.GroupStat, 0)
	for rows.Next() {
		var row usagestats.GroupStat
		if err = rows.Scan(
			&row.GroupID,
			&row.GroupName,
			&row.Requests,
			&row.TotalTokens,
			&row.Cost,
			&row.ActualCost,
			&row.AccountCost,
		); err != nil {
			return nil, true, err
		}
		results = append(results, row)
	}
	if err = rows.Err(); err != nil {
		return nil, true, err
	}
	return results, true, nil
}
