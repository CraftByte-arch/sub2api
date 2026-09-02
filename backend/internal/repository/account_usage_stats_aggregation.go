package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

const (
	accountUsageStatsDimensionTotal            int16 = 0
	accountUsageStatsDimensionModel            int16 = 1
	accountUsageStatsDimensionInboundEndpoint  int16 = 2
	accountUsageStatsDimensionUpstreamEndpoint int16 = 3

	accountUsageStatsBackfillLockID      int64 = 616394753208471925
	accountUsageStatsBackfillDays              = 90
	accountUsageStatsBackfillPause             = 250 * time.Millisecond
	accountUsageStatsBackfillRetry             = time.Minute
	accountUsageStatsMaintenanceInterval       = 5 * time.Minute
	accountUsageStatsStatementLimit            = 20 * time.Second
)

type accountUsageStatsStore struct {
	sql sqlExecutor
	db  *sql.DB

	startOnce sync.Once
}

type accountUsageStatsLegacyQuery func(
	ctx context.Context,
	accountID int64,
	startTime, endTime time.Time,
) (*usagestats.AccountUsageStatsResponse, error)

type accountUsageStatsRow struct {
	dimensionType  int16
	dimensionValue string
	date           string
	requests       int64
	inputTokens    int64
	outputTokens   int64
	cacheCreation  int64
	cacheRead      int64
	standardCost   float64
	accountCost    float64
	userCost       float64
	durationSumMs  int64
	durationCount  int64
}

func newAccountUsageStatsStore(sqlq sqlExecutor) *accountUsageStatsStore {
	store := &accountUsageStatsStore{sql: sqlq}
	if db, ok := sqlq.(*sql.DB); ok {
		store.db = db
	}
	return store
}

// StartAutomaticBackfill builds missing closed days and then keeps the closed
// watermark current. It never rebuilds the open application-timezone day.
func (s *accountUsageStatsStore) StartAutomaticBackfill() {
	if s == nil || s.db == nil {
		return
	}
	s.startOnce.Do(func() {
		go s.runBackfill()
	})
}

func (s *accountUsageStatsStore) runBackfill() {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), accountUsageStatsStatementLimit)
		complete, err := s.backfillStep(ctx)
		cancel()
		if err != nil {
			slog.Error("account usage stats maintenance failed; will retry", "error", err)
			time.Sleep(accountUsageStatsBackfillRetry)
			continue
		}
		if complete {
			time.Sleep(accountUsageStatsMaintenanceInterval)
			continue
		}
		time.Sleep(accountUsageStatsBackfillPause)
	}
}

func (s *accountUsageStatsStore) Get(
	ctx context.Context,
	accountID int64,
	startTime, endTime time.Time,
	legacy accountUsageStatsLegacyQuery,
) (*usagestats.AccountUsageStatsResponse, error) {
	if legacy == nil {
		return nil, fmt.Errorf("legacy account usage stats query is required")
	}
	if s == nil || s.sql == nil || accountID <= 0 || !endTime.After(startTime) {
		return legacy(ctx, accountID, startTime, endTime)
	}

	if !accountUsageStatsWholeDayRange(startTime, endTime) {
		return legacy(ctx, accountID, startTime, endTime)
	}

	startDate := timezone.StartOfDay(startTime)
	endDate := timezone.StartOfDay(endTime)
	closedBefore, ready, err := s.closedBeforeFor(ctx, startDate)
	if err != nil {
		if ctx == nil || ctx.Err() != nil {
			return nil, err
		}
		slog.Warn("account usage aggregate state unavailable; falling back to legacy SQL", "error", err)
		return legacy(ctx, accountID, startTime, endTime)
	}
	if !ready {
		return legacy(ctx, accountID, startTime, endTime)
	}
	response, err := s.getHybrid(ctx, accountID, startDate, endDate, closedBefore)
	if err == nil || ctx == nil || ctx.Err() != nil {
		return response, err
	}
	slog.Warn("account usage hybrid query failed; falling back to legacy SQL", "error", err)
	return legacy(ctx, accountID, startTime, endTime)
}

