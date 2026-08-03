package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type imageStudioJobRepository struct {
	db *sql.DB
}

const (
	defaultImageStudioInputLifecycleLimit = 50
	maxImageStudioInputLifecycleLimit     = 500
)

const imageStudioJobColumns = `
	id, user_id, api_key_id, mode, status, request_payload, settlement_payload, prompt, model, size, output_format,
	input_image_paths, input_mask_path, input_expires_at, input_deleted_at,
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
	inputImagePaths, err := encodeImageStudioInputPaths(input.InputImagePaths)
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO image_studio_jobs (
			user_id, api_key_id, mode, status, request_payload, prompt, model, size, output_format,
			estimated_cost_usd, billing_priority, input_image_paths, input_mask_path, input_expires_at,
			input_deleted_at, attempt_count, max_attempts, queued_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, 0, 3, NOW(), NOW(), NOW())
		RETURNING `+imageStudioJobColumns, input.UserID, input.APIKeyID, input.Mode, service.ImageStudioJobStatusQueued, []byte(input.RequestPayload), input.Prompt, input.Model, input.Size, input.OutputFormat, input.EstimatedCostUSD, input.BillingPriority, inputImagePaths, nullableImageStudioString(input.InputMaskPath), nullableTime(input.InputExpiresAt), nullableTime(input.InputDeletedAt))
	return scanImageStudioJob(row)
}

func (r *imageStudioJobRepository) GetByID(ctx context.Context, id int64) (*service.ImageStudioJob, error) {
	return r.getOne(ctx, `SELECT `+imageStudioJobColumns+` FROM image_studio_jobs WHERE id = $1`, id)
}

func (r *imageStudioJobRepository) GetByIDForUser(ctx context.Context, id, userID int64) (*service.ImageStudioJob, error) {
	return r.getOne(ctx, `SELECT `+imageStudioJobColumns+` FROM image_studio_jobs WHERE id = $1 AND user_id = $2`, id, userID)
}

func (r *imageStudioJobRepository) ClaimDeletingByIDForUser(ctx context.Context, id, userID int64) (*service.ImageStudioJob, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE image_studio_jobs
		SET status = $3, next_attempt_at = NULL, heartbeat_at = NULL, updated_at = NOW()
		WHERE id = $1 AND user_id = $2
			AND status IN ($4, $5, $6, $3)
		RETURNING `+imageStudioJobColumns+`
	`, id, userID, service.ImageStudioJobStatusDeleting, service.ImageStudioJobStatusQueued, service.ImageStudioJobStatusSucceeded, service.ImageStudioJobStatusFailed)
	job, err := scanImageStudioJob(row)
	if err == nil {
		return job, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var status string
	if err := r.db.QueryRowContext(ctx, `SELECT status FROM image_studio_jobs WHERE id = $1 AND user_id = $2`, id, userID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrImageStudioJobNotFound
		}
		return nil, err
	}
	return nil, service.ErrImageStudioJobBusy
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
	defer func() { _ = rows.Close() }()

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
	result, err := r.db.ExecContext(ctx, `DELETE FROM image_studio_jobs WHERE id = $1 AND user_id = $2 AND status = $3`, id, userID, service.ImageStudioJobStatusDeleting)
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
				AND (input_expires_at IS NULL OR input_expires_at > NOW())
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
	defer func() { _ = rows.Close() }()

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
			AND (input_expires_at IS NULL OR input_expires_at > $3)
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

func (r *imageStudioJobRepository) PersistLegacyInputs(ctx context.Context, id int64, paths []string, maskPath *string, redacted json.RawMessage, expiresAt time.Time) error {
	if len(paths) == 0 {
		return errors.New("image studio input paths must contain at least one item")
	}
	if len(paths) > 4 {
		return errors.New("image studio input paths must contain at most four items")
	}
	if _, err := referencedImageStudioInputDir(paths, maskPath); err != nil {
		return err
	}
	if err := validateImageStudioLegacyRedaction(redacted); err != nil {
		return err
	}
	encodedPaths, err := encodeImageStudioInputPaths(paths)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE image_studio_jobs
		SET input_image_paths = $2, input_mask_path = $3, input_expires_at = $4,
			request_payload = $5, updated_at = NOW()
		WHERE id = $1
			AND mode = $6
			AND status IN ($7, $8)
			AND input_image_paths = '[]'::jsonb
			AND input_mask_path IS NULL
			AND input_deleted_at IS NULL
	`, id, encodedPaths, nullableImageStudioString(maskPath), expiresAt, []byte(redacted), service.ImageStudioJobModeEdit, service.ImageStudioJobStatusQueued, service.ImageStudioJobStatusRunning)
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

