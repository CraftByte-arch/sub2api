package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

const (
	groupUsageBackfillLockID         int64 = 729414082979628431
	groupUsageBackfillChunk                = 6 * time.Hour
	groupUsageBackfillPause                = 250 * time.Millisecond
	groupUsageBackfillRetry                = time.Minute
	groupUsageBackfillSettleDelay          = 2 * time.Minute
	groupUsageBackfillStatementLimit       = 20 * time.Second
)

type groupUsageAggregation struct {
	sql sqlExecutor
	db  *sql.DB

	startOnce sync.Once
}

type groupUsageIncrement struct {
	groupID     int64
	actualCost  float64
	bucketStart time.Time
}

const groupUsageBucketUpsertSQL = `
	INSERT INTO group_usage_hourly (group_id, bucket_start, actual_cost, updated_at)
	SELECT grouped.group_id, grouped.bucket_start, grouped.actual_cost, NOW()
	FROM group_usage_grouped grouped
	ON CONFLICT (group_id, bucket_start)
	DO UPDATE SET
		actual_cost = group_usage_hourly.actual_cost + EXCLUDED.actual_cost,
		updated_at = NOW()
`

func newGroupUsageAggregation(sqlq sqlExecutor) *groupUsageAggregation {
	a := &groupUsageAggregation{sql: sqlq}
	if db, ok := sqlq.(*sql.DB); ok {
		a.db = db
	}
	return a
}

// StartAutomaticBackfill starts the one-time, resumable historical aggregation.
// It is only invoked by the production repository constructor; test repositories
// use newGroupUsageAggregation directly and remain deterministic.
func (a *groupUsageAggregation) StartAutomaticBackfill() {
	if a == nil || a.db == nil {
		return
	}
	a.startOnce.Do(func() {
		go a.runBackfill()
	})
}

func (a *groupUsageAggregation) runBackfill() {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), groupUsageBackfillStatementLimit)
		complete, err := a.backfillStep(ctx)
		cancel()
		if err != nil {
			slog.Error("group usage hourly backfill failed; will retry", "error", err)
			time.Sleep(groupUsageBackfillRetry)
			continue
		}
		if complete {
			return
		}
		time.Sleep(groupUsageBackfillPause)
	}
}

// Add records a single current-hour increment. Callers that already own a
// transaction use addInsertedBatch with the transaction-bound aggregator.
func (a *groupUsageAggregation) Add(ctx context.Context, groupID int64, actualCost float64) error {
	return a.addInsertedBatch(ctx, []groupUsageIncrement{{
		groupID:     groupID,
		actualCost:  actualCost,
		bucketStart: utcHour(time.Now()),
	}})
}

func (a *groupUsageAggregation) addInsertedBatch(ctx context.Context, increments []groupUsageIncrement) error {
	if a == nil || a.sql == nil || len(increments) == 0 {
		return nil
	}

	values := make([]string, 0, len(increments))
	args := make([]any, 0, len(increments)*3)
	for _, increment := range increments {
		if increment.groupID <= 0 || increment.actualCost == 0 {
			continue
		}
		values = append(values, fmt.Sprintf("($%d, $%d, $%d)", len(args)+1, len(args)+2, len(args)+3))
		args = append(args, increment.groupID, utcHour(increment.bucketStart), increment.actualCost)
	}
	if len(values) == 0 {
		return nil
	}

	query := `
		WITH input (group_id, bucket_start, actual_cost) AS (
			VALUES ` + strings.Join(values, ",") + `
		), grouped AS (
			SELECT group_id, bucket_start, SUM(actual_cost) AS actual_cost
			FROM input
			GROUP BY group_id, bucket_start
		)
		INSERT INTO group_usage_hourly (group_id, bucket_start, actual_cost, updated_at)
		SELECT grouped.group_id, grouped.bucket_start, grouped.actual_cost, NOW()
		FROM grouped
		ON CONFLICT (group_id, bucket_start)
		DO UPDATE SET
			actual_cost = group_usage_hourly.actual_cost + EXCLUDED.actual_cost,
			updated_at = NOW()
	`
	_, err := a.sql.ExecContext(ctx, query, args...)
	return err
}

// groupUsageAggregationCTEs appends the common CTEs used by every usage-log
// insert shape. `inserted` must expose group_id, actual_cost, and created_at.
// Buckets are written before readiness so range rebuilds have no handoff gap.
func groupUsageAggregationCTEs() string {
	return `,
		group_usage_input AS (
			SELECT
				group_id,
				date_trunc('hour', created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS bucket_start,
				actual_cost
			FROM inserted
			WHERE group_id IS NOT NULL AND actual_cost <> 0
		), group_usage_grouped AS (
			SELECT group_id, bucket_start, SUM(actual_cost) AS actual_cost
			FROM group_usage_input
			GROUP BY group_id, bucket_start
		)`
}