func accountUsageStatsWholeDayRange(startTime, endTime time.Time) bool {
	return endTime.After(startTime) &&
		startTime.Equal(timezone.StartOfDay(startTime)) &&
		endTime.Equal(timezone.StartOfDay(endTime))
}

func (s *accountUsageStatsStore) closedBeforeFor(ctx context.Context, startDate time.Time) (time.Time, bool, error) {
	var ready bool
	var coverageStartText sql.NullString
	var closedBeforeText sql.NullString
	err := scanSingleRow(ctx, s.sql, `
		SELECT ready, coverage_start::TEXT, closed_before::TEXT
		FROM account_usage_stats_daily_state
		WHERE id = 1
	`, nil, &ready, &coverageStartText, &closedBeforeText)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	coverageStart, hasCoverageStart, err := parseAccountUsageStatsDate(coverageStartText)
	if err != nil {
		return time.Time{}, false, err
	}
	closedBefore, hasClosedBefore, err := parseAccountUsageStatsDate(closedBeforeText)
	if err != nil {
		return time.Time{}, false, err
	}
	if !ready || !hasCoverageStart || !hasClosedBefore || coverageStart.After(startDate) || closedBefore.Before(coverageStart) {
		return time.Time{}, false, nil
	}
	if closedBefore.After(timezone.StartOfDay(timezone.Today())) {
		return time.Time{}, false, nil
	}
	return closedBefore, true, nil
}

func lockAccountUsageStatsAggregateTable(ctx context.Context, tx sqlExecutor) error {
	// Stage one still has the legacy synchronous usage-log triggers installed.
	// Serialize a bucket replacement with those trigger upserts so a concurrent
	// historical write cannot be overwritten by the rebuild snapshot. Once the
	// write triggers are removed in stage two, this lock only protects the
	// aggregate table and does not block usage-log writes.
	_, err := tx.ExecContext(ctx, `LOCK TABLE account_usage_stats_daily IN SHARE ROW EXCLUSIVE MODE`)
	return err
}

