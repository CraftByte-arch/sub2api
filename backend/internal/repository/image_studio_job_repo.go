package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type imageStudioJobRepository struct {
	db *sql.DB
}

const imageStudioJobColumns = `
	id, user_id, api_key_id, mode, status, request_payload, settlement_payload, prompt, model, size, output_format,
	estimated_cost_usd, charged_amount_usd, billing_priority, attempt_count, max_attempts,
	next_attempt_at, hold_balance_amount_usd, hold_usage_card_amount_usd, hold_usage_card_id,
	original_path, thumbnail_path, mime_type, file_size_bytes, width, height, error_code,
	error_message, queued_at, started_at, heartbeat_at, completed_at, expires_at,
	assets_deleted_at, created_at, updated_at
`

func qualifiedImageStudioJobColumns(alias string) string {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return imageStudioJobColumns
	}
	parts := strings.Split(imageStudioJobColumns, ",")
	for i, part := range parts {
		parts[i] = alias + "." + strings.TrimSpace(part)
	}
	return strings.Join(parts, ", ")
}

func NewImageStudioJobRepository(_ *dbent.Client, sqlDB *sql.DB) service.ImageStudioJobRepository {
	return &imageStudioJobRepository{db: sqlDB}
}

func (r *imageStudioJobRepository) Create(ctx context.Context, input service.ImageStudioJobCreateInput) (*service.ImageStudioJob, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("image studio job repository db is nil")
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO image_studio_jobs (
			user_id, api_key_id, mode, status, request_payload, prompt, model, size, output_format,
			estimated_cost_usd, billing_priority, attempt_count, max_attempts, queued_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 0, 3, NOW(), NOW(), NOW())
		RETURNING `+imageStudioJobColumns, input.UserID, input.APIKeyID, input.Mode, service.ImageStudioJobStatusQueued, []byte(input.RequestPayload), input.Prompt, input.Model, input.Size, input.OutputFormat, input.EstimatedCostUSD, input.BillingPriority)
	return scanImageStudioJob(row)
}

func (r *imageStudioJobRepository) GetByID(ctx context.Context, id int64) (*service.ImageStudioJob, error) {
	return r.getOne(ctx, `SELECT `+imageStudioJobColumns+` FROM image_studio_jobs WHERE id = $1`, id)
}

func (r *imageStudioJobRepository) GetByIDForUser(ctx context.Context, id, userID int64) (*service.ImageStudioJob, error) {
	return r.getOne(ctx, `SELECT `+imageStudioJobColumns+` FROM image_studio_jobs WHERE id = $1 AND user_id = $2`, id, userID)
}

func (r *imageStudioJobRepository) ListByUser(ctx context.Context, userID int64, page, pageSize int) (*service.ImageStudioJobList, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("image studio job repository db is nil")
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM image_studio_jobs WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, err
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT `+imageStudioJobColumns+`
		FROM image_studio_jobs
		WHERE user_id = $1
		ORDER BY id DESC
		LIMIT $2 OFFSET $3
	`, userID, pageSize, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]service.ImageStudioJob, 0, pageSize)
	for rows.Next() {
		job, scanErr := scanImageStudioJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &service.ImageStudioJobList{Items: items, Total: total}, nil
}

func (r *imageStudioJobRepository) DeleteByIDForUser(ctx context.Context, id, userID int64) error {
	if r == nil || r.db == nil {
		return errors.New("image studio job repository db is nil")
	}
	result, err := r.db.ExecContext(ctx, `DELETE FROM image_studio_jobs WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return service.ErrImageStudioJobNotFound
	}
	return nil
}

func (r *imageStudioJobRepository) CountStatusByUser(ctx context.Context, userID int64) (*service.ImageStudioJobStats, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("image studio job repository db is nil")
	}
	var stats service.ImageStudioJobStats
	err := r.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status IN ($2, $3, $4)) AS pending_count,
			COUNT(*) FILTER (WHERE status = $5) AS failed_count
		FROM image_studio_jobs
		WHERE user_id = $1
	`, userID, service.ImageStudioJobStatusQueued, service.ImageStudioJobStatusRunning, service.ImageStudioJobStatusSettling, service.ImageStudioJobStatusFailed).
		Scan(&stats.PendingCount, &stats.FailedCount)
	if err != nil {
		return nil, err
	}
	return &stats, nil
}

