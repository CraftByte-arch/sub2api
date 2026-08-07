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

	accountUsageStatsBackfillLockID int64 = 616394753208471925
	accountUsageStatsBackfillDays         = 90
	accountUsageStatsBackfillPause        = 250 * time.Millisecond
	accountUsageStatsBackfillRetry        = time.Minute
	accountUsageStatsBackfillSettle       = 2 * time.Minute
	accountUsageStatsStatementLimit       = 20 * time.Second
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

// StartAutomaticBackfill builds a verified 90-day aggregate before reads cut
// over. The database triggers already record live INSERT and DELETE statements.
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
			slog.Error("account usage stats backfill failed; will retry", "error", err)
			time.Sleep(accountUsageStatsBackfillRetry)
			continue
		}
		if complete {
			return
		}
		time.Sleep(accountUsageStatsBackfillPause)
	}
}

// GetAccountUsageStatsAggregated has the same inputs and response contract as
// the legacy raw-log query. It transparently falls back until verified coverage
// includes the requested range.
func (r *usageLogRepository) GetAccountUsageStatsAggregated(
	ctx context.Context,
	accountID int64,
	startTime, endTime time.Time,
) (*usagestats.AccountUsageStatsResponse, error) {
	store := r.accountUsageStats
	if store == nil {
		store = newAccountUsageStatsStore(r.sql)
	}
	return store.Get(ctx, accountID, startTime, endTime, r.GetAccountUsageStats)
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
	ready, err := s.isReadyFor(ctx, startDate)
	if err != nil {
		return nil, err
	}
	if !ready {
		return legacy(ctx, accountID, startTime, endTime)
	}
	return s.getAggregated(ctx, accountID, startDate, endDate)
}

func accountUsageStatsWholeDayRange(startTime, endTime time.Time) bool {
	return endTime.After(startTime) &&
		startTime.Equal(timezone.StartOfDay(startTime)) &&
		endTime.Equal(timezone.StartOfDay(endTime))
}

func (s *accountUsageStatsStore) isReadyFor(ctx context.Context, startDate time.Time) (bool, error) {
	var ready bool
	err := scanSingleRow(ctx, s.sql, `
		SELECT ready AND coverage_start IS NOT NULL AND coverage_start <= $1::date
		FROM account_usage_stats_daily_state
		WHERE id = 1
	`, []any{startDate}, &ready)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return ready, err
}

func (s *accountUsageStatsStore) getAggregated(
	ctx context.Context,
	accountID int64,
	startTime, endTime time.Time,
) (*usagestats.AccountUsageStatsResponse, error) {
	rows, err := s.sql.QueryContext(ctx, `
		SELECT
			dimension_type,
			dimension_value,
			CASE
				WHEN dimension_type = 0 THEN bucket_date::TEXT
				ELSE ''
			END AS date,
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
		FROM account_usage_stats_daily
		WHERE account_id = $1
			AND bucket_date >= $2::date
			AND bucket_date < $3::date
			AND requests <> 0
		GROUP BY
			dimension_type,
			dimension_value,
			CASE
				WHEN dimension_type = 0 THEN bucket_date::TEXT
				ELSE ''
			END
	`, accountID, startTime, endTime)
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

	var locked bool
	if err := scanSingleRow(ctx, tx, `SELECT pg_try_advisory_xact_lock($1)`, []any{accountUsageStatsBackfillLockID}, &locked); err != nil {
		return false, err
	}
	if !locked {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL statement_timeout = '20s'`); err != nil {
		return false, err
	}

	var ready bool
	var coverageStartText sql.NullString
	var cursorText sql.NullString
	var updatedAt time.Time
	if err := scanSingleRow(ctx, tx, `
		SELECT ready, coverage_start::TEXT, cursor::TEXT, updated_at
		FROM account_usage_stats_daily_state
		WHERE id = 1
		FOR UPDATE
	`, nil, &ready, &coverageStartText, &cursorText, &updatedAt); err != nil {
		return false, err
	}
	if ready {
		return true, tx.Commit()
	}

	coverageStart, hasCoverageStart, err := parseAccountUsageStatsDate(coverageStartText)
	if err != nil {
		return false, err
	}
	cursor, hasCursor, err := parseAccountUsageStatsDate(cursorText)
	if err != nil {
		return false, err
	}

	today := timezone.Today()
	wantedStart := today.AddDate(0, 0, -(accountUsageStatsBackfillDays - 1))
	if !hasCoverageStart || coverageStart.After(wantedStart) {
		coverageStart = wantedStart
		cursor = wantedStart
		hasCursor = true
	}
	if !hasCursor || cursor.Before(wantedStart) {
		cursor = wantedStart
		if _, err := tx.ExecContext(ctx, `
			UPDATE account_usage_stats_daily_state
			SET coverage_start = $1::date, cursor = $1::date, updated_at = NOW()
			WHERE id = 1
		`, wantedStart); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}

	cursorDay := timezone.StartOfDay(cursor)
	if cursorDay.Before(today) {
		nextDay := cursorDay.AddDate(0, 0, 1)
		if err := s.rebuildDay(ctx, tx, cursorDay, nextDay); err != nil {
			return false, err
		}
		if err := s.verifyDay(ctx, tx, cursorDay, nextDay); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE account_usage_stats_daily_state
			SET coverage_start = $1::date, cursor = $2::date, updated_at = NOW()
			WHERE id = 1
		`, coverageStart, nextDay); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}

	if time.Since(updatedAt) < accountUsageStatsBackfillSettle {
		return false, tx.Commit()
	}

	// Block trigger upserts during the final current-day replacement. Usage-log
	// transactions then resume and add any increments that were not yet visible.
	if _, err := tx.ExecContext(ctx, `LOCK TABLE account_usage_stats_daily IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return false, err
	}
	nextDay := today.AddDate(0, 0, 1)
	if err := s.rebuildDay(ctx, tx, today, nextDay); err != nil {
		return false, err
	}
	if err := s.verifyDay(ctx, tx, today, nextDay); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE account_usage_stats_daily_state
		SET ready = TRUE, coverage_start = $1::date, cursor = $2::date, updated_at = NOW()
		WHERE id = 1
	`, coverageStart, nextDay); err != nil {
		return false, err
	}
	return true, tx.Commit()
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