func (r *imageStudioJobRepository) FailLegacyInputs(ctx context.Context, id int64, redacted json.RawMessage, completedAt time.Time) error {
	if err := validateImageStudioLegacyRedaction(redacted); err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE image_studio_jobs
		SET status = $2, completed_at = $3, error_code = 'legacy_input_invalid',
			error_message = 'legacy image studio input is invalid', request_payload = $4,
			next_attempt_at = NULL, heartbeat_at = NULL, updated_at = NOW()
		WHERE id = $1
			AND mode = $5
			AND status = $6
			AND input_image_paths = '[]'::jsonb
			AND input_mask_path IS NULL
			AND input_deleted_at IS NULL
	`, id, service.ImageStudioJobStatusFailed, completedAt, []byte(redacted), service.ImageStudioJobModeEdit, service.ImageStudioJobStatusRunning)
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

func validateImageStudioLegacyRedaction(redacted json.RawMessage) error {
	var redactedObject map[string]json.RawMessage
	if err := json.Unmarshal(redacted, &redactedObject); err != nil || redactedObject == nil {
		return errors.New("image studio redacted request payload must be a JSON object")
	}
	if _, ok := redactedObject["images"]; ok {
		return errors.New("image studio redacted request payload must not contain images")
	}
	if _, ok := redactedObject["mask"]; ok {
		return errors.New("image studio redacted request payload must not contain mask")
	}
	return nil
}

func (r *imageStudioJobRepository) ExpireQueuedInputs(ctx context.Context, now time.Time, limit int) ([]service.ImageStudioJob, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("image studio job repository db is nil")
	}
	limit = normalizeImageStudioInputLifecycleLimit(limit)
	rows, err := r.db.QueryContext(ctx, `
		WITH expired AS (
			SELECT id
			FROM image_studio_jobs
			WHERE status = $1
				AND input_deleted_at IS NULL
				AND input_expires_at IS NOT NULL
				AND input_expires_at <= $2
			ORDER BY input_expires_at ASC, id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $3
		)
		UPDATE image_studio_jobs AS j
		SET status = $4, error_code = 'input_expired',
			error_message = 'image studio input files expired before execution',
			next_attempt_at = NULL, completed_at = $2, heartbeat_at = NULL, updated_at = NOW()
		FROM expired
		WHERE j.id = expired.id
		RETURNING `+qualifiedImageStudioJobColumns("j")+`
	`, service.ImageStudioJobStatusQueued, now, limit, service.ImageStudioJobStatusFailed)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	jobs := make([]service.ImageStudioJob, 0, limit)
	for rows.Next() {
		job, scanErr := scanImageStudioJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		jobs = append(jobs, *job)
	}
	return jobs, rows.Err()
}

func (r *imageStudioJobRepository) ListExpiredInputs(ctx context.Context, now time.Time, limit int) ([]service.ImageStudioJob, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("image studio job repository db is nil")
	}
	limit = normalizeImageStudioInputLifecycleLimit(limit)
	rows, err := r.db.QueryContext(ctx, `
			SELECT `+imageStudioJobColumns+`
			FROM image_studio_jobs
			WHERE input_deleted_at IS NULL
				AND (
					(status IN ($2, $3) AND (input_image_paths <> '[]'::jsonb OR input_mask_path IS NOT NULL))
					OR (
						input_expires_at IS NOT NULL
						AND input_expires_at <= $1
						AND status NOT IN ($4, $5)
					)
				)
			ORDER BY CASE WHEN status IN ($2, $3) THEN 0 ELSE 1 END,
				input_expires_at ASC NULLS LAST, id ASC
			LIMIT $6
		`, now, service.ImageStudioJobStatusSettling, service.ImageStudioJobStatusSucceeded,
		service.ImageStudioJobStatusQueued, service.ImageStudioJobStatusRunning, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	jobs := make([]service.ImageStudioJob, 0, limit)
	for rows.Next() {
		job, scanErr := scanImageStudioJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		jobs = append(jobs, *job)
	}
	return jobs, rows.Err()
}

func (r *imageStudioJobRepository) MarkInputsDeleted(ctx context.Context, id int64, deletedAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE image_studio_jobs
		SET input_deleted_at = COALESCE(input_deleted_at, $2), updated_at = NOW()
		WHERE id = $1
	`, id, deletedAt)
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

