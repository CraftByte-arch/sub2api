package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	userDashboardStatsBackfillLockID int64 = 528276810991742311

	userDashboardStatsBackfillPause        = 250 * time.Millisecond
	userDashboardStatsBackfillRetry        = time.Minute
	userDashboardStatsBackfillSettle       = 2 * time.Minute
	userDashboardStatsStatementLimit       = 20 * time.Second
	userDashboardStatsMaintenanceInterval  = time.Hour
	userDashboardStatsPerformanceWindowMin = int64(5)
)

// userDashboardStatsStore keeps the ordinary-user dashboard aggregation
// isolated from the legacy usage-log repository implementation. Reads remain
// disabled until the resumable historical backfill has been verified.
type userDashboardStatsStore struct {
	sql sqlExecutor
	db  *sql.DB

	startOnce sync.Once
}

func newUserDashboardStatsStore(sqlq sqlExecutor) *userDashboardStatsStore {
	store := &userDashboardStatsStore{sql: sqlq}
	if db, ok := sqlq.(*sql.DB); ok {
		store.db = db
	}
	return store
}

func (s *userDashboardStatsStore) StartAutomaticBackfill() {
	if s == nil || s.db == nil {
		return
	}
	s.startOnce.Do(func() {
		go s.runBackfillAndMaintenance()
	})
}

func (s *userDashboardStatsStore) runBackfillAndMaintenance() {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), userDashboardStatsStatementLimit)
		complete, err := s.backfillStep(ctx)
		cancel()
		if err != nil {
			slog.Error("user dashboard stats backfill failed; will retry", "error", err)
			time.Sleep(userDashboardStatsBackfillRetry)
			continue
		}
		if complete {
			break
		}
		time.Sleep(userDashboardStatsBackfillPause)
	}

	ticker := time.NewTicker(userDashboardStatsMaintenanceInterval)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), userDashboardStatsStatementLimit)
		err := s.reconcileRetention(ctx)
		cancel()
		if err != nil {
			slog.Error("user dashboard stats retention reconciliation failed", "error", err)
		}
	}
}

// Get returns handled=false until verified aggregate coverage is ready. The
// caller then transparently falls back to the unchanged legacy raw-log query.
func (s *userDashboardStatsStore) Get(ctx context.Context, userID int64) (*usagestats.UserDashboardStats, bool, error) {
	if s == nil || s.sql == nil || userID <= 0 {
		return nil, false, nil
	}

	ready, err := s.isReady(ctx)
	if err != nil {
		return nil, false, err
	}
	if !ready {
		return nil, false, nil
	}

	stats := &usagestats.UserDashboardStats{}
	if err := s.fillAPIKeyStats(ctx, userID, stats); err != nil {
		return nil, false, err
	}
	if err := s.fillUsageStats(ctx, userID, stats); err != nil {
		return nil, false, err
	}
	if err := s.fillPerformanceStats(ctx, userID, stats); err != nil {
		return nil, false, err
	}
	if err := s.fillPlatformStats(ctx, userID, stats); err != nil {
		return nil, false, err
	}
	return stats, true, nil
}

func (s *userDashboardStatsStore) isReady(ctx context.Context) (bool, error) {
	var ready bool
	err := scanSingleRow(ctx, s.sql, `
		SELECT ready
		FROM user_dashboard_route_daily_state
		WHERE id = 1
	`, nil, &ready)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return ready, err
}

func (s *userDashboardStatsStore) fillAPIKeyStats(ctx context.Context, userID int64, stats *usagestats.UserDashboardStats) error {
	return scanSingleRow(ctx, s.sql, `
		SELECT
			COUNT(*) AS total_api_keys,
			COUNT(*) FILTER (WHERE status = $2) AS active_api_keys
		FROM api_keys
		WHERE user_id = $1
			AND deleted_at IS NULL
	`, []any{userID, service.StatusActive}, &stats.TotalAPIKeys, &stats.ActiveAPIKeys)
}