func groupUsageAggregationWriteCTE() string {
	return `,
		group_usage_write AS (` + groupUsageBucketUpsertSQL + `
		)`
}

func (a *groupUsageAggregation) GetAllGroupUsageSummary(ctx context.Context, todayStart time.Time) ([]usagestats.GroupUsageSummary, error) {
	ready, err := a.isReady(ctx)
	if err != nil {
		return nil, err
	}
	if !ready {
		return a.getLegacySummary(ctx, todayStart)
	}
	return a.getHourlySummary(ctx, todayStart)
}

func (a *groupUsageAggregation) isReady(ctx context.Context) (bool, error) {
	var ready bool
	err := scanSingleRow(ctx, a.sql, `SELECT ready FROM group_usage_aggregation_state WHERE id = 1`, nil, &ready)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return ready, err
}

func (a *groupUsageAggregation) getLegacySummary(ctx context.Context, todayStart time.Time) ([]usagestats.GroupUsageSummary, error) {
	query := `
		SELECT
			g.id AS group_id,
			COALESCE(SUM(ul.actual_cost), 0) AS total_cost,
			COALESCE(SUM(CASE WHEN ul.created_at >= $1 THEN ul.actual_cost ELSE 0 END), 0) AS today_cost
		FROM groups g
		LEFT JOIN usage_logs ul ON ul.group_id = g.id
		WHERE g.deleted_at IS NULL
		GROUP BY g.id
		ORDER BY g.id
	`
	return scanGroupUsageSummary(ctx, a.sql, query, []any{todayStart})
}

func (a *groupUsageAggregation) getHourlySummary(ctx context.Context, todayStart time.Time) ([]usagestats.GroupUsageSummary, error) {
	todayStart = todayStart.UTC()
	firstFullHour := utcHour(todayStart)
	if firstFullHour.Before(todayStart) {
		firstFullHour = firstFullHour.Add(time.Hour)
	}
	query := `
		WITH hourly AS (
			SELECT
				group_id,
				COALESCE(SUM(actual_cost), 0) AS total_cost,
				COALESCE(SUM(actual_cost) FILTER (WHERE bucket_start >= $2), 0) AS today_cost
			FROM group_usage_hourly
			GROUP BY group_id
		), initial_partial_hour AS (
			SELECT
				group_id,
				COALESCE(SUM(actual_cost), 0) AS actual_cost
			FROM usage_logs
			WHERE group_id IS NOT NULL
				AND created_at >= $1
				AND created_at < $2
			GROUP BY group_id
		)
		SELECT
			g.id AS group_id,
			COALESCE(hourly.total_cost, 0) AS total_cost,
			COALESCE(hourly.today_cost, 0) + COALESCE(initial_partial_hour.actual_cost, 0) AS today_cost
		FROM groups g
		LEFT JOIN hourly ON hourly.group_id = g.id
		LEFT JOIN initial_partial_hour ON initial_partial_hour.group_id = g.id
		WHERE g.deleted_at IS NULL
		ORDER BY g.id
	`
	return scanGroupUsageSummary(ctx, a.sql, query, []any{todayStart, firstFullHour})
}

