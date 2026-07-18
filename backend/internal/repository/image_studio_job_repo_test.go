package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
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
			json.RawMessage(`["inputs/upload-1/image-01.webp","inputs/upload-1/image-02.webp"]`),
			"inputs/upload-1/mask.png",
			now.Add(24*time.Hour),
			nil,
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
	require.Equal(t, []string{"inputs/upload-1/image-01.webp", "inputs/upload-1/image-02.webp"}, job.InputImagePaths)
	require.NotNil(t, job.InputMaskPath)
	require.Equal(t, "inputs/upload-1/mask.png", *job.InputMaskPath)
	require.NotNil(t, job.InputExpiresAt)
	require.Nil(t, job.InputDeletedAt)
	require.Equal(t, 0, job.AttemptCount)
	require.Equal(t, 3, job.MaxAttempts)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryCreateWritesOrderedInputMetadata(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	maskPath := "inputs/upload-1/mask.png"
	expiresAt := now.Add(24 * time.Hour)
	deletedAt := now.Add(time.Hour)
	paths := []string{"inputs/upload-1/image-01.webp", "inputs/upload-1/image-02.webp"}

	mock.ExpectQuery("INSERT INTO image_studio_jobs[\\s\\S]*input_image_paths, input_mask_path, input_expires_at, input_deleted_at[\\s\\S]*RETURNING").
		WithArgs(
			int64(7), int64(9), service.ImageStudioJobModeEdit, service.ImageStudioJobStatusQueued,
			[]byte(`{"model":"gpt-image-2"}`), "prompt", "gpt-image-2", "1024x1024", "png", 0.25, "balance_first",
			[]byte(`["inputs/upload-1/image-01.webp","inputs/upload-1/image-02.webp"]`), maskPath, expiresAt, deletedAt,
		).
		WillReturnRows(sqlmock.NewRows(imageStudioJobColumnNames()).AddRow(imageStudioJobRowValues(now, paths, &maskPath, &expiresAt, &deletedAt)...))

	repo := NewImageStudioJobRepository(nil, db)
	job, err := repo.Create(context.Background(), service.ImageStudioJobCreateInput{
		UserID: 7, APIKeyID: 9, Mode: service.ImageStudioJobModeEdit, Prompt: "prompt", Model: "gpt-image-2",
		Size: "1024x1024", OutputFormat: "png", EstimatedCostUSD: 0.25, BillingPriority: "balance_first",
		RequestPayload: json.RawMessage(`{"model":"gpt-image-2"}`), InputImagePaths: paths,
		InputMaskPath: &maskPath, InputExpiresAt: &expiresAt, InputDeletedAt: &deletedAt,
	})

	require.NoError(t, err)
	require.Equal(t, paths, job.InputImagePaths)
	require.NotNil(t, job.InputDeletedAt)
	require.Equal(t, deletedAt, *job.InputDeletedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryCreateRejectsMoreThanFourInputPaths(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewImageStudioJobRepository(nil, db)
	_, err = repo.Create(context.Background(), service.ImageStudioJobCreateInput{
		InputImagePaths: []string{"1", "2", "3", "4", "5"},
	})

	require.ErrorContains(t, err, "at most four")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestScanImageStudioJobRejectsInvalidInputImagePathArrays(t *testing.T) {
	tests := []struct {
		name  string
		paths json.RawMessage
	}{
		{name: "non-string item", paths: json.RawMessage(`["inputs/1.webp",42]`)},
		{name: "too many items", paths: json.RawMessage(`["1","2","3","4","5"]`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			now := time.Now()
			mock.ExpectQuery(regexp.QuoteMeta("SELECT " + imageStudioJobColumns + " FROM image_studio_jobs WHERE id = $1")).
				WithArgs(int64(39)).
				WillReturnRows(sqlmock.NewRows(imageStudioJobColumnNames()).AddRow(imageStudioJobRowValues(now, tt.paths, nil, nil, nil)...))

			repo := NewImageStudioJobRepository(nil, db)
			_, err = repo.GetByID(context.Background(), 39)
			require.Error(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func imageStudioJobRowValues(now time.Time, paths any, maskPath *string, inputExpiresAt, inputDeletedAt *time.Time) []driver.Value {
	if stringPaths, ok := paths.([]string); ok {
		encoded, err := json.Marshal(stringPaths)
		if err != nil {
			panic(err)
		}
		paths = encoded
	}
	return []driver.Value{
		int64(39), int64(7), int64(9), service.ImageStudioJobModeEdit, service.ImageStudioJobStatusQueued,
		json.RawMessage(`{"model":"gpt-image-2"}`), json.RawMessage(`{}`), "prompt", "gpt-image-2", "1024x1024", "png",
		paths, maskPath, inputExpiresAt, inputDeletedAt, 0.25, 0.0, "balance_first", 0, 3, nil,
		0.0, 0.0, nil, "", "", "", int64(0), 0, 0, "", "", now, nil, nil, nil, nil, nil, now, now,
	}
}

func TestImageStudioJobRepositoryDeleteByIDForUser(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM image_studio_jobs WHERE id = $1 AND user_id = $2 AND status = $3")).
		WithArgs(int64(39), int64(1), service.ImageStudioJobStatusDeleting).
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

	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM image_studio_jobs WHERE id = $1 AND user_id = $2 AND status = $3")).
		WithArgs(int64(39), int64(1), service.ImageStudioJobStatusDeleting).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := NewImageStudioJobRepository(nil, db)
	err = repo.DeleteByIDForUser(context.Background(), 39, 1)
	require.ErrorIs(t, err, service.ErrImageStudioJobNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryClaimDeletingRejectsRunningJobAsBusy(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("UPDATE image_studio_jobs[\\s\\S]*status = \\$3[\\s\\S]*status IN \\(\\$4, \\$5, \\$6, \\$3\\)[\\s\\S]*RETURNING").
		WithArgs(int64(39), int64(7), service.ImageStudioJobStatusDeleting, service.ImageStudioJobStatusQueued, service.ImageStudioJobStatusSucceeded, service.ImageStudioJobStatusFailed).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT status FROM image_studio_jobs WHERE id = \\$1 AND user_id = \\$2").
		WithArgs(int64(39), int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(service.ImageStudioJobStatusRunning))

	repo := NewImageStudioJobRepository(nil, db)
	_, err = repo.ClaimDeletingByIDForUser(context.Background(), 39, 7)

	require.ErrorIs(t, err, service.ErrImageStudioJobBusy)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryDeleteRequiresDeletingOwnership(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("DELETE FROM image_studio_jobs WHERE id = \\$1 AND user_id = \\$2 AND status = \\$3").
		WithArgs(int64(39), int64(1), service.ImageStudioJobStatusDeleting).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewImageStudioJobRepository(nil, db)
	err = repo.DeleteByIDForUser(context.Background(), 39, 1)
	require.NoError(t, err)
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

func TestImageStudioJobRepositoryListRunnableIncludesStaleRunning(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("WITH first_per_user[\\s\\S]*status = \\$3[\\s\\S]*heartbeat_at IS NULL OR heartbeat_at <= NOW\\(\\) - INTERVAL '5 minutes'[\\s\\S]*LIMIT \\$4").
		WithArgs(
			service.ImageStudioJobStatusQueued,
			service.ImageStudioJobStatusSettling,
			service.ImageStudioJobStatusRunning,
			2,
		).
		WillReturnRows(sqlmock.NewRows(imageStudioJobColumnNames()))

	repo := NewImageStudioJobRepository(nil, db)
	jobs, err := repo.ListRunnableJobs(context.Background(), 2)

	require.NoError(t, err)
	require.Empty(t, jobs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryListRunnableFiltersExpiredQueuedBeforeLimit(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("WITH first_per_user[\\s\\S]*status = \\$1[\\s\\S]*next_attempt_at IS NULL OR next_attempt_at <= NOW\\(\\)[\\s\\S]*input_expires_at IS NULL OR input_expires_at > NOW\\(\\)[\\s\\S]*status = \\$2[\\s\\S]*LIMIT \\$4").
		WithArgs(
			service.ImageStudioJobStatusQueued,
			service.ImageStudioJobStatusSettling,
			service.ImageStudioJobStatusRunning,
			2,
		).
		WillReturnRows(sqlmock.NewRows(imageStudioJobColumnNames()))

	repo := NewImageStudioJobRepository(nil, db)
	jobs, err := repo.ListRunnableJobs(context.Background(), 2)

	require.NoError(t, err)
	require.Empty(t, jobs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryUpdateHeartbeatOnlyTouchesRunning(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	heartbeatAt := time.Now()
	mock.ExpectExec("UPDATE image_studio_jobs SET heartbeat_at = \\$2, updated_at = NOW\\(\\) WHERE id = \\$1 AND status = \\$3").
		WithArgs(int64(39), heartbeatAt, service.ImageStudioJobStatusRunning).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewImageStudioJobRepository(nil, db)
	err = repo.UpdateHeartbeat(context.Background(), 39, heartbeatAt)

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryMarkStaleRunningFailedUsesHeartbeatGuard(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	completedAt := time.Now()
	staleBefore := completedAt.Add(-5 * time.Minute)
	mock.ExpectExec("UPDATE image_studio_jobs[\\s\\S]*status = \\$2[\\s\\S]*error_code = 'worker_interrupted'[\\s\\S]*WHERE id = \\$1[\\s\\S]*status = \\$4[\\s\\S]*heartbeat_at IS NULL OR heartbeat_at <= \\$5").
		WithArgs(
			int64(39),
			service.ImageStudioJobStatusFailed,
			completedAt,
			service.ImageStudioJobStatusRunning,
			staleBefore,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewImageStudioJobRepository(nil, db)
	changed, err := repo.MarkStaleRunningFailed(context.Background(), 39, completedAt, staleBefore)

	require.NoError(t, err)
	require.True(t, changed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryMarkRunningRejectsExpiredInputsAtClaimTime(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	claimAt := time.Now()
	mock.ExpectExec("UPDATE image_studio_jobs[\\s\\S]*status = \\$2[\\s\\S]*WHERE id = \\$1 AND status = \\$4[\\s\\S]*input_expires_at IS NULL OR input_expires_at > \\$3").
		WithArgs(int64(39), service.ImageStudioJobStatusRunning, claimAt, service.ImageStudioJobStatusQueued).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := NewImageStudioJobRepository(nil, db)
	claimed, err := repo.MarkRunning(context.Background(), 39, claimAt)

	require.NoError(t, err)
	require.False(t, claimed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryMarkRunningKeepsGenerationJobsWithoutInputExpiry(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	claimAt := time.Now()
	mock.ExpectExec("UPDATE image_studio_jobs[\\s\\S]*input_expires_at IS NULL OR input_expires_at > \\$3").
		WithArgs(int64(40), service.ImageStudioJobStatusRunning, claimAt, service.ImageStudioJobStatusQueued).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewImageStudioJobRepository(nil, db)
	claimed, err := repo.MarkRunning(context.Background(), 40, claimAt)

	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryExpireQueuedInputsUsesAtomicSkipLockedUpdate(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	paths := []string{"inputs/upload-expired/image-01.webp"}
	mock.ExpectQuery("WITH expired AS[\\s\\S]*SELECT id[\\s\\S]*status = \\$1[\\s\\S]*input_deleted_at IS NULL[\\s\\S]*input_expires_at <= \\$2[\\s\\S]*ORDER BY input_expires_at ASC, id ASC[\\s\\S]*FOR UPDATE SKIP LOCKED[\\s\\S]*LIMIT \\$3[\\s\\S]*UPDATE image_studio_jobs AS j[\\s\\S]*status = \\$4[\\s\\S]*error_code = 'input_expired'[\\s\\S]*next_attempt_at = NULL[\\s\\S]*completed_at = \\$2[\\s\\S]*FROM expired[\\s\\S]*RETURNING").
		WithArgs(service.ImageStudioJobStatusQueued, now, 2, service.ImageStudioJobStatusFailed).
		WillReturnRows(sqlmock.NewRows(imageStudioJobColumnNames()).AddRow(imageStudioJobRowValues(now, paths, nil, &now, nil)...))

	repo := NewImageStudioJobRepository(nil, db)
	jobs, err := repo.ExpireQueuedInputs(context.Background(), now, 2)

	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, paths, jobs[0].InputImagePaths)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryExpireQueuedInputsDefaultsNonPositiveLimit(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectQuery("WITH expired AS[\\s\\S]*LIMIT \\$3[\\s\\S]*UPDATE image_studio_jobs AS j").
		WithArgs(service.ImageStudioJobStatusQueued, now, 50, service.ImageStudioJobStatusFailed).
		WillReturnRows(sqlmock.NewRows(imageStudioJobColumnNames()))

	repo := NewImageStudioJobRepository(nil, db)
	jobs, err := repo.ExpireQueuedInputs(context.Background(), now, 0)

	require.NoError(t, err)
	require.Empty(t, jobs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryExpireQueuedInputsCapsLargeLimit(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectQuery("WITH expired AS[\\s\\S]*LIMIT \\$3[\\s\\S]*UPDATE image_studio_jobs AS j").
		WithArgs(service.ImageStudioJobStatusQueued, now, 500, service.ImageStudioJobStatusFailed).
		WillReturnRows(sqlmock.NewRows(imageStudioJobColumnNames()))

	repo := NewImageStudioJobRepository(nil, db)
	jobs, err := repo.ExpireQueuedInputs(context.Background(), now, 100_000)

	require.NoError(t, err)
	require.Empty(t, jobs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryListExpiredInputsExcludesQueuedAndRunning(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	paths := []string{"inputs/upload-failed/image-01.webp"}
	mock.ExpectQuery("SELECT[\\s\\S]*FROM image_studio_jobs[\\s\\S]*input_deleted_at IS NULL[\\s\\S]*input_expires_at <= \\$1[\\s\\S]*status NOT IN \\(\\$2, \\$3\\)[\\s\\S]*ORDER BY input_expires_at ASC, id ASC[\\s\\S]*LIMIT \\$4").
		WithArgs(now, service.ImageStudioJobStatusQueued, service.ImageStudioJobStatusRunning, 3).
		WillReturnRows(sqlmock.NewRows(imageStudioJobColumnNames()).AddRow(imageStudioJobRowValues(now, paths, nil, &now, nil)...))

	repo := NewImageStudioJobRepository(nil, db)
	jobs, err := repo.ListExpiredInputs(context.Background(), now, 3)

	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, paths, jobs[0].InputImagePaths)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryFailExpiredRunningInputsIsAtomic(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	now := time.Now()

	mock.ExpectExec("UPDATE image_studio_jobs[\\s\\S]*status = \\$2[\\s\\S]*error_code = 'input_expired'[\\s\\S]*WHERE id = \\$1 AND status = \\$4[\\s\\S]*input_expires_at <= \\$3").
		WithArgs(int64(39), service.ImageStudioJobStatusFailed, now, service.ImageStudioJobStatusRunning).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewImageStudioJobRepository(nil, db)
	changed, err := repo.FailExpiredRunningInputs(context.Background(), 39, now)

	require.NoError(t, err)
	require.True(t, changed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryListExpiredInputsPropagatesRowsError(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows(imageStudioJobColumnNames()).
		AddRow(imageStudioJobRowValues(now, []string{"inputs/upload-failed/image-01.webp"}, nil, &now, nil)...).
		RowError(0, context.Canceled)
	mock.ExpectQuery("SELECT[\\s\\S]*input_expires_at <= \\$1[\\s\\S]*LIMIT \\$4").
		WithArgs(now, service.ImageStudioJobStatusQueued, service.ImageStudioJobStatusRunning, 50).
		WillReturnRows(rows)

	repo := NewImageStudioJobRepository(nil, db)
	_, err = repo.ListExpiredInputs(context.Background(), now, -1)

	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryListExpiredInputsCapsLargeLimit(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectQuery("SELECT[\\s\\S]*input_expires_at <= \\$1[\\s\\S]*LIMIT \\$4").
		WithArgs(now, service.ImageStudioJobStatusQueued, service.ImageStudioJobStatusRunning, 500).
		WillReturnRows(sqlmock.NewRows(imageStudioJobColumnNames()))

	repo := NewImageStudioJobRepository(nil, db)
	jobs, err := repo.ListExpiredInputs(context.Background(), now, 100_000)

	require.NoError(t, err)
	require.Empty(t, jobs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryPersistLegacyInputsUpdatesActiveEditOnce(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expiresAt := time.Now().Add(24 * time.Hour)
	paths := []string{"inputs/upload-legacy/image-01.webp", "inputs/upload-legacy/image-02.webp"}
	maskPath := "inputs/upload-legacy/mask.png"
	redacted := json.RawMessage(`{"model":"gpt-image-2","prompt":"restored"}`)
	mock.ExpectExec("UPDATE image_studio_jobs[\\s\\S]*input_image_paths = \\$2[\\s\\S]*input_mask_path = \\$3[\\s\\S]*input_expires_at = \\$4[\\s\\S]*request_payload = \\$5[\\s\\S]*WHERE id = \\$1[\\s\\S]*mode = \\$6[\\s\\S]*status IN \\(\\$7, \\$8\\)[\\s\\S]*input_image_paths = '\\[\\]'::jsonb[\\s\\S]*input_mask_path IS NULL").
		WithArgs(int64(39), []byte(`["inputs/upload-legacy/image-01.webp","inputs/upload-legacy/image-02.webp"]`), maskPath, expiresAt, []byte(redacted), service.ImageStudioJobModeEdit, service.ImageStudioJobStatusQueued, service.ImageStudioJobStatusRunning).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewImageStudioJobRepository(nil, db)
	err = repo.PersistLegacyInputs(context.Background(), 39, paths, &maskPath, redacted, expiresAt)

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryPersistLegacyInputsRejectsAlreadyMaterializedJob(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expiresAt := time.Now().Add(24 * time.Hour)
	mock.ExpectExec("UPDATE image_studio_jobs[\\s\\S]*input_image_paths = '\\[\\]'::jsonb").
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := NewImageStudioJobRepository(nil, db)
	err = repo.PersistLegacyInputs(context.Background(), 39, []string{"inputs/upload-legacy/image-01.webp"}, nil, json.RawMessage(`{"model":"gpt-image-2"}`), expiresAt)

	require.ErrorIs(t, err, service.ErrImageStudioJobInvalid)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryPersistLegacyInputsValidatesPathCardinalityBeforeUpdate(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewImageStudioJobRepository(nil, db)
	err = repo.PersistLegacyInputs(context.Background(), 39, []string{"1", "2", "3", "4", "5"}, nil, json.RawMessage(`{}`), time.Now())

	require.ErrorContains(t, err, "at most four")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryPersistLegacyInputsRejectsInvalidMetadataBeforeUpdate(t *testing.T) {
	emptyMask := ""
	wrongDirMask := "inputs/upload-other/mask.png"
	wrongNameMask := "inputs/upload-valid/image-02.png"
	wrongExtMask := "inputs/upload-valid/mask.jpg"
	tests := []struct {
		name      string
		paths     []string
		maskPath  *string
		redacted  json.RawMessage
		wantError string
	}{
		{name: "zero paths", paths: nil, redacted: json.RawMessage(`{}`), wantError: "at least one"},
		{name: "absolute path", paths: []string{"/inputs/upload-valid/image-01.webp"}, redacted: json.RawMessage(`{}`), wantError: "path is invalid"},
		{name: "parent traversal", paths: []string{"inputs/upload-valid/../image-01.webp"}, redacted: json.RawMessage(`{}`), wantError: "path is invalid"},
		{name: "wrong ordered name", paths: []string{"inputs/upload-valid/image-02.webp"}, redacted: json.RawMessage(`{}`), wantError: "path is invalid"},
		{name: "unsupported image extension", paths: []string{"inputs/upload-valid/image-01.gif"}, redacted: json.RawMessage(`{}`), wantError: "path is invalid"},
		{name: "mixed image dirs", paths: []string{"inputs/upload-valid/image-01.webp", "inputs/upload-other/image-02.webp"}, redacted: json.RawMessage(`{}`), wantError: "share one upload directory"},
		{name: "empty mask", paths: []string{"inputs/upload-valid/image-01.webp"}, maskPath: &emptyMask, redacted: json.RawMessage(`{}`), wantError: "mask path is invalid"},
		{name: "mixed mask dir", paths: []string{"inputs/upload-valid/image-01.webp"}, maskPath: &wrongDirMask, redacted: json.RawMessage(`{}`), wantError: "share one upload directory"},
		{name: "wrong mask name", paths: []string{"inputs/upload-valid/image-01.webp"}, maskPath: &wrongNameMask, redacted: json.RawMessage(`{}`), wantError: "mask path is invalid"},
		{name: "unsupported mask extension", paths: []string{"inputs/upload-valid/image-01.webp"}, maskPath: &wrongExtMask, redacted: json.RawMessage(`{}`), wantError: "mask path is invalid"},
		{name: "malformed redacted payload", paths: []string{"inputs/upload-valid/image-01.webp"}, redacted: json.RawMessage(`{"model":`), wantError: "JSON object"},
		{name: "null redacted payload", paths: []string{"inputs/upload-valid/image-01.webp"}, redacted: json.RawMessage(`null`), wantError: "JSON object"},
		{name: "array redacted payload", paths: []string{"inputs/upload-valid/image-01.webp"}, redacted: json.RawMessage(`[]`), wantError: "JSON object"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			repo := NewImageStudioJobRepository(nil, db)
			err = repo.PersistLegacyInputs(context.Background(), 39, tt.paths, tt.maskPath, tt.redacted, time.Now())

			require.ErrorContains(t, err, tt.wantError)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestImageStudioJobRepositoryFailLegacyInputsAtomicallyRedactsAndTerminatesRunningEdit(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	completedAt := time.Now()
	redacted := json.RawMessage(`{"model":"gpt-image-2","prompt":"restore"}`)
	mock.ExpectExec("UPDATE image_studio_jobs[\\s\\S]*status = \\$2[\\s\\S]*error_code = 'legacy_input_invalid'[\\s\\S]*request_payload = \\$4[\\s\\S]*WHERE id = \\$1[\\s\\S]*mode = \\$5[\\s\\S]*status = \\$6[\\s\\S]*input_image_paths = '\\[\\]'::jsonb[\\s\\S]*input_mask_path IS NULL").
		WithArgs(int64(39), service.ImageStudioJobStatusFailed, completedAt, []byte(redacted), service.ImageStudioJobModeEdit, service.ImageStudioJobStatusRunning).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewImageStudioJobRepository(nil, db)
	err = repo.FailLegacyInputs(context.Background(), 39, redacted, completedAt)

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryFailLegacyInputsRejectsUnsafeRedactionBeforeUpdate(t *testing.T) {
	tests := []json.RawMessage{
		json.RawMessage(`null`),
		json.RawMessage(`[]`),
		json.RawMessage(`{"images":[]}`),
		json.RawMessage(`{"mask":null}`),
	}
	for _, redacted := range tests {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		repo := NewImageStudioJobRepository(nil, db)

		err = repo.FailLegacyInputs(context.Background(), 39, redacted, time.Now())

		require.Error(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	}
}

func TestImageStudioJobRepositoryMarkInputsDeletedKeepsFirstTimestamp(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	deletedAt := time.Now()
	mock.ExpectExec("UPDATE image_studio_jobs[\\s\\S]*input_deleted_at = COALESCE\\(input_deleted_at, \\$2\\)[\\s\\S]*WHERE id = \\$1").
		WithArgs(int64(39), deletedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewImageStudioJobRepository(nil, db)
	err = repo.MarkInputsDeleted(context.Background(), 39, deletedAt)

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryMarkInputsDeletedReturnsNotFound(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("UPDATE image_studio_jobs[\\s\\S]*input_deleted_at = COALESCE").
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := NewImageStudioJobRepository(nil, db)
	err = repo.MarkInputsDeleted(context.Background(), 39, time.Now())

	require.ErrorIs(t, err, service.ErrImageStudioJobNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryListReferencedInputDirsReturnsUndeletedUploadDirs(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT id, input_image_paths, input_mask_path[\\s\\S]*FROM image_studio_jobs[\\s\\S]*input_deleted_at IS NULL[\\s\\S]*ORDER BY id ASC").
		WillReturnRows(sqlmock.NewRows([]string{"id", "input_image_paths", "input_mask_path"}).
			AddRow(int64(39), json.RawMessage(`["inputs/upload-a/image-01.webp","inputs/upload-a/image-02.webp"]`), "inputs/upload-a/mask.png").
			AddRow(int64(40), json.RawMessage(`["inputs/upload-b/image-01.png"]`), nil).
			AddRow(int64(41), json.RawMessage(`["inputs/upload-a/image-01.webp"]`), nil))

	repo := NewImageStudioJobRepository(nil, db)
	dirs, err := repo.ListReferencedInputDirs(context.Background())

	require.NoError(t, err)
	require.Equal(t, map[string]struct{}{"inputs/upload-a": {}, "inputs/upload-b": {}}, dirs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryListRunningInputDirsReturnsOnlyRunningUndeletedDirs(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT id, input_image_paths, input_mask_path[\\s\\S]*FROM image_studio_jobs[\\s\\S]*status = \\$1[\\s\\S]*input_deleted_at IS NULL").
		WithArgs(service.ImageStudioJobStatusRunning).
		WillReturnRows(sqlmock.NewRows([]string{"id", "input_image_paths", "input_mask_path"}).
			AddRow(int64(39), json.RawMessage(`["inputs/upload-running/image-01.webp"]`), nil))

	repo := NewImageStudioJobRepository(nil, db)
	dirs, err := repo.ListRunningInputDirs(context.Background())

	require.NoError(t, err)
	require.Equal(t, map[string]struct{}{"inputs/upload-running": {}}, dirs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryListReferencedInputDirsRejectsInvalidPathJSON(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT id, input_image_paths, input_mask_path[\\s\\S]*input_deleted_at IS NULL").
		WillReturnRows(sqlmock.NewRows([]string{"id", "input_image_paths", "input_mask_path"}).
			AddRow(int64(39), json.RawMessage(`["inputs/upload-a/image-01.webp",42]`), nil))

	repo := NewImageStudioJobRepository(nil, db)
	_, err = repo.ListReferencedInputDirs(context.Background())

	require.ErrorContains(t, err, "JSON string array")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageStudioJobRepositoryListReferencedInputDirsRejectsUnsafeOrMixedDirectories(t *testing.T) {
	tests := []struct {
		name     string
		paths    json.RawMessage
		maskPath any
	}{
		{name: "unsafe", paths: json.RawMessage(`["inputs/../image-01.webp"]`)},
		{name: "terminal traversal", paths: json.RawMessage(`["inputs/upload-a/.."]`)},
		{name: "mixed", paths: json.RawMessage(`["inputs/upload-a/image-01.webp","inputs/upload-b/image-02.webp"]`)},
		{name: "mixed mask", paths: json.RawMessage(`["inputs/upload-a/image-01.webp"]`), maskPath: "inputs/upload-b/mask.png"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			mock.ExpectQuery("SELECT id, input_image_paths, input_mask_path[\\s\\S]*input_deleted_at IS NULL").
				WillReturnRows(sqlmock.NewRows([]string{"id", "input_image_paths", "input_mask_path"}).AddRow(int64(39), tt.paths, tt.maskPath))

			repo := NewImageStudioJobRepository(nil, db)
			_, err = repo.ListReferencedInputDirs(context.Background())

			require.Error(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
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

func TestImageStudioJobRepositoryMarkSettlementFailedRequiresSettlingState(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	completedAt := time.Now()
	mock.ExpectExec("UPDATE image_studio_jobs[\\s\\S]*status = \\$2[\\s\\S]*WHERE id = \\$1 AND status = \\$6").
		WithArgs(
			int64(39),
			service.ImageStudioJobStatusFailed,
			completedAt,
			"settlement_unrecoverable",
			"missing account",
			service.ImageStudioJobStatusSettling,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewImageStudioJobRepository(nil, db)
	changed, err := repo.MarkSettlementFailed(context.Background(), 39, completedAt, "settlement_unrecoverable", "missing account")

	require.NoError(t, err)
	require.True(t, changed)
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

func TestImageStudioJobRepositoryOutputRetentionIgnoresInputDeletionState(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	now := time.Now()

	mock.ExpectQuery("SELECT[\\s\\S]*FROM image_studio_jobs[\\s\\S]*status = \\$1[\\s\\S]*assets_deleted_at IS NULL[\\s\\S]*expires_at <= \\$2[\\s\\S]*LIMIT \\$3").
		WithArgs(service.ImageStudioJobStatusSucceeded, now, 50).
		WillReturnRows(sqlmock.NewRows(imageStudioJobColumnNames()))

	repo := NewImageStudioJobRepository(nil, db)
	items, err := repo.ListExpiredAssets(context.Background(), now, 50)

	require.NoError(t, err)
	require.Empty(t, items)
	require.NoError(t, mock.ExpectationsWereMet())
}

func imageStudioJobColumnNames() []string {
	parts := strings.Split(imageStudioJobColumns, ",")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	return parts
}