func (s *accountUsageStatsStore) getHybrid(
	ctx context.Context,
	accountID int64,
	startTime, endTime, closedBefore time.Time,
) (*usagestats.AccountUsageStatsResponse, error) {
	rows, err := s.sql.QueryContext(ctx, `
		WITH raw_usage AS (
			SELECT
				ul.created_at,
				ul.requested_model,
				ul.model,
				ul.inbound_endpoint,
				ul.upstream_endpoint,
				ul.input_tokens,
				ul.output_tokens,
				ul.cache_creation_tokens,
				ul.cache_read_tokens,
				ul.total_cost,
				ul.account_stats_cost,
				ul.account_rate_multiplier,
				ul.actual_cost,
				ul.duration_ms
			FROM usage_logs ul
			WHERE ul.account_id = $1
				AND ul.created_at >= GREATEST($2::TIMESTAMPTZ, $4::TIMESTAMPTZ)
				AND ul.created_at < $3

			UNION ALL

			SELECT
				ul.created_at,
				ul.requested_model,
				ul.model,
				ul.inbound_endpoint,
				ul.upstream_endpoint,
				ul.input_tokens,
				ul.output_tokens,
				ul.cache_creation_tokens,
				ul.cache_read_tokens,
				ul.total_cost,
				ul.account_stats_cost,
				ul.account_rate_multiplier,
				ul.actual_cost,
				ul.duration_ms
			FROM account_usage_stats_dirty_days dirty
			JOIN usage_logs ul
				ON ul.account_id = $1
				AND ul.created_at >= dirty.bucket_date::TIMESTAMPTZ
				AND ul.created_at < (dirty.bucket_date + 1)::TIMESTAMPTZ
			WHERE dirty.bucket_date >= $2::DATE
				AND dirty.bucket_date < LEAST($3::DATE, $4::DATE)
		), combined AS (
			SELECT
				daily.dimension_type,
				daily.dimension_value,
				CASE
					WHEN daily.dimension_type = 0 THEN daily.bucket_date::TEXT
					ELSE ''
				END AS date,
				daily.requests,
				daily.input_tokens,
				daily.output_tokens,
				daily.cache_creation_tokens,
				daily.cache_read_tokens,
				daily.standard_cost,
				daily.account_cost,
				daily.user_cost,
				daily.duration_sum_ms,
				daily.duration_count
			FROM account_usage_stats_daily daily
			WHERE daily.account_id = $1
				AND daily.bucket_date >= $2::date
				AND daily.bucket_date < LEAST($3::date, $4::date)
				AND daily.requests <> 0
				AND NOT EXISTS (
					SELECT 1
					FROM account_usage_stats_dirty_days dirty
					WHERE dirty.bucket_date = daily.bucket_date
				)

			UNION ALL

			SELECT
				dimensions.dimension_type,
				dimensions.dimension_value,
				CASE
					WHEN dimensions.dimension_type = 0 THEN ul.created_at::DATE::TEXT
					ELSE ''
				END AS date,
				1::BIGINT AS requests,
				ul.input_tokens::BIGINT,
				ul.output_tokens::BIGINT,
				ul.cache_creation_tokens::BIGINT,
				ul.cache_read_tokens::BIGINT,
				ul.total_cost AS standard_cost,
				COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1) AS account_cost,
				ul.actual_cost AS user_cost,
				COALESCE(ul.duration_ms, 0)::BIGINT AS duration_sum_ms,
				CASE WHEN ul.duration_ms IS NULL THEN 0 ELSE 1 END::BIGINT AS duration_count
			FROM raw_usage ul
			CROSS JOIN LATERAL (
				VALUES
					(0::SMALLINT, ''::TEXT),
					(1::SMALLINT, COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model)),
					(2::SMALLINT, COALESCE(NULLIF(TRIM(ul.inbound_endpoint), ''), 'unknown')),
					(3::SMALLINT, COALESCE(NULLIF(TRIM(ul.upstream_endpoint), ''), 'unknown'))
			) AS dimensions(dimension_type, dimension_value)
		)
		SELECT
			dimension_type,
			dimension_value,
			date,
			SUM(requests)::BIGINT,
			SUM(input_tokens)::BIGINT,
			SUM(output_tokens)::BIGINT,
			SUM(cache_creation_tokens)::BIGINT,
			SUM(cache_read_tokens)::BIGINT,
			SUM(standard_cost),
			SUM(account_cost),
			SUM(user_cost),
			SUM(duration_sum_ms)::BIGINT,
			SUM(duration_count)::BIGINT
		FROM combined
		GROUP BY dimension_type, dimension_value, date
		HAVING SUM(requests) <> 0
	`, accountID, startTime, endTime, closedBefore)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	aggregatedRows := make([]accountUsageStatsRow, 0)
	for rows.Next() {
		var row accountUsageStatsRow
		if err := rows.Scan(
			&row.dimensionType,
			&row.dimensionValue,
			&row.date,
			&row.requests,
			&row.inputTokens,
			&row.outputTokens,
			&row.cacheCreation,
			&row.cacheRead,
			&row.standardCost,
			&row.accountCost,
			&row.userCost,
			&row.durationSumMs,
			&row.durationCount,
		); err != nil {
			return nil, err
		}
		aggregatedRows = append(aggregatedRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return buildAccountUsageStatsResponse(aggregatedRows, startTime, endTime), nil
}

func buildAccountUsageStatsResponse(
	rows []accountUsageStatsRow,
	startTime, endTime time.Time,
) *usagestats.AccountUsageStatsResponse {
	history := make([]usagestats.AccountUsageHistory, 0)
	models := make([]usagestats.ModelStat, 0)
	endpoints := make([]usagestats.EndpointStat, 0)
	upstreamEndpoints := make([]usagestats.EndpointStat, 0)
	var durationSum, durationCount int64

	for _, row := range rows {
		totalTokens := row.inputTokens + row.outputTokens + row.cacheCreation + row.cacheRead
		switch row.dimensionType {
		case accountUsageStatsDimensionTotal:
			parsed, _ := time.Parse("2006-01-02", row.date)
			history = append(history, usagestats.AccountUsageHistory{
				Date:       row.date,
				Label:      parsed.Format("01/02"),
				Requests:   row.requests,
				Tokens:     totalTokens,
				Cost:       row.standardCost,
				ActualCost: row.accountCost,
				UserCost:   row.userCost,
			})
			durationSum += row.durationSumMs
			durationCount += row.durationCount
		case accountUsageStatsDimensionModel:
			models = append(models, usagestats.ModelStat{
				Model:               row.dimensionValue,
				Requests:            row.requests,
				InputTokens:         row.inputTokens,
				OutputTokens:        row.outputTokens,
				CacheCreationTokens: row.cacheCreation,
				CacheReadTokens:     row.cacheRead,
				TotalTokens:         totalTokens,
				Cost:                row.standardCost,
				ActualCost:          row.accountCost,
				AccountCost:         row.accountCost,
			})
		case accountUsageStatsDimensionInboundEndpoint:
			endpoints = append(endpoints, usagestats.EndpointStat{
				Endpoint: row.dimensionValue, Requests: row.requests, TotalTokens: totalTokens,
				Cost: row.standardCost, ActualCost: row.accountCost,
			})
		case accountUsageStatsDimensionUpstreamEndpoint:
			upstreamEndpoints = append(upstreamEndpoints, usagestats.EndpointStat{
				Endpoint: row.dimensionValue, Requests: row.requests, TotalTokens: totalTokens,
				Cost: row.standardCost, ActualCost: row.accountCost,
			})
		}
	}

	sort.Slice(history, func(i, j int) bool { return history[i].Date < history[j].Date })
	sort.Slice(models, func(i, j int) bool { return models[i].TotalTokens > models[j].TotalTokens })
	sort.SliceStable(endpoints, func(i, j int) bool { return endpoints[i].Requests > endpoints[j].Requests })
	sort.SliceStable(upstreamEndpoints, func(i, j int) bool { return upstreamEndpoints[i].Requests > upstreamEndpoints[j].Requests })

	resp := &usagestats.AccountUsageStatsResponse{
		History: history, Models: models, Endpoints: endpoints, UpstreamEndpoints: upstreamEndpoints,
	}
	resp.Summary = summarizeAccountUsageStats(history, durationSum, durationCount, startTime, endTime)
	return resp
}

func summarizeAccountUsageStats(
	history []usagestats.AccountUsageHistory,
	durationSum, durationCount int64,
	startTime, endTime time.Time,
) usagestats.AccountUsageSummary {
	daysCount := int(endTime.Sub(startTime).Hours()/24) + 1
	if daysCount <= 0 {
		daysCount = 30
	}

	var totalAccountCost, totalUserCost, totalStandardCost float64
	var totalRequests, totalTokens int64
	var highestCostDay, highestRequestDay *usagestats.AccountUsageHistory
	for i := range history {
		row := &history[i]
		totalAccountCost += row.ActualCost
		totalUserCost += row.UserCost
		totalStandardCost += row.Cost
		totalRequests += row.Requests
		totalTokens += row.Tokens
		if highestCostDay == nil || row.ActualCost > highestCostDay.ActualCost {
			highestCostDay = row
		}
		if highestRequestDay == nil || row.Requests > highestRequestDay.Requests {
			highestRequestDay = row
		}
	}

	actualDaysUsed := len(history)
	if actualDaysUsed == 0 {
		actualDaysUsed = 1
	}
	summary := usagestats.AccountUsageSummary{
		Days: daysCount, ActualDaysUsed: actualDaysUsed,
		TotalCost: totalAccountCost, TotalUserCost: totalUserCost, TotalStandardCost: totalStandardCost,
		TotalRequests: totalRequests, TotalTokens: totalTokens,
		AvgDailyCost:     totalAccountCost / float64(actualDaysUsed),
		AvgDailyUserCost: totalUserCost / float64(actualDaysUsed),
		AvgDailyRequests: float64(totalRequests) / float64(actualDaysUsed),
		AvgDailyTokens:   float64(totalTokens) / float64(actualDaysUsed),
	}
	if durationCount > 0 {
		summary.AvgDurationMs = float64(durationSum) / float64(durationCount)
	}

	today := timezone.Now().Format("2006-01-02")
	for i := range history {
		if history[i].Date == today {
			summary.Today = &struct {
				Date     string  `json:"date"`
				Cost     float64 `json:"cost"`
				UserCost float64 `json:"user_cost"`
				Requests int64   `json:"requests"`
				Tokens   int64   `json:"tokens"`
			}{history[i].Date, history[i].ActualCost, history[i].UserCost, history[i].Requests, history[i].Tokens}
			break
		}
	}
	if highestCostDay != nil {
		summary.HighestCostDay = &struct {
			Date     string  `json:"date"`
			Label    string  `json:"label"`
			Cost     float64 `json:"cost"`
			UserCost float64 `json:"user_cost"`
			Requests int64   `json:"requests"`
		}{highestCostDay.Date, highestCostDay.Label, highestCostDay.ActualCost, highestCostDay.UserCost, highestCostDay.Requests}
	}
	if highestRequestDay != nil {
		summary.HighestRequestDay = &struct {
			Date     string  `json:"date"`
			Label    string  `json:"label"`
			Requests int64   `json:"requests"`
			Cost     float64 `json:"cost"`
			UserCost float64 `json:"user_cost"`
		}{highestRequestDay.Date, highestRequestDay.Label, highestRequestDay.Requests, highestRequestDay.ActualCost, highestRequestDay.UserCost}
	}
	return summary
}

func (s *accountUsageStatsStore) backfillStep(ctx context.Context) (complete bool, err error) {
	if s == nil || s.db == nil {
		return true, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	locked, err := tryUsageAggregationMaintenanceLocks(ctx, tx, accountUsageStatsBackfillLockID)
	if err != nil {
		return false, err
	}
	if !locked {
		return false, nil
	}
	if err := configureUsageAggregationMaintenance(ctx, tx); err != nil {
		return false, err
	}

	var ready bool
	var coverageStartText sql.NullString
	var cursorText sql.NullString
	var closedBeforeText sql.NullString
	if err := scanSingleRow(ctx, tx, `
		SELECT ready, coverage_start::TEXT, cursor::TEXT, closed_before::TEXT
		FROM account_usage_stats_daily_state
		WHERE id = 1
		FOR UPDATE
	`, nil, &ready, &coverageStartText, &cursorText, &closedBeforeText); err != nil {
		return false, err
	}

	coverageStart, hasCoverageStart, err := parseAccountUsageStatsDate(coverageStartText)
	if err != nil {
		return false, err
	}
	cursor, hasCursor, err := parseAccountUsageStatsDate(cursorText)
	if err != nil {
		return false, err
	}
	closedBefore, hasClosedBefore, err := parseAccountUsageStatsDate(closedBeforeText)
	if err != nil {
		return false, err
	}

	today := timezone.Today()
	wantedStart := today.AddDate(0, 0, -(accountUsageStatsBackfillDays - 1))
	if !ready {
		if !hasCoverageStart || coverageStart.After(wantedStart) {
			coverageStart = wantedStart
			cursor = wantedStart
			hasCursor = true
			if _, err := tx.ExecContext(ctx, `
				UPDATE account_usage_stats_daily_state
				SET ready = FALSE,
					coverage_start = $1::DATE,
					cursor = $1::DATE,
					closed_before = NULL,
					updated_at = NOW()
				WHERE id = 1
			`, wantedStart); err != nil {
				return false, err
			}
			return false, tx.Commit()
		}
		if !hasCursor || cursor.Before(wantedStart) {
			cursor = wantedStart
			hasCursor = true
		}
		if !hasCursor {
			cursor = coverageStart
		}

		cursorDay := timezone.StartOfDay(cursor)
		if cursorDay.Before(today) {
			if err := lockAccountUsageStatsAggregateTable(ctx, tx); err != nil {
				return false, err
			}
			nextDay := cursorDay.AddDate(0, 0, 1)
			if err := s.rebuildDay(ctx, tx, cursorDay, nextDay); err != nil {
				return false, err
			}
			if err := s.verifyDay(ctx, tx, cursorDay, nextDay); err != nil {
				return false, err
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE account_usage_stats_daily_state
				SET coverage_start = $1::DATE,
					cursor = $2::DATE,
					closed_before = NULL,
					updated_at = NOW()
				WHERE id = 1
			`, coverageStart, nextDay); err != nil {
				return false, err
			}
			return false, tx.Commit()
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE account_usage_stats_daily_state
			SET ready = TRUE,
				coverage_start = $1::DATE,
				cursor = $2::DATE,
				closed_before = $2::DATE,
				updated_at = NOW()
			WHERE id = 1
		`, coverageStart, today); err != nil {
			return false, err
		}
		return true, tx.Commit()
	}

	if !hasCoverageStart {
		if _, err := tx.ExecContext(ctx, `
			UPDATE account_usage_stats_daily_state
			SET ready = FALSE,
				coverage_start = $1::DATE,
				cursor = $1::DATE,
				closed_before = NULL,
				updated_at = NOW()
			WHERE id = 1
		`, wantedStart); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}

	if !hasClosedBefore {
		closedBefore = today
		if hasCursor && cursor.Before(closedBefore) {
			closedBefore = timezone.StartOfDay(cursor)
		}
		if closedBefore.Before(coverageStart) {
			closedBefore = coverageStart
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE account_usage_stats_daily_state
			SET cursor = $1::DATE, closed_before = $1::DATE, updated_at = NOW()
			WHERE id = 1
		`, closedBefore); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}

	if closedBefore.Before(coverageStart) {
		if _, err := tx.ExecContext(ctx, `
			UPDATE account_usage_stats_daily_state
			SET ready = FALSE,
				coverage_start = $1::DATE,
				cursor = $1::DATE,
				closed_before = NULL,
				updated_at = NOW()
			WHERE id = 1
		`, wantedStart); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}

	legacyTriggersInstalled, err := accountUsageStatsSynchronousTriggersInstalled(ctx, tx)
	if err != nil {
		return false, err
	}
	if legacyTriggersInstalled {
		// Stage one keeps the legacy INSERT/DELETE triggers, which already maintain
		// every aggregate bucket synchronously. Advance the closed watermark without
		// rescanning usage_logs or taking the aggregate-table rebuild lock. Once the
		// stage-two migration removes those triggers, normal closed-day maintenance
		// resumes from this watermark.
		if _, err := tx.ExecContext(ctx, `
			UPDATE account_usage_stats_daily_state
			SET cursor = $1::DATE,
				closed_before = $1::DATE,
				updated_at = NOW()
			WHERE id = 1
				AND (cursor IS DISTINCT FROM $1::DATE OR closed_before IS DISTINCT FROM $1::DATE)
		`, today); err != nil {
			return false, err
		}
		return true, tx.Commit()
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM account_usage_stats_dirty_days
		WHERE bucket_date < $1::DATE
	`, coverageStart); err != nil {
		return false, err
	}

	var dirtyDayText string
	err = scanSingleRow(ctx, tx, `
		SELECT bucket_date::TEXT
		FROM account_usage_stats_dirty_days
		WHERE bucket_date >= $1::DATE
			AND bucket_date < $2::DATE
		ORDER BY bucket_date
		LIMIT 1
	`, []any{coverageStart, closedBefore}, &dirtyDayText)
	if err == nil {
		dirtyDay, err := time.ParseInLocation("2006-01-02", dirtyDayText, timezone.Location())
		if err != nil {
			return false, fmt.Errorf("parse dirty account usage stats date %q: %w", dirtyDayText, err)
		}
		nextDay := dirtyDay.AddDate(0, 0, 1)
		if err := lockAccountUsageStatsAggregateTable(ctx, tx); err != nil {
			return false, err
		}
		if err := s.rebuildDay(ctx, tx, dirtyDay, nextDay); err != nil {
			return false, err
		}
		if err := s.verifyDay(ctx, tx, dirtyDay, nextDay); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM account_usage_stats_dirty_days WHERE bucket_date = $1::DATE
		`, dirtyDay); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	closedDay := timezone.StartOfDay(closedBefore)
	if !closedDay.Before(today) {
		return true, tx.Commit()
	}
	nextDay := closedDay.AddDate(0, 0, 1)
	if err := lockAccountUsageStatsAggregateTable(ctx, tx); err != nil {
		return false, err
	}
	if err := s.rebuildDay(ctx, tx, closedDay, nextDay); err != nil {
		return false, err
	}
	if err := s.verifyDay(ctx, tx, closedDay, nextDay); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM account_usage_stats_dirty_days
		WHERE bucket_date = $1::DATE
	`, closedDay); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE account_usage_stats_daily_state
		SET cursor = $1::DATE, closed_before = $1::DATE, updated_at = NOW()
		WHERE id = 1
	`, nextDay); err != nil {
		return false, err
	}
	return !nextDay.Before(today), tx.Commit()
}

func accountUsageStatsSynchronousTriggersInstalled(ctx context.Context, sqlq sqlExecutor) (bool, error) {
	var installed bool
	err := scanSingleRow(ctx, sqlq, `
		SELECT COUNT(*) = 2
		FROM pg_trigger
		WHERE tgrelid = 'usage_logs'::regclass
			AND NOT tgisinternal
			AND tgname IN (
				'trg_account_usage_stats_daily_insert',
				'trg_account_usage_stats_daily_delete'
			)
	`, nil, &installed)
	return installed, err
}

func parseAccountUsageStatsDate(value sql.NullString) (time.Time, bool, error) {
	if !value.Valid || value.String == "" {
		return time.Time{}, false, nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", value.String, timezone.Location())
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse account usage stats date %q: %w", value.String, err)
	}
	return parsed, true, nil
}

func (s *accountUsageStatsStore) rebuildDay(ctx context.Context, sqlq sqlExecutor, start, end time.Time) error {
	if !end.After(start) {
		return nil
	}
	if _, err := sqlq.ExecContext(ctx, `DELETE FROM account_usage_stats_daily WHERE bucket_date = $1::date`, start); err != nil {
		return err
	}
	_, err := sqlq.ExecContext(ctx, `
		INSERT INTO account_usage_stats_daily (
			account_id, bucket_date, dimension_type, dimension_value,
			requests, input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
			standard_cost, account_cost, user_cost, duration_sum_ms, duration_count, updated_at
		)
		SELECT
			expanded.account_id,
			expanded.bucket_date,
			expanded.dimension_type,
			expanded.dimension_value,
			COUNT(*)::BIGINT,
			SUM(expanded.input_tokens)::BIGINT,
			SUM(expanded.output_tokens)::BIGINT,
			SUM(expanded.cache_creation_tokens)::BIGINT,
			SUM(expanded.cache_read_tokens)::BIGINT,
			SUM(expanded.total_cost),
			SUM(expanded.account_cost),
			SUM(expanded.actual_cost),
			SUM(COALESCE(expanded.duration_ms, 0))::BIGINT,
			COUNT(expanded.duration_ms)::BIGINT,
			NOW()
		FROM (
			SELECT
				ul.account_id,
				ul.created_at::DATE AS bucket_date,
				dimensions.dimension_type,
				dimensions.dimension_value,
				ul.input_tokens,
				ul.output_tokens,
				ul.cache_creation_tokens,
				ul.cache_read_tokens,
				ul.total_cost,
				COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1) AS account_cost,
				ul.actual_cost,
				ul.duration_ms
			FROM usage_logs ul
			CROSS JOIN LATERAL (
				VALUES
					(0::SMALLINT, ''::TEXT),
					(1::SMALLINT, COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model)),
					(2::SMALLINT, COALESCE(NULLIF(TRIM(ul.inbound_endpoint), ''), 'unknown')),
					(3::SMALLINT, COALESCE(NULLIF(TRIM(ul.upstream_endpoint), ''), 'unknown'))
			) AS dimensions(dimension_type, dimension_value)
			WHERE ul.account_id > 0
				AND ul.created_at >= $1
				AND ul.created_at < $2
		) expanded
		GROUP BY expanded.account_id, expanded.bucket_date, expanded.dimension_type, expanded.dimension_value
		ON CONFLICT (account_id, bucket_date, dimension_type, dimension_value)
		DO UPDATE SET
			requests = EXCLUDED.requests,
			input_tokens = EXCLUDED.input_tokens,
			output_tokens = EXCLUDED.output_tokens,
			cache_creation_tokens = EXCLUDED.cache_creation_tokens,
			cache_read_tokens = EXCLUDED.cache_read_tokens,
			standard_cost = EXCLUDED.standard_cost,
			account_cost = EXCLUDED.account_cost,
			user_cost = EXCLUDED.user_cost,
			duration_sum_ms = EXCLUDED.duration_sum_ms,
			duration_count = EXCLUDED.duration_count,
			updated_at = NOW()
	`, start, end)
	return err
}

func (s *accountUsageStatsStore) verifyDay(ctx context.Context, sqlq sqlExecutor, start, end time.Time) error {
	var mismatch bool
	err := scanSingleRow(ctx, sqlq, `
		WITH raw AS (
			SELECT
				expanded.account_id,
				expanded.bucket_date,
				expanded.dimension_type,
				expanded.dimension_value,
				COUNT(*)::BIGINT AS requests,
				SUM(expanded.input_tokens)::BIGINT AS input_tokens,
				SUM(expanded.output_tokens)::BIGINT AS output_tokens,
				SUM(expanded.cache_creation_tokens)::BIGINT AS cache_creation_tokens,
				SUM(expanded.cache_read_tokens)::BIGINT AS cache_read_tokens,
				SUM(expanded.total_cost) AS standard_cost,
				SUM(expanded.account_cost) AS account_cost,
				SUM(expanded.actual_cost) AS user_cost,
				SUM(COALESCE(expanded.duration_ms, 0))::BIGINT AS duration_sum_ms,
				COUNT(expanded.duration_ms)::BIGINT AS duration_count
			FROM (
				SELECT
					ul.account_id,
					ul.created_at::DATE AS bucket_date,
					dimensions.dimension_type,
					dimensions.dimension_value,
					ul.input_tokens,
					ul.output_tokens,
					ul.cache_creation_tokens,
					ul.cache_read_tokens,
					ul.total_cost,
					COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1) AS account_cost,
					ul.actual_cost,
					ul.duration_ms
				FROM usage_logs ul
				CROSS JOIN LATERAL (
					VALUES
						(0::SMALLINT, ''::TEXT),
						(1::SMALLINT, COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model)),
						(2::SMALLINT, COALESCE(NULLIF(TRIM(ul.inbound_endpoint), ''), 'unknown')),
						(3::SMALLINT, COALESCE(NULLIF(TRIM(ul.upstream_endpoint), ''), 'unknown'))
				) AS dimensions(dimension_type, dimension_value)
				WHERE ul.account_id > 0 AND ul.created_at >= $1 AND ul.created_at < $2
			) expanded
			GROUP BY expanded.account_id, expanded.bucket_date, expanded.dimension_type, expanded.dimension_value
		), aggregate AS (
			SELECT
				account_id, bucket_date, dimension_type, dimension_value,
				requests, input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
				standard_cost, account_cost, user_cost, duration_sum_ms, duration_count
			FROM account_usage_stats_daily
			WHERE bucket_date = $1::date AND requests <> 0
		)
		SELECT EXISTS (
			SELECT 1
			FROM raw
			FULL JOIN aggregate USING (account_id, bucket_date, dimension_type, dimension_value)
			WHERE COALESCE(raw.requests, 0) <> COALESCE(aggregate.requests, 0)
				OR COALESCE(raw.input_tokens, 0) <> COALESCE(aggregate.input_tokens, 0)
				OR COALESCE(raw.output_tokens, 0) <> COALESCE(aggregate.output_tokens, 0)
				OR COALESCE(raw.cache_creation_tokens, 0) <> COALESCE(aggregate.cache_creation_tokens, 0)
				OR COALESCE(raw.cache_read_tokens, 0) <> COALESCE(aggregate.cache_read_tokens, 0)
				OR COALESCE(raw.standard_cost, 0) <> COALESCE(aggregate.standard_cost, 0)
				OR COALESCE(raw.account_cost, 0) <> COALESCE(aggregate.account_cost, 0)
				OR COALESCE(raw.user_cost, 0) <> COALESCE(aggregate.user_cost, 0)
				OR COALESCE(raw.duration_sum_ms, 0) <> COALESCE(aggregate.duration_sum_ms, 0)
				OR COALESCE(raw.duration_count, 0) <> COALESCE(aggregate.duration_count, 0)
		)
	`, []any{start, end}, &mismatch)
	if err != nil {
		return err
	}
	if mismatch {
		return fmt.Errorf("account usage stats verification mismatch for %s", start.Format("2006-01-02"))
	}
	return nil
}