func (s *userDashboardStatsStore) fillUsageStats(ctx context.Context, userID int64, stats *usagestats.UserDashboardStats) error {
	var durationSumMs int64
	var durationCount int64
	today := timezone.Today()
	err := scanSingleRow(ctx, s.sql, `
		WITH retention AS (
			SELECT MIN(created_at)::DATE AS retained_from
			FROM usage_logs
		), scoped AS (
			SELECT daily.*
			FROM user_dashboard_route_daily daily
			CROSS JOIN retention
			WHERE daily.user_id = $1
				AND retention.retained_from IS NOT NULL
				AND daily.bucket_date >= retention.retained_from
		)
		SELECT
			COALESCE(SUM(requests), 0)::BIGINT,
			COALESCE(SUM(input_tokens), 0)::BIGINT,
			COALESCE(SUM(output_tokens), 0)::BIGINT,
			COALESCE(SUM(cache_creation_tokens), 0)::BIGINT,
			COALESCE(SUM(cache_read_tokens), 0)::BIGINT,
			COALESCE(SUM(total_cost), 0),
			COALESCE(SUM(actual_cost), 0),
			COALESCE(SUM(duration_sum_ms), 0)::BIGINT,
			COALESCE(SUM(duration_count), 0)::BIGINT,
			COALESCE(SUM(requests) FILTER (WHERE bucket_date = $2::DATE), 0)::BIGINT,
			COALESCE(SUM(input_tokens) FILTER (WHERE bucket_date = $2::DATE), 0)::BIGINT,
			COALESCE(SUM(output_tokens) FILTER (WHERE bucket_date = $2::DATE), 0)::BIGINT,
			COALESCE(SUM(cache_creation_tokens) FILTER (WHERE bucket_date = $2::DATE), 0)::BIGINT,
			COALESCE(SUM(cache_read_tokens) FILTER (WHERE bucket_date = $2::DATE), 0)::BIGINT,
			COALESCE(SUM(total_cost) FILTER (WHERE bucket_date = $2::DATE), 0),
			COALESCE(SUM(actual_cost) FILTER (WHERE bucket_date = $2::DATE), 0)
		FROM scoped
	`, []any{userID, today},
		&stats.TotalRequests,
		&stats.TotalInputTokens,
		&stats.TotalOutputTokens,
		&stats.TotalCacheCreationTokens,
		&stats.TotalCacheReadTokens,
		&stats.TotalCost,
		&stats.TotalActualCost,
		&durationSumMs,
		&durationCount,
		&stats.TodayRequests,
		&stats.TodayInputTokens,
		&stats.TodayOutputTokens,
		&stats.TodayCacheCreationTokens,
		&stats.TodayCacheReadTokens,
		&stats.TodayCost,
		&stats.TodayActualCost,
	)
	if err != nil {
		return err
	}

	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheCreationTokens + stats.TotalCacheReadTokens
	stats.TodayTokens = stats.TodayInputTokens + stats.TodayOutputTokens + stats.TodayCacheCreationTokens + stats.TodayCacheReadTokens
	if durationCount > 0 {
		stats.AverageDurationMs = float64(durationSumMs) / float64(durationCount)
	}
	return nil
}

func (s *userDashboardStatsStore) fillPerformanceStats(ctx context.Context, userID int64, stats *usagestats.UserDashboardStats) error {
	windowStart := time.Now().Add(-time.Duration(userDashboardStatsPerformanceWindowMin) * time.Minute)
	var requests int64
	var tokens int64
	if err := scanSingleRow(ctx, s.sql, `
		SELECT
			COUNT(*) AS request_count,
			COALESCE(SUM(input_tokens + output_tokens), 0) AS token_count
		FROM usage_logs
		WHERE created_at >= $1
			AND user_id = $2
	`, []any{windowStart, userID}, &requests, &tokens); err != nil {
		return err
	}
	stats.Rpm = requests / userDashboardStatsPerformanceWindowMin
	stats.Tpm = tokens / userDashboardStatsPerformanceWindowMin
	return nil
}

