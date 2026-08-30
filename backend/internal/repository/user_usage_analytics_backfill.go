package repository

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

const (
	userUsageAnalyticsBackfillLockID int64 = 528276810991742312

	userUsageAnalyticsBackfillPause       = 250 * time.Millisecond
	userUsageAnalyticsBackfillRetry       = time.Minute
	userUsageAnalyticsBackfillSettle      = 2 * time.Minute
	userUsageAnalyticsStatementLimit      = 20 * time.Second
	userUsageAnalyticsMaintenanceInterval = time.Hour
)

func (s *userUsageAnalyticsStore) StartAutomaticBackfill() {
	if s == nil || s.db == nil {
		return
	}
	s.startOnce.Do(func() {
		go s.runBackfillAndMaintenance()
	})
}

func (s *userUsageAnalyticsStore) runBackfillAndMaintenance() {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), userUsageAnalyticsStatementLimit)
		complete, err := s.backfillStep(ctx)
		cancel()
		if err != nil {
			slog.Error("user usage analytics backfill failed; will retry", "error", err)
			time.Sleep(userUsageAnalyticsBackfillRetry)
			continue
		}
		if complete {
			break
		}
		time.Sleep(userUsageAnalyticsBackfillPause)
	}

	ticker := time.NewTicker(userUsageAnalyticsMaintenanceInterval)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), userUsageAnalyticsStatementLimit)
		err := s.reconcileRetention(ctx)
		cancel()
		if err != nil {
			slog.Error("user usage analytics retention reconciliation failed", "error", err)
		}
	}
}