func (r *imageStudioJobRepository) FailExpiredRunningInputs(ctx context.Context, id int64, completedAt time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE image_studio_jobs
		SET status = $2, completed_at = $3, error_code = 'input_expired',
			error_message = 'image studio input files expired during execution',
			next_attempt_at = NULL, heartbeat_at = NULL, updated_at = NOW()
		WHERE id = $1 AND status = $4
			AND input_expires_at IS NOT NULL AND input_expires_at <= $3
	`, id, service.ImageStudioJobStatusFailed, completedAt, service.ImageStudioJobStatusRunning)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

func (r *imageStudioJobRepository) ListReferencedInputDirs(ctx context.Context) (map[string]struct{}, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("image studio job repository db is nil")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, input_image_paths, input_mask_path
		FROM image_studio_jobs
		WHERE input_deleted_at IS NULL
			AND (input_image_paths <> '[]'::jsonb OR input_mask_path IS NOT NULL)
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	dirs := make(map[string]struct{})
	for rows.Next() {
		var (
			id       int64
			rawPaths []byte
			maskPath sql.NullString
		)
		if err := rows.Scan(&id, &rawPaths, &maskPath); err != nil {
			return nil, err
		}
		paths, err := decodeImageStudioInputPaths(rawPaths)
		if err != nil {
			return nil, fmt.Errorf("image studio job %d: %w", id, err)
		}
		var mask *string
		if maskPath.Valid {
			mask = &maskPath.String
		}
		dir, err := referencedImageStudioInputDir(paths, mask)
		if err != nil {
			return nil, fmt.Errorf("image studio job %d: %w", id, err)
		}
		dirs[dir] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return dirs, nil
}

func (r *imageStudioJobRepository) ListRunningInputDirs(ctx context.Context) (map[string]struct{}, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("image studio job repository db is nil")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, input_image_paths, input_mask_path
		FROM image_studio_jobs
		WHERE status = $1
			AND input_deleted_at IS NULL
			AND (input_image_paths <> '[]'::jsonb OR input_mask_path IS NOT NULL)
		ORDER BY id ASC
	`, service.ImageStudioJobStatusRunning)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	dirs := make(map[string]struct{})
	for rows.Next() {
		var (
			id       int64
			rawPaths []byte
			maskPath sql.NullString
		)
		if err := rows.Scan(&id, &rawPaths, &maskPath); err != nil {
			return nil, err
		}
		paths, err := decodeImageStudioInputPaths(rawPaths)
		if err != nil {
			return nil, fmt.Errorf("image studio job %d: %w", id, err)
		}
		var mask *string
		if maskPath.Valid {
			mask = &maskPath.String
		}
		dir, err := referencedImageStudioInputDir(paths, mask)
		if err != nil {
			return nil, fmt.Errorf("image studio job %d: %w", id, err)
		}
		dirs[dir] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return dirs, nil
}

func referencedImageStudioInputDir(paths []string, maskPath *string) (string, error) {
	var uploadDir string
	for i, inputPath := range paths {
		dir, err := imageStudioInputDirForPath(inputPath, fmt.Sprintf("image-%02d", i+1), false)
		if err != nil {
			return "", err
		}
		if uploadDir == "" {
			uploadDir = dir
		} else if dir != uploadDir {
			return "", errors.New("image studio input paths must share one upload directory")
		}
	}
	if maskPath != nil {
		maskDir, err := imageStudioInputDirForPath(*maskPath, "mask", true)
		if err != nil {
			return "", errors.New("image studio input mask path is invalid")
		}
		if uploadDir == "" {
			uploadDir = maskDir
		} else if maskDir != uploadDir {
			return "", errors.New("image studio input paths must share one upload directory")
		}
	}
	if uploadDir == "" {
		return "", errors.New("image studio input paths are empty")
	}
	return uploadDir, nil
}

