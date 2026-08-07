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
	"github.com/lib/pq"
)

const (
	apiKeyUsageDailyBackfillLockID int64 = 721839315684021467
	apiKeyUsageDailyDefaultDays          = 30
	apiKeyUsageDailyBackfillPause        = 250 * time.Millisecond
	apiKeyUsageDailyBackfillRetry        = time.Minute
	apiKeyUsageDailyBackfillSettle       = 10 * time.Minute
	apiKeyUsageDailyStatementLimit       = 20 * time.Second
)

type apiKeyUsageDailyStore struct {
	sql sqlExecutor
	db  *sql.DB

	startOnce sync.Once
}

const apiKeyUsageDailyBucketUpsertSQL = `
	INSERT INTO api_key_usage_daily (api_key_id, bucket_date, actual_cost, updated_at)
	SELECT grouped.api_key_id, grouped.bucket_date, grouped.actual_cost, NOW()
	FROM api_key_usage_daily_grouped grouped
	ON CONFLICT (api_key_id, bucket_date)
	DO UPDATE SET
		actual_cost = api_key_usage_daily.actual_cost + EXCLUDED.actual_cost,
		updated_at = NOW()
`

func newAPIKeyUsageDailyStore(sqlq sqlExecutor) *apiKeyUsageDailyStore {
	store := &apiKeyUsageDailyStore{sql: sqlq}
	if db, ok := sqlq.(*sql.DB); ok {
		store.db = db
	}
	return store
}

// StartAutomaticBackfill builds the recent daily buckets before enabling reads.
// Test repositories do not call it, which keeps integration tests deterministic.
func (s *apiKeyUsageDailyStore) StartAutomaticBackfill() {
	if s == nil || s.db == nil {
		return
	}
	s.startOnce.Do(func() {
		go s.runBackfill()
	})
}

func (s *apiKeyUsageDailyStore) runBackfill() {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), apiKeyUsageDailyStatementLimit)
		complete, err := s.backfillStep(ctx)
		cancel()
		if err != nil {
			slog.Error("API key daily usage backfill failed; will retry", "error", err)
			time.Sleep(apiKeyUsageDailyBackfillRetry)
			continue
		}
		if complete {
			return
		}
		time.Sleep(apiKeyUsageDailyBackfillPause)
	}
}

// Add atomically adds a cost to the current application-timezone day.
func (s *apiKeyUsageDailyStore) Add(ctx context.Context, apiKeyID int64, increment float64) error {
	if s == nil || s.sql == nil || apiKeyID <= 0 || increment == 0 {
		return nil
	}
	_, err := s.sql.ExecContext(ctx, `
		INSERT INTO api_key_usage_daily (api_key_id, bucket_date, actual_cost, updated_at)
		VALUES ($1, CURRENT_DATE, $2, NOW())
		ON CONFLICT (api_key_id, bucket_date)
		DO UPDATE SET
			actual_cost = api_key_usage_daily.actual_cost + EXCLUDED.actual_cost,
			updated_at = NOW()
	`, apiKeyID, increment)
	return err
}

