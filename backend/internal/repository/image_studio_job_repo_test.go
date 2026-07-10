package repository

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestImageStudioJobRepositoryGetByIDForUserUsesStableColumnOrder(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows(imageStudioJobColumnNames()).
		AddRow(
			int64(39),
			int64(1),
			int64(1),
			service.ImageStudioJobModeGenerate,
			service.ImageStudioJobStatusSucceeded,
			json.RawMessage(`{"model":"gpt-image-2"}`),
			json.RawMessage(`{"account_id":77}`),
			"prompt",
			"gpt-image-2",
			"1024x1024",
			"jpeg",
			1.0,
			1.0,
			"",
			0,
			3,
			nil,
			0.0,
			0.0,
			nil,
			"/app/data/image-studio/39/original.png",
			"/app/data/image-studio/39/thumbnail.jpg",
			"image/png",
			int64(123),
			1024,
			1024,
			"",
			"",
			now,
			nil,
			nil,
			now,
			nil,
			nil,
			now,
			now,
		)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT "+imageStudioJobColumns+" FROM image_studio_jobs WHERE id = $1 AND user_id = $2")).
		WithArgs(int64(39), int64(1)).
		WillReturnRows(rows)

	repo := NewImageStudioJobRepository(nil, db)
	job, err := repo.GetByIDForUser(context.Background(), 39, 1)
	require.NoError(t, err)
	require.Equal(t, "/app/data/image-studio/39/original.png", job.OriginalPath)
	require.Equal(t, "/app/data/image-studio/39/thumbnail.jpg", job.ThumbnailPath)
	require.JSONEq(t, `{"account_id":77}`, string(job.SettlementPayload))
	require.Equal(t, 0, job.AttemptCount)
	require.Equal(t, 3, job.MaxAttempts)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryDeleteByIDForUser(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM image_studio_jobs WHERE id = $1 AND user_id = $2")).
		WithArgs(int64(39), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewImageStudioJobRepository(nil, db)
	err = repo.DeleteByIDForUser(context.Background(), 39, 1)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryDeleteByIDForUserReturnsNotFound(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM image_studio_jobs WHERE id = $1 AND user_id = $2")).
		WithArgs(int64(39), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := NewImageStudioJobRepository(nil, db)
	err = repo.DeleteByIDForUser(context.Background(), 39, 1)
	require.ErrorIs(t, err, service.ErrImageStudioJobNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryCountStatusByUser(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT
			COUNT(*) FILTER (WHERE status IN ($2, $3, $4)) AS pending_count,
			COUNT(*) FILTER (WHERE status = $5) AS failed_count
		FROM image_studio_jobs
		WHERE user_id = $1
	`)).
		WithArgs(int64(7), service.ImageStudioJobStatusQueued, service.ImageStudioJobStatusRunning, service.ImageStudioJobStatusSettling, service.ImageStudioJobStatusFailed).
		WillReturnRows(sqlmock.NewRows([]string{"pending_count", "failed_count"}).AddRow(int64(2), int64(3)))

	repo := NewImageStudioJobRepository(nil, db)
	stats, err := repo.CountStatusByUser(context.Background(), 7)

	require.NoError(t, err)
	require.Equal(t, int64(2), stats.PendingCount)
	require.Equal(t, int64(3), stats.FailedCount)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryMarkSettlingStoresAssetsBeforeBilling(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	leaseAt := time.Now()
	settlementPayload := json.RawMessage(`{"account_id":77}`)
	mock.ExpectExec("UPDATE image_studio_jobs[\\s\\S]*status = \\$2[\\s\\S]*settlement_payload = \\$4[\\s\\S]*original_path = \\$5[\\s\\S]*WHERE id = \\$1 AND status = \\$3").
		WithArgs(
			int64(39),
			service.ImageStudioJobStatusSettling,
			service.ImageStudioJobStatusRunning,
			[]byte(settlementPayload),
			"/app/data/image-studio/39/original.png",
			"/app/data/image-studio/39/thumbnail.jpg",
			"image/png",
			int64(123),
			1024,
			768,
			leaseAt,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewImageStudioJobRepository(nil, db)
	err = repo.MarkSettling(
		context.Background(),
		39,
		settlementPayload,
		"/app/data/image-studio/39/original.png",
		"/app/data/image-studio/39/thumbnail.jpg",
		"image/png",
		123,
		1024,
		768,
		leaseAt,
	)

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryClaimSettlingUsesLease(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	leaseAt := time.Now()
	staleBefore := leaseAt.Add(-5 * time.Minute)
	mock.ExpectExec("UPDATE image_studio_jobs[\\s\\S]*heartbeat_at = \\$2[\\s\\S]*heartbeat_at IS NULL OR heartbeat_at <= \\$3[\\s\\S]*status = \\$4").
		WithArgs(int64(39), leaseAt, staleBefore, service.ImageStudioJobStatusSettling).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewImageStudioJobRepository(nil, db)
	claimed, err := repo.ClaimSettling(context.Background(), 39, leaseAt, staleBefore)

	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryMarkSettlementRetryableKeepsSettlementStage(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	nextAttemptAt := time.Now().Add(time.Minute)
	mock.ExpectExec("UPDATE image_studio_jobs[\\s\\S]*status = \\$2[\\s\\S]*heartbeat_at = NULL[\\s\\S]*attempt_count = attempt_count \\+ 1[\\s\\S]*WHERE id = \\$1 AND status = \\$6").
		WithArgs(int64(39), service.ImageStudioJobStatusSettling, "billing_failed", "temporary", nextAttemptAt, service.ImageStudioJobStatusSettling).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewImageStudioJobRepository(nil, db)
	err = repo.MarkSettlementRetryable(context.Background(), 39, nextAttemptAt, "billing_failed", "temporary")

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryMarkSucceededRequiresSettlingState(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	completedAt := time.Now()
	mock.ExpectExec("UPDATE image_studio_jobs[\\s\\S]*status = \\$2[\\s\\S]*heartbeat_at = NULL[\\s\\S]*WHERE id = \\$1 AND status = \\$12").
		WithArgs(
			int64(39),
			service.ImageStudioJobStatusSucceeded,
			completedAt,
			0.25,
			"/app/data/image-studio/39/original.png",
			"/app/data/image-studio/39/thumbnail.jpg",
			"image/png",
			int64(123),
			1024,
			768,
			nil,
			service.ImageStudioJobStatusSettling,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewImageStudioJobRepository(nil, db)
	err = repo.MarkSucceeded(
		context.Background(),
		39,
		completedAt,
		0.25,
		"/app/data/image-studio/39/original.png",
		"/app/data/image-studio/39/thumbnail.jpg",
		"image/png",
		123,
		1024,
		768,
		nil,
	)

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryMarkSucceededRejectsStaleWorker(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	completedAt := time.Now()
	mock.ExpectExec("UPDATE image_studio_jobs[\\s\\S]*WHERE id = \\$1 AND status = \\$12").
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := NewImageStudioJobRepository(nil, db)
	err = repo.MarkSucceeded(context.Background(), 39, completedAt, 0.25, "", "", "", 0, 0, 0, nil)

	require.ErrorIs(t, err, service.ErrImageStudioJobInvalid)
	require.NoError(t, mock.ExpectationsWereMet())
}

func imageStudioJobColumnNames() []string {
	parts := strings.Split(imageStudioJobColumns, ",")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	return parts
}
