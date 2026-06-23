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
			COUNT(*) FILTER (WHERE status IN ($2, $3)) AS pending_count,
			COUNT(*) FILTER (WHERE status = $4) AS failed_count
		FROM image_studio_jobs
		WHERE user_id = $1
	`)).
		WithArgs(int64(7), service.ImageStudioJobStatusQueued, service.ImageStudioJobStatusRunning, service.ImageStudioJobStatusFailed).
		WillReturnRows(sqlmock.NewRows([]string{"pending_count", "failed_count"}).AddRow(int64(2), int64(3)))

	repo := NewImageStudioJobRepository(nil, db)
	stats, err := repo.CountStatusByUser(context.Background(), 7)

	require.NoError(t, err)
	require.Equal(t, int64(2), stats.PendingCount)
	require.Equal(t, int64(3), stats.FailedCount)
	require.NoError(t, mock.ExpectationsWereMet())
}

func imageStudioJobColumnNames() []string {
	parts := strings.Split(imageStudioJobColumns, ",")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	return parts
}