func (s *userDashboardStatsStore) fillPlatformStats(ctx context.Context, userID int64, stats *usagestats.UserDashboardStats) (err error) {
	today := timezone.Today()
	rows, err := s.sql.QueryContext(ctx, `
		WITH retention AS (
			SELECT MIN(created_at)::DATE AS retained_from
			FROM usage_logs
		), routes AS (
			SELECT
				daily.group_id,
				daily.account_id,
				SUM(daily.billable_requests)::BIGINT AS total_requests,
				SUM(daily.billable_tokens)::BIGINT AS total_tokens,
				SUM(daily.billable_actual_cost) AS total_actual_cost,
				COALESCE(SUM(daily.billable_requests) FILTER (WHERE daily.bucket_date = $2::DATE), 0)::BIGINT AS today_requests,
				COALESCE(SUM(daily.billable_tokens) FILTER (WHERE daily.bucket_date = $2::DATE), 0)::BIGINT AS today_tokens,
				COALESCE(SUM(daily.billable_actual_cost) FILTER (WHERE daily.bucket_date = $2::DATE), 0) AS today_actual_cost
			FROM user_dashboard_route_daily daily
			CROSS JOIN retention
			WHERE daily.user_id = $1
				AND retention.retained_from IS NOT NULL
				AND daily.bucket_date >= retention.retained_from
				AND daily.billable_requests <> 0
			GROUP BY daily.group_id, daily.account_id
		), resolved AS (
			SELECT
				CASE
					WHEN g.platform = 'composite' THEN a.platform
					ELSE COALESCE(NULLIF(g.platform, ''), a.platform)
				END AS platform,
				routes.total_requests,
				routes.total_tokens,
				routes.total_actual_cost,
				routes.today_requests,
				routes.today_tokens,
				routes.today_actual_cost
			FROM routes
			LEFT JOIN groups g ON g.id = NULLIF(routes.group_id, 0)
			LEFT JOIN accounts a ON a.id = routes.account_id
		)
		SELECT
			platform,
			SUM(total_requests)::BIGINT,
			SUM(total_tokens)::BIGINT,
			SUM(total_actual_cost),
			SUM(today_requests)::BIGINT,
			SUM(today_tokens)::BIGINT,
			SUM(today_actual_cost)
		FROM resolved
		WHERE platform IS NOT NULL
			AND platform <> ''
		GROUP BY platform
		ORDER BY SUM(total_actual_cost) DESC
	`, userID, today)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			stats.ByPlatform = nil
		}
	}()

	for rows.Next() {
		var platform usagestats.PlatformDashboardStats
		if err := rows.Scan(
			&platform.Platform,
			&platform.TotalRequests,
			&platform.TotalTokens,
			&platform.TotalActualCost,
			&platform.TodayRequests,
			&platform.TodayTokens,
			&platform.TodayActualCost,
		); err != nil {
			return err
		}
		stats.ByPlatform = append(stats.ByPlatform, platform)
	}
	return rows.Err()
}