// GetBatch returns the existing API-key usage response shape. Until the
// historical buckets are ready, it transparently uses the legacy raw query.
func (s *apiKeyUsageDailyStore) GetBatch(
	ctx context.Context,
	apiKeyIDs []int64,
	startTime, endTime time.Time,
) (map[int64]*usagestats.BatchAPIKeyUsageStats, error) {
	apiKeyIDs = normalizePositiveInt64IDs(apiKeyIDs)
	result := newBatchAPIKeyUsageResult(apiKeyIDs)
	if len(apiKeyIDs) == 0 {
		return result, nil
	}

	startDate, endDate, today := apiKeyUsageDailyRange(startTime, endTime)
	ready, err := s.isReadyFor(ctx, startDate)
	if err != nil {
		return nil, err
	}
	if !ready {
		return s.getLegacyBatch(ctx, apiKeyIDs, startTime, endTime)
	}

	rows, err := s.sql.QueryContext(ctx, `
		SELECT
			api_key_id,
			COALESCE(SUM(actual_cost) FILTER (
				WHERE bucket_date >= $2::date AND bucket_date < $3::date
			), 0) AS total_cost,
			COALESCE(SUM(actual_cost) FILTER (WHERE bucket_date = $4::date), 0) AS today_cost
		FROM api_key_usage_daily
		WHERE api_key_id = ANY($1)
			AND bucket_date >= LEAST($2::date, $4::date)
			AND bucket_date < GREATEST($3::date, $4::date + 1)
		GROUP BY api_key_id
	`, pq.Array(apiKeyIDs), startDate, endDate, today)
	if err != nil {
		return nil, err
	}
	if err := scanBatchAPIKeyUsageRows(rows, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *apiKeyUsageDailyStore) getLegacyBatch(
	ctx context.Context,
	apiKeyIDs []int64,
	startTime, endTime time.Time,
) (map[int64]*usagestats.BatchAPIKeyUsageStats, error) {
	result := newBatchAPIKeyUsageResult(apiKeyIDs)
	if startTime.IsZero() {
		startTime = time.Now().AddDate(0, 0, -30)
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}

	rows, err := s.sql.QueryContext(ctx, `
		SELECT
			api_key_id,
			COALESCE(SUM(actual_cost) FILTER (WHERE created_at >= $2 AND created_at < $3), 0) AS total_cost,
			COALESCE(SUM(actual_cost) FILTER (WHERE created_at >= $4), 0) AS today_cost
		FROM usage_logs
		WHERE api_key_id = ANY($1)
			AND created_at >= LEAST($2, $4)
		GROUP BY api_key_id
	`, pq.Array(apiKeyIDs), startTime, endTime, timezone.Today())
	if err != nil {
		return nil, err
	}
	if err := scanBatchAPIKeyUsageRows(rows, result); err != nil {
		return nil, err
	}
	return result, nil
}

func scanBatchAPIKeyUsageRows(rows *sql.Rows, result map[int64]*usagestats.BatchAPIKeyUsageStats) error {
	for rows.Next() {
		var apiKeyID int64
		var total float64
		var todayTotal float64
		if err := rows.Scan(&apiKeyID, &total, &todayTotal); err != nil {
			_ = rows.Close()
			return err
		}
		if stats, ok := result[apiKeyID]; ok {
			stats.TotalActualCost = total
			stats.TodayActualCost = todayTotal
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	return rows.Close()
}

func newBatchAPIKeyUsageResult(apiKeyIDs []int64) map[int64]*usagestats.BatchAPIKeyUsageStats {
	result := make(map[int64]*usagestats.BatchAPIKeyUsageStats, len(apiKeyIDs))
	for _, apiKeyID := range apiKeyIDs {
		result[apiKeyID] = &usagestats.BatchAPIKeyUsageStats{APIKeyID: apiKeyID}
	}
	return result
}

func apiKeyUsageDailyRange(startTime, endTime time.Time) (startDate, endDate, today time.Time) {
	today = timezone.Today()
	if startTime.IsZero() {
		startDate = today.AddDate(0, 0, -(apiKeyUsageDailyDefaultDays - 1))
	} else {
		startDate = timezone.StartOfDay(startTime)
	}

	if endTime.IsZero() {
		endDate = today.AddDate(0, 0, 1)
	} else {
		endDate = timezone.StartOfDay(endTime)
		if endTime.After(endDate) {
			endDate = endDate.AddDate(0, 0, 1)
		}
	}
	return startDate, endDate, today
}

func (s *apiKeyUsageDailyStore) isReadyFor(ctx context.Context, startDate time.Time) (bool, error) {
	if s == nil || s.sql == nil {
		return false, nil
	}
	var ready bool
	err := scanSingleRow(ctx, s.sql, `
		SELECT ready AND coverage_start IS NOT NULL AND coverage_start <= $1::date
		FROM api_key_usage_daily_state
		WHERE id = 1
	`, []any{startDate}, &ready)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return ready, err
}

// apiKeyUsageDailyAggregationCTEs is shared by every usage-log insert shape.
// The inserted CTE must expose api_key_id, actual_cost, and created_at.
func apiKeyUsageDailyAggregationCTEs() string {
	return `,
		api_key_usage_daily_input AS (
			SELECT api_key_id, created_at::date AS bucket_date, actual_cost
			FROM inserted
			WHERE api_key_id > 0 AND actual_cost <> 0
		), api_key_usage_daily_grouped AS (
			SELECT api_key_id, bucket_date, SUM(actual_cost) AS actual_cost
			FROM api_key_usage_daily_input
			GROUP BY api_key_id, bucket_date
		)`
}

func apiKeyUsageDailyAggregationWriteCTE() string {
	return `,
		api_key_usage_daily_write AS (` + apiKeyUsageDailyBucketUpsertSQL + `
		)`
}

func (s *apiKeyUsageDailyStore) backfillStep(ctx context.Context) (complete bool, err error) {
	if s == nil || s.db == nil {
		return true, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var locked bool
	if err := scanSingleRow(ctx, tx, `SELECT pg_try_advisory_xact_lock($1)`, []any{apiKeyUsageDailyBackfillLockID}, &locked); err != nil {
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
		SELECT ready, coverage_start::text, cursor::text, updated_at
		FROM api_key_usage_daily_state
		WHERE id = 1
		FOR UPDATE
	`, nil, &ready, &coverageStartText, &cursorText, &updatedAt); err != nil {
		return false, err
	}
	if ready {
		return true, tx.Commit()
	}
	coverageStart, hasCoverageStart, err := parseAPIKeyUsageDailyDate(coverageStartText)
	if err != nil {
		return false, err
	}
	cursor, hasCursor, err := parseAPIKeyUsageDailyDate(cursorText)
	if err != nil {
		return false, err
	}

	today := timezone.Today()
	wantedStart := today.AddDate(0, 0, -(apiKeyUsageDailyDefaultDays - 1))
	coverageExpanded := !hasCoverageStart || coverageStart.After(wantedStart)
	if coverageExpanded {
		coverageStart = wantedStart
		cursor = wantedStart
		hasCursor = true
	}
	if !hasCursor || cursor.Before(wantedStart) {
		cursor = wantedStart
		if _, err := tx.ExecContext(ctx, `
			UPDATE api_key_usage_daily_state
			SET coverage_start = $1::date, cursor = $1::date, updated_at = NOW()
			WHERE id = 1
		`, wantedStart); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}

	cursorDay := cursor
	if cursorDay.Before(today) {
		nextDay := cursorDay.AddDate(0, 0, 1)
		if err := s.rebuildDay(ctx, tx, cursorDay, nextDay); err != nil {
			return false, err
		}
		if err := s.verifyDay(ctx, tx, cursorDay, nextDay); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE api_key_usage_daily_state
			SET coverage_start = $1::date, cursor = $2::date, updated_at = NOW()
			WHERE id = 1
		`, coverageStart, nextDay); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}

	if time.Since(updatedAt) < apiKeyUsageDailyBackfillSettle {
		return false, tx.Commit()
	}

	// Block aggregate upserts during the final current-day replacement. New
	// usage-log statements then resume and add their increments after commit.
	if _, err := tx.ExecContext(ctx, `LOCK TABLE api_key_usage_daily IN SHARE ROW EXCLUSIVE MODE`); err != nil {
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
		UPDATE api_key_usage_daily_state
		SET ready = TRUE, coverage_start = $1::date, cursor = $2::date, updated_at = NOW()
		WHERE id = 1
	`, coverageStart, nextDay); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func parseAPIKeyUsageDailyDate(value sql.NullString) (time.Time, bool, error) {
	if !value.Valid || value.String == "" {
		return time.Time{}, false, nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", value.String, timezone.Location())
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse API key usage daily date %q: %w", value.String, err)
	}
	return parsed, true, nil
}

func (s *apiKeyUsageDailyStore) rebuildDay(ctx context.Context, sqlq sqlExecutor, start, end time.Time) error {
	if !end.After(start) {
		return nil
	}
	if _, err := sqlq.ExecContext(ctx, `DELETE FROM api_key_usage_daily WHERE bucket_date = $1::date`, start); err != nil {
		return err
	}
	_, err := sqlq.ExecContext(ctx, `
		INSERT INTO api_key_usage_daily (api_key_id, bucket_date, actual_cost, updated_at)
		SELECT api_key_id, created_at::date, COALESCE(SUM(actual_cost), 0), NOW()
		FROM usage_logs
		WHERE api_key_id > 0
			AND created_at >= $1
			AND created_at < $2
		GROUP BY api_key_id, created_at::date
		ON CONFLICT (api_key_id, bucket_date)
		DO UPDATE SET actual_cost = EXCLUDED.actual_cost, updated_at = NOW()
	`, start, end)
	return err
}

func (s *apiKeyUsageDailyStore) verifyDay(ctx context.Context, sqlq sqlExecutor, start, end time.Time) error {
	var mismatch bool
	err := scanSingleRow(ctx, sqlq, `
		WITH raw AS (
			SELECT api_key_id, COALESCE(SUM(actual_cost), 0) AS actual_cost
			FROM usage_logs
			WHERE api_key_id > 0
				AND created_at >= $1
				AND created_at < $2
			GROUP BY api_key_id
		), daily AS (
			SELECT api_key_id, actual_cost
			FROM api_key_usage_daily
			WHERE bucket_date = $1::date
		)
		SELECT EXISTS (
			SELECT 1
			FROM raw
			FULL JOIN daily USING (api_key_id)
			WHERE COALESCE(raw.actual_cost, 0) <> COALESCE(daily.actual_cost, 0)
		)
	`, []any{start, end}, &mismatch)
	if err != nil {
		return err
	}
	if mismatch {
		return fmt.Errorf("API key daily usage verification mismatch for %s", start.Format("2006-01-02"))
	}
	return nil
}