func (r *imageStudioJobRepository) ListRunnableJobs(ctx context.Context, limit int) ([]service.ImageStudioJob, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("image studio job repository db is nil")
	}
	if limit <= 0 {
		limit = 1
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH first_per_user AS (
			SELECT DISTINCT ON (user_id) id
			FROM image_studio_jobs
			WHERE (
				status = $1
				AND (next_attempt_at IS NULL OR next_attempt_at <= NOW())
				) OR (
					status = $2
					AND (next_attempt_at IS NULL OR next_attempt_at <= NOW())
					AND (heartbeat_at IS NULL OR heartbeat_at <= NOW() - INTERVAL '5 minutes')
				) OR (
					status = $3
					AND (heartbeat_at IS NULL OR heartbeat_at <= NOW() - INTERVAL '5 minutes')
				)
			ORDER BY user_id, COALESCE(next_attempt_at, queued_at) ASC, id ASC
		)
		SELECT `+qualifiedImageStudioJobColumns("j")+`
		FROM image_studio_jobs j
		INNER JOIN first_per_user f ON f.id = j.id
		ORDER BY j.queued_at ASC, j.id ASC
			LIMIT $4
		`, service.ImageStudioJobStatusQueued, service.ImageStudioJobStatusSettling, service.ImageStudioJobStatusRunning, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]service.ImageStudioJob, 0, limit)
	for rows.Next() {
		job, scanErr := scanImageStudioJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *job)
	}
	return items, rows.Err()
}

func (r *imageStudioJobRepository) MarkRunning(ctx context.Context, id int64, startedAt time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE image_studio_jobs
		SET status = $2, started_at = $3, heartbeat_at = $3, updated_at = NOW()
		WHERE id = $1 AND status = $4
	`, id, service.ImageStudioJobStatusRunning, startedAt, service.ImageStudioJobStatusQueued)
	if err != nil {
		return false, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rowsAffected > 0, nil
}

func (r *imageStudioJobRepository) MarkStaleRunningFailed(ctx context.Context, id int64, completedAt, staleBefore time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE image_studio_jobs
		SET status = $2, completed_at = $3, error_code = 'worker_interrupted',
			error_message = 'image generation worker was interrupted', next_attempt_at = NULL,
			heartbeat_at = NULL, updated_at = NOW()
		WHERE id = $1
			AND status = $4
			AND (heartbeat_at IS NULL OR heartbeat_at <= $5)
	`, id, service.ImageStudioJobStatusFailed, completedAt, service.ImageStudioJobStatusRunning, staleBefore)
	if err != nil {
		return false, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rowsAffected > 0, nil
}

func (r *imageStudioJobRepository) MarkSettling(ctx context.Context, id int64, settlementPayload json.RawMessage, originalPath, thumbnailPath, mimeType string, fileSizeBytes int64, width, height int, leaseAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE image_studio_jobs
		SET status = $2, settlement_payload = $4, original_path = $5, thumbnail_path = $6, mime_type = $7,
			file_size_bytes = $8, width = $9, height = $10, heartbeat_at = $11,
			next_attempt_at = NULL, error_code = '', error_message = '', updated_at = NOW()
		WHERE id = $1 AND status = $3
	`, id, service.ImageStudioJobStatusSettling, service.ImageStudioJobStatusRunning, []byte(settlementPayload), originalPath, thumbnailPath, mimeType, fileSizeBytes, width, height, leaseAt)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return service.ErrImageStudioJobInvalid
	}
	return nil
}

func (r *imageStudioJobRepository) ClaimSettling(ctx context.Context, id int64, leaseAt, staleBefore time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE image_studio_jobs
		SET heartbeat_at = $2, updated_at = NOW()
		WHERE id = $1
			AND (heartbeat_at IS NULL OR heartbeat_at <= $3)
			AND status = $4
			AND (next_attempt_at IS NULL OR next_attempt_at <= NOW())
	`, id, leaseAt, staleBefore, service.ImageStudioJobStatusSettling)
	if err != nil {
		return false, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rowsAffected > 0, nil
}

func (r *imageStudioJobRepository) MarkRetryable(ctx context.Context, id int64, nextAttemptAt time.Time, errorCode, errorMessage string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE image_studio_jobs
		SET status = $2, error_code = $3, error_message = $4, next_attempt_at = $5, updated_at = NOW(),
			attempt_count = attempt_count + 1
		WHERE id = $1
	`, id, service.ImageStudioJobStatusQueued, errorCode, errorMessage, nullableTime(&nextAttemptAt))
	return err
}

func (r *imageStudioJobRepository) MarkSettlementRetryable(ctx context.Context, id int64, nextAttemptAt time.Time, errorCode, errorMessage string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE image_studio_jobs
		SET status = $2, error_code = $3, error_message = $4, next_attempt_at = $5,
			heartbeat_at = NULL, updated_at = NOW(), attempt_count = attempt_count + 1
		WHERE id = $1 AND status = $6
	`, id, service.ImageStudioJobStatusSettling, errorCode, errorMessage, nullableTime(&nextAttemptAt), service.ImageStudioJobStatusSettling)
	return err
}

func (r *imageStudioJobRepository) UpdateHeartbeat(ctx context.Context, id int64, heartbeatAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE image_studio_jobs SET heartbeat_at = $2, updated_at = NOW() WHERE id = $1 AND status = $3`, id, heartbeatAt, service.ImageStudioJobStatusRunning)
	return err
}