func (s *userDashboardStatsStore) backfillStep(ctx context.Context) (complete bool, err error) {
	if s == nil || s.db == nil {
		return true, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var locked bool
	if err := scanSingleRow(ctx, tx, `SELECT pg_try_advisory_xact_lock($1)`, []any{userDashboardStatsBackfillLockID}, &locked); err != nil {
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
		FROM user_dashboard_route_daily_state
		WHERE id = 1
		FOR UPDATE
	`, nil, &ready, &coverageStartText, &cursorText, &updatedAt); err != nil {
		return false, err
	}
	if ready {
		return true, tx.Commit()
	}

	wantedStart, err := userDashboardStatsRawStart(ctx, tx)
	if err != nil {
		return false, err
	}
	coverageStart, hasCoverageStart, err := parseUserDashboardStatsDate(coverageStartText)
	if err != nil {
		return false, err
	}
	cursor, hasCursor, err := parseUserDashboardStatsDate(cursorText)
	if err != nil {
		return false, err
	}

	if !hasCoverageStart || coverageStart.After(wantedStart) {
		coverageStart = wantedStart
		cursor = wantedStart
		hasCursor = true
	}
	if !hasCursor || cursor.Before(wantedStart) {
		cursor = wantedStart
		if _, err := tx.ExecContext(ctx, `
			UPDATE user_dashboard_route_daily_state
			SET coverage_start = $1::DATE, cursor = $1::DATE, updated_at = NOW()
			WHERE id = 1
		`, wantedStart); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}

	today := timezone.Today()
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
			UPDATE user_dashboard_route_daily_state
			SET coverage_start = $1::DATE, cursor = $2::DATE, updated_at = NOW()
			WHERE id = 1
		`, coverageStart, nextDay); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}

	if time.Since(updatedAt) < userDashboardStatsBackfillSettle {
		return false, tx.Commit()
	}

	// Block trigger upserts during the final current-day replacement. New raw
	// log transactions resume after commit and add any increments not visible to
	// this transaction's rebuild statement.
	if _, err := tx.ExecContext(ctx, `LOCK TABLE user_dashboard_route_daily IN SHARE ROW EXCLUSIVE MODE`); err != nil {
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
		UPDATE user_dashboard_route_daily_state
		SET ready = TRUE, coverage_start = $1::DATE, cursor = $2::DATE, updated_at = NOW()
		WHERE id = 1
	`, coverageStart, nextDay); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func userDashboardStatsRawStart(ctx context.Context, sqlq sqlExecutor) (time.Time, error) {
	var value string
	if err := scanSingleRow(ctx, sqlq, `
		SELECT COALESCE(MIN(created_at)::DATE::TEXT, CURRENT_DATE::TEXT)
		FROM usage_logs
	`, nil, &value); err != nil {
		return time.Time{}, err
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, timezone.Location())
	if err != nil {
		return time.Time{}, fmt.Errorf("parse user dashboard raw start %q: %w", value, err)
	}
	return parsed, nil
}

func parseUserDashboardStatsDate(value sql.NullString) (time.Time, bool, error) {
	if !value.Valid || value.String == "" {
		return time.Time{}, false, nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", value.String, timezone.Location())
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse user dashboard stats date %q: %w", value.String, err)
	}
	return parsed, true, nil
}

func (s *userDashboardStatsStore) rebuildDay(ctx context.Context, sqlq sqlExecutor, start, end time.Time) error {
	if !end.After(start) {
		return nil
	}
	if _, err := sqlq.ExecContext(ctx, `DELETE FROM user_dashboard_route_daily WHERE bucket_date = $1::DATE`, start); err != nil {
		return err
	}
	_, err := sqlq.ExecContext(ctx, `
		INSERT INTO user_dashboard_route_daily (
			user_id, bucket_date, group_id, account_id,
			requests, input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
			total_cost, actual_cost, duration_sum_ms, duration_count,
			billable_requests, billable_tokens, billable_actual_cost, updated_at
		)
		SELECT
			user_id,
			created_at::DATE,
			COALESCE(group_id, 0),
			account_id,
			COUNT(*)::BIGINT,
			COALESCE(SUM(input_tokens), 0)::BIGINT,
			COALESCE(SUM(output_tokens), 0)::BIGINT,
			COALESCE(SUM(cache_creation_tokens), 0)::BIGINT,
			COALESCE(SUM(cache_read_tokens), 0)::BIGINT,
			COALESCE(SUM(total_cost), 0),
			COALESCE(SUM(actual_cost), 0),
			COALESCE(SUM(COALESCE(duration_ms, 0)), 0)::BIGINT,
			COUNT(duration_ms)::BIGINT,
			(COUNT(*) FILTER (WHERE actual_cost > 0))::BIGINT,
			COALESCE(SUM(
				input_tokens::BIGINT
				+ output_tokens::BIGINT
				+ cache_creation_tokens::BIGINT
				+ cache_read_tokens::BIGINT
			) FILTER (WHERE actual_cost > 0), 0)::BIGINT,
			COALESCE(SUM(actual_cost) FILTER (WHERE actual_cost > 0), 0),
			NOW()
		FROM usage_logs
		WHERE user_id > 0
			AND account_id > 0
			AND created_at >= $1
			AND created_at < $2
		GROUP BY user_id, created_at::DATE, COALESCE(group_id, 0), account_id
		ON CONFLICT (user_id, bucket_date, group_id, account_id)
		DO UPDATE SET
			requests = EXCLUDED.requests,
			input_tokens = EXCLUDED.input_tokens,
			output_tokens = EXCLUDED.output_tokens,
			cache_creation_tokens = EXCLUDED.cache_creation_tokens,
			cache_read_tokens = EXCLUDED.cache_read_tokens,
			total_cost = EXCLUDED.total_cost,
			actual_cost = EXCLUDED.actual_cost,
			duration_sum_ms = EXCLUDED.duration_sum_ms,
			duration_count = EXCLUDED.duration_count,
			billable_requests = EXCLUDED.billable_requests,
			billable_tokens = EXCLUDED.billable_tokens,
			billable_actual_cost = EXCLUDED.billable_actual_cost,
			updated_at = NOW()
	`, start, end)
	return err
}

func (s *userDashboardStatsStore) verifyDay(ctx context.Context, sqlq sqlExecutor, start, end time.Time) error {
	var mismatch bool
	err := scanSingleRow(ctx, sqlq, `
		WITH raw AS (
			SELECT
				user_id,
				created_at::DATE AS bucket_date,
				COALESCE(group_id, 0) AS group_id,
				account_id,
				COUNT(*)::BIGINT AS requests,
				COALESCE(SUM(input_tokens), 0)::BIGINT AS input_tokens,
				COALESCE(SUM(output_tokens), 0)::BIGINT AS output_tokens,
				COALESCE(SUM(cache_creation_tokens), 0)::BIGINT AS cache_creation_tokens,
				COALESCE(SUM(cache_read_tokens), 0)::BIGINT AS cache_read_tokens,
				COALESCE(SUM(total_cost), 0) AS total_cost,
				COALESCE(SUM(actual_cost), 0) AS actual_cost,
				COALESCE(SUM(COALESCE(duration_ms, 0)), 0)::BIGINT AS duration_sum_ms,
				COUNT(duration_ms)::BIGINT AS duration_count,
				(COUNT(*) FILTER (WHERE actual_cost > 0))::BIGINT AS billable_requests,
				COALESCE(SUM(
					input_tokens::BIGINT
					+ output_tokens::BIGINT
					+ cache_creation_tokens::BIGINT
					+ cache_read_tokens::BIGINT
				) FILTER (WHERE actual_cost > 0), 0)::BIGINT AS billable_tokens,
				COALESCE(SUM(actual_cost) FILTER (WHERE actual_cost > 0), 0) AS billable_actual_cost
			FROM usage_logs
			WHERE user_id > 0
				AND account_id > 0
				AND created_at >= $1
				AND created_at < $2
			GROUP BY user_id, created_at::DATE, COALESCE(group_id, 0), account_id
		), daily AS (
			SELECT
				user_id, bucket_date, group_id, account_id,
				requests, input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
				total_cost, actual_cost, duration_sum_ms, duration_count,
				billable_requests, billable_tokens, billable_actual_cost
			FROM user_dashboard_route_daily
			WHERE bucket_date = $1::DATE
		), differences AS (
			(SELECT * FROM raw EXCEPT SELECT * FROM daily)
			UNION ALL
			(SELECT * FROM daily EXCEPT SELECT * FROM raw)
		)
		SELECT EXISTS(SELECT 1 FROM differences)
	`, []any{start, end}, &mismatch)
	if err != nil {
		return err
	}
	if mismatch {
		return fmt.Errorf("user dashboard daily verification mismatch for %s", start.Format("2006-01-02"))
	}
	return nil
}

func (s *userDashboardStatsStore) reconcileRetention(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var locked bool
	if err := scanSingleRow(ctx, tx, `SELECT pg_try_advisory_xact_lock($1)`, []any{userDashboardStatsBackfillLockID}, &locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL statement_timeout = '20s'`); err != nil {
		return err
	}

	var retainedFrom sql.NullString
	if err := scanSingleRow(ctx, tx, `SELECT MIN(created_at)::DATE::TEXT FROM usage_logs`, nil, &retainedFrom); err != nil {
		return err
	}
	if !retainedFrom.Valid || retainedFrom.String == "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_dashboard_route_daily`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE user_dashboard_route_daily_state
			SET coverage_start = NULL, updated_at = NOW()
			WHERE id = 1 AND ready = TRUE
		`); err != nil {
			return err
		}
		return tx.Commit()
	}

	retainedDate, _, err := parseUserDashboardStatsDate(retainedFrom)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_dashboard_route_daily WHERE bucket_date < $1::DATE`, retainedDate); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE user_dashboard_route_daily_state
		SET coverage_start = $1::DATE, updated_at = NOW()
		WHERE id = 1 AND ready = TRUE
	`, retainedDate); err != nil {
		return err
	}
	return tx.Commit()
}