func (s *userUsageAnalyticsStore) backfillStep(ctx context.Context) (complete bool, err error) {
	if s == nil || s.db == nil {
		return true, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var locked bool
	if err := scanSingleRow(ctx, tx, `SELECT pg_try_advisory_xact_lock($1)`, []any{userUsageAnalyticsBackfillLockID}, &locked); err != nil {
		return false, err
	}
	if !locked {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL statement_timeout = '20s'`); err != nil {
		return false, err
	}

	var ready bool
	var coverageStart sql.NullTime
	var cursor sql.NullTime
	var updatedAt time.Time
	if err := scanSingleRow(ctx, tx, `
		SELECT ready, coverage_start, cursor, updated_at
		FROM user_usage_analytics_hourly_state
		WHERE id = 1
		FOR UPDATE
	`, nil, &ready, &coverageStart, &cursor, &updatedAt); err != nil {
		return false, err
	}
	if ready {
		return true, tx.Commit()
	}

	wantedStart, err := userUsageAnalyticsRawStart(ctx, tx)
	if err != nil {
		return false, err
	}
	if !coverageStart.Valid || coverageStart.Time.After(wantedStart) || !cursor.Valid {
		if _, err := tx.ExecContext(ctx, `
			UPDATE user_usage_analytics_hourly_state
			SET ready = FALSE, coverage_start = $1, cursor = $1, updated_at = NOW()
			WHERE id = 1
		`, wantedStart); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	if cursor.Time.Before(wantedStart) {
		if _, err := tx.ExecContext(ctx, `
			UPDATE user_usage_analytics_hourly_state
			SET cursor = $1, updated_at = NOW()
			WHERE id = 1
		`, wantedStart); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}

	coverage := coverageStart.Time
	cursorTime := cursor.Time.UTC()
	currentHour := time.Now().UTC().Truncate(time.Hour)
	if cursorTime.Before(currentHour) {
		segmentEnd := cursorTime.Truncate(24 * time.Hour).Add(24 * time.Hour)
		if !segmentEnd.After(cursorTime) {
			segmentEnd = cursorTime.Add(24 * time.Hour)
		}
		if segmentEnd.After(currentHour) {
			segmentEnd = currentHour
		}
		if err := s.rebuildSegment(ctx, tx, cursorTime, segmentEnd); err != nil {
			return false, err
		}
		if err := s.verifySegment(ctx, tx, cursorTime, segmentEnd); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE user_usage_analytics_hourly_state
			SET coverage_start = $1, cursor = $2, updated_at = NOW()
			WHERE id = 1
		`, coverage, segmentEnd); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}

	if time.Since(updatedAt) < userUsageAnalyticsBackfillSettle {
		return false, tx.Commit()
	}

	// Trigger upserts take ROW EXCLUSIVE locks on the aggregate table. Holding
	// SHARE ROW EXCLUSIVE here makes concurrent raw-log writers wait at their
	// trigger until the current-hour replacement commits; their deltas are then
	// applied after the verified snapshot instead of being lost.
	if _, err := tx.ExecContext(ctx, `LOCK TABLE user_usage_analytics_hourly IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return false, err
	}
	currentHourEnd := currentHour.Add(time.Hour)
	if err := s.rebuildSegment(ctx, tx, currentHour, currentHourEnd); err != nil {
		return false, err
	}
	if err := s.verifySegment(ctx, tx, currentHour, currentHourEnd); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE user_usage_analytics_hourly_state
		SET ready = TRUE, coverage_start = $1, cursor = $2, updated_at = NOW()
		WHERE id = 1
	`, coverage, currentHourEnd); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func userUsageAnalyticsRawStart(ctx context.Context, sqlq sqlExecutor) (time.Time, error) {
	var value sql.NullTime
	if err := scanSingleRow(ctx, sqlq, `SELECT MIN(created_at) FROM usage_logs`, nil, &value); err != nil {
		return time.Time{}, err
	}
	if !value.Valid {
		return time.Now().UTC().Truncate(time.Hour), nil
	}
	return value.Time.UTC().Truncate(time.Hour), nil
}

func (s *userUsageAnalyticsStore) rebuildSegment(ctx context.Context, sqlq sqlExecutor, startTime, endTime time.Time) error {
	if !endTime.After(startTime) {
		return nil
	}
	if _, err := sqlq.ExecContext(ctx, `
		DELETE FROM user_usage_analytics_hourly
		WHERE bucket_start >= $1 AND bucket_start < $2
	`, startTime, endTime); err != nil {
		return err
	}

	_, err := sqlq.ExecContext(ctx, `
		INSERT INTO user_usage_analytics_hourly (
			user_id,
			bucket_start,
			api_key_id,
			group_id,
			requested_model,
			request_type,
			stream,
			openai_ws_mode,
			billing_type,
			billing_mode,
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
			duration_count,
			updated_at
		)
		SELECT
			user_id,
			(DATE_TRUNC('hour', created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') AS bucket_start,
			COALESCE(api_key_id, 0) AS api_key_id,
			COALESCE(group_id, 0) AS group_id,
			COALESCE(NULLIF(TRIM(requested_model), ''), model) AS requested_model,
			CASE WHEN request_type BETWEEN 0 AND 5 THEN request_type ELSE 0 END AS request_type,
			COALESCE(stream, FALSE) AS stream,
			COALESCE(openai_ws_mode, FALSE) AS openai_ws_mode,
			COALESCE(billing_type, 0)::SMALLINT AS billing_type,
			CASE
				WHEN billing_mode IS NULL OR billing_mode = '' THEN
					CASE WHEN COALESCE(image_count, 0) > 0 THEN 'image' ELSE 'token' END
				ELSE billing_mode
			END AS billing_mode,
			COALESCE(NULLIF(TRIM(inbound_endpoint), ''), 'unknown') AS inbound_endpoint,
			COUNT(*)::BIGINT,
			COALESCE(SUM(input_tokens), 0)::BIGINT,
			COALESCE(SUM(output_tokens), 0)::BIGINT,
			COALESCE(SUM(cache_creation_tokens), 0)::BIGINT,
			COALESCE(SUM(cache_read_tokens), 0)::BIGINT,
			COALESCE(SUM(total_cost), 0),
			COALESCE(SUM(actual_cost), 0),
			COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0),
			COALESCE(SUM(COALESCE(duration_ms, 0)), 0)::BIGINT,
			COUNT(duration_ms)::BIGINT,
			NOW()
		FROM usage_logs
		WHERE user_id > 0
			AND created_at >= $1
			AND created_at < $2
		GROUP BY
			user_id,
			(DATE_TRUNC('hour', created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'),
			COALESCE(api_key_id, 0),
			COALESCE(group_id, 0),
			COALESCE(NULLIF(TRIM(requested_model), ''), model),
			CASE WHEN request_type BETWEEN 0 AND 5 THEN request_type ELSE 0 END,
			COALESCE(stream, FALSE),
			COALESCE(openai_ws_mode, FALSE),
			COALESCE(billing_type, 0)::SMALLINT,
			CASE
				WHEN billing_mode IS NULL OR billing_mode = '' THEN
					CASE WHEN COALESCE(image_count, 0) > 0 THEN 'image' ELSE 'token' END
				ELSE billing_mode
			END,
			COALESCE(NULLIF(TRIM(inbound_endpoint), ''), 'unknown')
		ON CONFLICT (
			user_id,
			bucket_start,
			api_key_id,
			group_id,
			requested_model,
			request_type,
			stream,
			openai_ws_mode,
			billing_type,
			billing_mode,
			inbound_endpoint
		)
		DO UPDATE SET
			requests = EXCLUDED.requests,
			input_tokens = EXCLUDED.input_tokens,
			output_tokens = EXCLUDED.output_tokens,
			cache_creation_tokens = EXCLUDED.cache_creation_tokens,
			cache_read_tokens = EXCLUDED.cache_read_tokens,
			total_cost = EXCLUDED.total_cost,
			actual_cost = EXCLUDED.actual_cost,
			account_cost = EXCLUDED.account_cost,
			duration_sum_ms = EXCLUDED.duration_sum_ms,
			duration_count = EXCLUDED.duration_count,
			updated_at = NOW()
	`, startTime, endTime)
	return err
}

func (s *userUsageAnalyticsStore) verifySegment(ctx context.Context, sqlq sqlExecutor, startTime, endTime time.Time) error {
	var mismatch bool
	err := scanSingleRow(ctx, sqlq, `
		WITH raw AS (
			SELECT
				user_id,
				(DATE_TRUNC('hour', created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') AS bucket_start,
				COALESCE(api_key_id, 0) AS api_key_id,
				COALESCE(group_id, 0) AS group_id,
				COALESCE(NULLIF(TRIM(requested_model), ''), model) AS requested_model,
				CASE WHEN request_type BETWEEN 0 AND 5 THEN request_type ELSE 0 END AS request_type,
				COALESCE(stream, FALSE) AS stream,
				COALESCE(openai_ws_mode, FALSE) AS openai_ws_mode,
				COALESCE(billing_type, 0)::SMALLINT AS billing_type,
				CASE
					WHEN billing_mode IS NULL OR billing_mode = '' THEN
						CASE WHEN COALESCE(image_count, 0) > 0 THEN 'image' ELSE 'token' END
					ELSE billing_mode
				END AS billing_mode,
				COALESCE(NULLIF(TRIM(inbound_endpoint), ''), 'unknown') AS inbound_endpoint,
				COUNT(*)::BIGINT AS requests,
				COALESCE(SUM(input_tokens), 0)::BIGINT AS input_tokens,
				COALESCE(SUM(output_tokens), 0)::BIGINT AS output_tokens,
				COALESCE(SUM(cache_creation_tokens), 0)::BIGINT AS cache_creation_tokens,
				COALESCE(SUM(cache_read_tokens), 0)::BIGINT AS cache_read_tokens,
				COALESCE(SUM(total_cost), 0) AS total_cost,
				COALESCE(SUM(actual_cost), 0) AS actual_cost,
				COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) AS account_cost,
				COALESCE(SUM(COALESCE(duration_ms, 0)), 0)::BIGINT AS duration_sum_ms,
				COUNT(duration_ms)::BIGINT AS duration_count
			FROM usage_logs
			WHERE user_id > 0
				AND created_at >= $1
				AND created_at < $2
			GROUP BY
				user_id,
				(DATE_TRUNC('hour', created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'),
				COALESCE(api_key_id, 0),
				COALESCE(group_id, 0),
				COALESCE(NULLIF(TRIM(requested_model), ''), model),
				CASE WHEN request_type BETWEEN 0 AND 5 THEN request_type ELSE 0 END,
				COALESCE(stream, FALSE),
				COALESCE(openai_ws_mode, FALSE),
				COALESCE(billing_type, 0)::SMALLINT,
				CASE
					WHEN billing_mode IS NULL OR billing_mode = '' THEN
						CASE WHEN COALESCE(image_count, 0) > 0 THEN 'image' ELSE 'token' END
					ELSE billing_mode
				END,
				COALESCE(NULLIF(TRIM(inbound_endpoint), ''), 'unknown')
		), aggregate_rows AS (
			SELECT
				user_id,
				bucket_start,
				api_key_id,
				group_id,
				requested_model,
				request_type,
				stream,
				openai_ws_mode,
				billing_type,
				billing_mode,
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
			WHERE bucket_start >= $1 AND bucket_start < $2
		), differences AS (
			(SELECT * FROM raw EXCEPT SELECT * FROM aggregate_rows)
			UNION ALL
			(SELECT * FROM aggregate_rows EXCEPT SELECT * FROM raw)
		)
		SELECT EXISTS(SELECT 1 FROM differences)
	`, []any{startTime, endTime}, &mismatch)
	if err != nil {
		return err
	}
	if mismatch {
		return fmt.Errorf("user usage analytics verification mismatch for %s to %s", startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
	}
	return nil
}

func (s *userUsageAnalyticsStore) reconcileRetention(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var locked bool
	if err := scanSingleRow(ctx, tx, `SELECT pg_try_advisory_xact_lock($1)`, []any{userUsageAnalyticsBackfillLockID}, &locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL statement_timeout = '20s'`); err != nil {
		return err
	}

	var retainedFrom sql.NullTime
	if err := scanSingleRow(ctx, tx, `SELECT MIN(created_at) FROM usage_logs`, nil, &retainedFrom); err != nil {
		return err
	}
	if !retainedFrom.Valid {
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_usage_analytics_hourly`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE user_usage_analytics_hourly_state
			SET coverage_start = NULL, updated_at = NOW()
			WHERE id = 1 AND ready = TRUE
		`); err != nil {
			return err
		}
		return tx.Commit()
	}

	retainedHour := retainedFrom.Time.UTC().Truncate(time.Hour)
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM user_usage_analytics_hourly
		WHERE bucket_start < $1
	`, retainedHour); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE user_usage_analytics_hourly_state
		SET coverage_start = $1, updated_at = NOW()
		WHERE id = 1 AND ready = TRUE
	`, retainedHour); err != nil {
		return err
	}
	return tx.Commit()
}