func (r *imageStudioJobRepository) MarkSucceeded(ctx context.Context, id int64, completedAt time.Time, chargedAmountUSD float64, originalPath, thumbnailPath, mimeType string, fileSizeBytes int64, width, height int, expiresAt *time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE image_studio_jobs
		SET status = $2, completed_at = $3, charged_amount_usd = $4, original_path = $5, thumbnail_path = $6,
			mime_type = $7, file_size_bytes = $8, width = $9, height = $10, expires_at = $11, next_attempt_at = NULL,
			heartbeat_at = NULL, updated_at = NOW()
		WHERE id = $1 AND status = $12
	`, id, service.ImageStudioJobStatusSucceeded, completedAt, chargedAmountUSD, originalPath, thumbnailPath, mimeType, fileSizeBytes, width, height, nullableTime(expiresAt), service.ImageStudioJobStatusSettling)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return service.ErrImageStudioJobInvalid
	}
	return nil
}

func (r *imageStudioJobRepository) MarkFailed(ctx context.Context, id int64, completedAt time.Time, errorCode, errorMessage string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE image_studio_jobs SET status = $2, completed_at = $3, error_code = $4, error_message = $5, next_attempt_at = NULL, updated_at = NOW() WHERE id = $1`, id, service.ImageStudioJobStatusFailed, completedAt, errorCode, errorMessage)
	return err
}

func (r *imageStudioJobRepository) ListExpiredAssets(ctx context.Context, now time.Time, limit int) ([]service.ImageStudioJob, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("image studio job repository db is nil")
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+imageStudioJobColumns+`
		FROM image_studio_jobs
		WHERE status = $1
			AND assets_deleted_at IS NULL
			AND expires_at IS NOT NULL
			AND expires_at <= $2
		ORDER BY expires_at ASC, id ASC
		LIMIT $3
	`, service.ImageStudioJobStatusSucceeded, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]service.ImageStudioJob, 0, limit)
	for rows.Next() {
		job, scanErr := scanImageStudioJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *job)
	}
	return items, rows.Err()
}

func (r *imageStudioJobRepository) MarkAssetsDeleted(ctx context.Context, id int64, deletedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE image_studio_jobs SET assets_deleted_at = $2, updated_at = NOW() WHERE id = $1`, id, deletedAt)
	return err
}

func (r *imageStudioJobRepository) getOne(ctx context.Context, query string, args ...any) (*service.ImageStudioJob, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("image studio job repository db is nil")
	}
	row := r.db.QueryRowContext(ctx, query, args...)
	job, err := scanImageStudioJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrImageStudioJobNotFound
	}
	return job, err
}

type imageStudioJobScanner interface {
	Scan(dest ...any) error
}

func scanImageStudioJob(scanner imageStudioJobScanner) (*service.ImageStudioJob, error) {
	var (
		job             service.ImageStudioJob
		payload         []byte
		settlement      []byte
		holdUsageCardID sql.NullInt64
		startedAt       sql.NullTime
		heartbeatAt     sql.NullTime
		completedAt     sql.NullTime
		expiresAt       sql.NullTime
		assetsDeletedAt sql.NullTime
	)
	err := scanner.Scan(
		&job.ID,
		&job.UserID,
		&job.APIKeyID,
		&job.Mode,
		&job.Status,
		&payload,
		&settlement,
		&job.Prompt,
		&job.Model,
		&job.Size,
		&job.OutputFormat,
		&job.EstimatedCostUSD,
		&job.ChargedAmountUSD,
		&job.BillingPriority,
		&job.AttemptCount,
		&job.MaxAttempts,
		&job.NextAttemptAt,
		&job.HoldBalanceAmountUSD,
		&job.HoldUsageCardAmountUSD,
		&holdUsageCardID,
		&job.OriginalPath,
		&job.ThumbnailPath,
		&job.MIMEType,
		&job.FileSizeBytes,
		&job.Width,
		&job.Height,
		&job.ErrorCode,
		&job.ErrorMessage,
		&job.QueuedAt,
		&startedAt,
		&heartbeatAt,
		&completedAt,
		&expiresAt,
		&assetsDeletedAt,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	job.RequestPayload = json.RawMessage(payload)
	job.SettlementPayload = json.RawMessage(settlement)
	if holdUsageCardID.Valid {
		job.HoldUsageCardID = &holdUsageCardID.Int64
	}
	if job.AttemptCount < 0 {
		job.AttemptCount = 0
	}
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = 3
	}
	if startedAt.Valid {
		value := startedAt.Time
		job.StartedAt = &value
	}
	if heartbeatAt.Valid {
		value := heartbeatAt.Time
		job.HeartbeatAt = &value
	}
	if completedAt.Valid {
		value := completedAt.Time
		job.CompletedAt = &value
	}
	if expiresAt.Valid {
		value := expiresAt.Time
		job.ExpiresAt = &value
	}
	if assetsDeletedAt.Valid {
		value := assetsDeletedAt.Time
		job.AssetsDeletedAt = &value
	}
	return &job, nil
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}