func imageStudioInputDirForPath(inputPath, expectedBase string, mask bool) (string, error) {
	parts := strings.Split(inputPath, "/")
	if strings.TrimSpace(inputPath) != inputPath || strings.Contains(inputPath, "\\") || len(parts) != 3 ||
		parts[0] != "inputs" || !strings.HasPrefix(parts[1], "upload-") || len(parts[1]) == len("upload-") {
		return "", errors.New("image studio input path is invalid")
	}
	fileName := parts[2]
	dot := strings.LastIndex(fileName, ".")
	if dot <= 0 || fileName[:dot] != expectedBase {
		return "", errors.New("image studio input path is invalid")
	}
	extension := strings.ToLower(fileName[dot:])
	if mask {
		if extension != ".png" && extension != ".webp" {
			return "", errors.New("image studio input path is invalid")
		}
	} else if extension != ".png" && extension != ".jpg" && extension != ".webp" {
		return "", errors.New("image studio input path is invalid")
	}
	return parts[0] + "/" + parts[1], nil
}

func normalizeImageStudioInputLifecycleLimit(limit int) int {
	if limit <= 0 {
		return defaultImageStudioInputLifecycleLimit
	}
	if limit > maxImageStudioInputLifecycleLimit {
		return maxImageStudioInputLifecycleLimit
	}
	return limit
}

func (r *imageStudioJobRepository) MarkStaleRunningFailed(ctx context.Context, id int64, completedAt, staleBefore time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE image_studio_jobs
		SET status = $2, completed_at = $3, error_code = 'worker_interrupted',
			error_message = 'image generation worker was interrupted', next_attempt_at = NULL,
			heartbeat_at = NULL,
			request_payload = CASE
				WHEN mode = $6 AND input_image_paths = '[]'::jsonb AND input_mask_path IS NULL
				THEN request_payload - 'images' - 'mask'
				ELSE request_payload
			END,
			updated_at = NOW()
		WHERE id = $1
			AND status = $4
			AND (heartbeat_at IS NULL OR heartbeat_at <= $5)
	`, id, service.ImageStudioJobStatusFailed, completedAt, service.ImageStudioJobStatusRunning, staleBefore, service.ImageStudioJobModeEdit)
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

func (r *imageStudioJobRepository) MarkSettlementFailed(ctx context.Context, id int64, completedAt time.Time, errorCode, errorMessage string) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE image_studio_jobs
		SET status = $2, completed_at = $3, error_code = $4, error_message = $5,
			next_attempt_at = NULL, heartbeat_at = NULL, updated_at = NOW()
		WHERE id = $1 AND status = $6
	`, id, service.ImageStudioJobStatusFailed, completedAt, errorCode, errorMessage, service.ImageStudioJobStatusSettling)
	if err != nil {
		return false, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rowsAffected > 0, nil
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
	defer func() { _ = rows.Close() }()

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
		inputImagePaths []byte
		inputMaskPath   sql.NullString
		inputExpiresAt  sql.NullTime
		inputDeletedAt  sql.NullTime
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
		&inputImagePaths,
		&inputMaskPath,
		&inputExpiresAt,
		&inputDeletedAt,
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
	job.InputImagePaths, err = decodeImageStudioInputPaths(inputImagePaths)
	if err != nil {
		return nil, err
	}
	if inputMaskPath.Valid {
		value := inputMaskPath.String
		job.InputMaskPath = &value
	}
	if inputExpiresAt.Valid {
		value := inputExpiresAt.Time
		job.InputExpiresAt = &value
	}
	if inputDeletedAt.Valid {
		value := inputDeletedAt.Time
		job.InputDeletedAt = &value
	}
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

func encodeImageStudioInputPaths(paths []string) ([]byte, error) {
	if len(paths) > 4 {
		return nil, errors.New("image studio input paths must contain at most four items")
	}
	if paths == nil {
		paths = []string{}
	}
	return json.Marshal(paths)
}

func decodeImageStudioInputPaths(raw []byte) ([]string, error) {
	var values []any
	if len(raw) == 0 || string(raw) == "null" {
		return nil, errors.New("image studio input paths must be a JSON string array")
	}
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, errors.New("image studio input paths must be a JSON string array")
	}
	if len(values) > 4 {
		return nil, errors.New("image studio input paths must contain at most four items")
	}
	paths := make([]string, len(values))
	for i, value := range values {
		path, ok := value.(string)
		if !ok {
			return nil, errors.New("image studio input paths must be a JSON string array")
		}
		paths[i] = path
	}
	return paths, nil
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableImageStudioString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