func scanGroupUsageSummary(ctx context.Context, sqlq sqlExecutor, query string, args []any) (results []usagestats.GroupUsageSummary, err error) {
	rows, err := sqlq.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			results = nil
		}
	}()

	results = make([]usagestats.GroupUsageSummary, 0)
	for rows.Next() {
		var row usagestats.GroupUsageSummary
		if err := rows.Scan(&row.GroupID, &row.TotalCost, &row.TodayCost); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (a *groupUsageAggregation) CleanupBefore(ctx context.Context, cutoff time.Time) error {
	if a == nil || a.sql == nil {
		return nil
	}
	cutoff = cutoff.UTC()
	boundaryHour := utcHour(cutoff)
	if _, err := a.sql.ExecContext(ctx, `DELETE FROM group_usage_hourly WHERE bucket_start < $1`, boundaryHour); err != nil {
		return err
	}
	if cutoff.Equal(boundaryHour) {
		return nil
	}
	// The raw-log cleanup may retain a portion of the cutoff hour. Rebuild that
	// bucket after deletion so the summary retains exactly the same history.
	return a.rebuildRange(ctx, a.sql, boundaryHour, boundaryHour.Add(time.Hour))
}

func (a *groupUsageAggregation) backfillStep(ctx context.Context) (complete bool, err error) {
	if a == nil || a.db == nil {
		return true, nil
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var locked bool
	if err := scanSingleRow(ctx, tx, `SELECT pg_try_advisory_xact_lock($1)`, []any{groupUsageBackfillLockID}, &locked); err != nil {
		return false, err
	}
	if !locked {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL statement_timeout = '20s'`); err != nil {
		return false, err
	}

	var ready bool
	var cursor sql.NullTime
	if err := scanSingleRow(ctx, tx, `SELECT ready, cursor FROM group_usage_aggregation_state WHERE id = 1`, nil, &ready, &cursor); err != nil {
		return false, err
	}
	if ready {
		return true, tx.Commit()
	}

	safeEnd := utcHour(time.Now().Add(-groupUsageBackfillSettleDelay))
	if !cursor.Valid {
		start, err := firstGroupedUsageHour(ctx, tx)
		if err != nil {
			return false, err
		}
		if start.IsZero() {
			cursor.Time = safeEnd
		} else {
			cursor.Time = start
		}
		cursor.Valid = true
	}
	cursorTime := utcHour(cursor.Time)
	if cursorTime.Before(safeEnd) {
		end := cursorTime.Add(groupUsageBackfillChunk)
		if end.After(safeEnd) {
			end = safeEnd
		}
		if err := a.rebuildRange(ctx, tx, cursorTime, end); err != nil {
			return false, err
		}
		if err := a.verifyRange(ctx, tx, cursorTime, end); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE group_usage_aggregation_state SET cursor = $1, updated_at = NOW() WHERE id = 1`, end); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}

	// A live insert updates its raw row and bucket in one statement. This final
	// replacement therefore sees the row or leaves its later increment intact.
	finalEnd := time.Now().UTC()
	if err := a.rebuildRange(ctx, tx, cursorTime, finalEnd); err != nil {
		return false, err
	}
	if err := a.verifyRange(ctx, tx, cursorTime, finalEnd); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE group_usage_aggregation_state SET ready = TRUE, cursor = $1, updated_at = NOW() WHERE id = 1`, finalEnd); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func firstGroupedUsageHour(ctx context.Context, sqlq sqlExecutor) (time.Time, error) {
	var start time.Time
	err := scanSingleRow(ctx, sqlq, `
		SELECT date_trunc('hour', created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
		FROM usage_logs
		WHERE group_id IS NOT NULL
		ORDER BY created_at
		LIMIT 1
	`, nil, &start)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return utcHour(start), nil
}

func (a *groupUsageAggregation) rebuildRange(ctx context.Context, sqlq sqlExecutor, start, end time.Time) error {
	if !end.After(start) {
		return nil
	}
	if _, err := sqlq.ExecContext(ctx, `DELETE FROM group_usage_hourly WHERE bucket_start >= $1 AND bucket_start < $2`, start, end); err != nil {
		return err
	}
	_, err := sqlq.ExecContext(ctx, `
		INSERT INTO group_usage_hourly (group_id, bucket_start, actual_cost, updated_at)
		SELECT
			group_id,
			date_trunc('hour', created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS bucket_start,
			COALESCE(SUM(actual_cost), 0) AS actual_cost,
			NOW()
		FROM usage_logs
		WHERE group_id IS NOT NULL
			AND created_at >= $1
			AND created_at < $2
		GROUP BY group_id, bucket_start
		ON CONFLICT (group_id, bucket_start)
		DO UPDATE SET actual_cost = EXCLUDED.actual_cost, updated_at = NOW()
	`, start, end)
	return err
}

func (a *groupUsageAggregation) verifyRange(ctx context.Context, sqlq sqlExecutor, start, end time.Time) error {
	if !end.After(start) {
		return nil
	}
	var mismatch bool
	err := scanSingleRow(ctx, sqlq, `
		WITH raw AS (
			SELECT group_id, COALESCE(SUM(actual_cost), 0) AS actual_cost
			FROM usage_logs
			WHERE group_id IS NOT NULL
				AND created_at >= $1
				AND created_at < $2
			GROUP BY group_id
		), hourly AS (
			SELECT group_id, COALESCE(SUM(actual_cost), 0) AS actual_cost
			FROM group_usage_hourly
			WHERE bucket_start >= $1
				AND bucket_start < $2
			GROUP BY group_id
		)
		SELECT EXISTS (
			SELECT 1
			FROM groups g
			LEFT JOIN raw ON raw.group_id = g.id
			LEFT JOIN hourly ON hourly.group_id = g.id
			WHERE g.deleted_at IS NULL
				AND COALESCE(raw.actual_cost, 0) <> COALESCE(hourly.actual_cost, 0)
		)
	`, []any{start, end}, &mismatch)
	if err != nil {
		return err
	}
	if mismatch {
		return fmt.Errorf("group usage hourly verification mismatch for %s to %s", start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	}
	return nil
}

func utcHour(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC)
}
