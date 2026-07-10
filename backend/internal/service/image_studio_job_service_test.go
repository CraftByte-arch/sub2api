package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestImageStudioJobServiceDeleteJobRemovesAssetsAndRecord(t *testing.T) {
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "original.png")
	thumbnailPath := filepath.Join(dir, "thumbnail.jpg")
	require.NoError(t, os.WriteFile(originalPath, []byte("original"), 0o600))
	require.NoError(t, os.WriteFile(thumbnailPath, []byte("thumbnail"), 0o600))

	repo := &imageStudioJobDeleteRepoStub{
		job: &ImageStudioJob{
			ID:            44,
			UserID:        7,
			OriginalPath:  originalPath,
			ThumbnailPath: thumbnailPath,
		},
	}
	svc := NewImageStudioJobService(repo, nil)

	err := svc.DeleteJob(context.Background(), 7, 44)

	require.NoError(t, err)
	require.Equal(t, int64(44), repo.deletedID)
	require.Equal(t, int64(7), repo.deletedUserID)
	require.NoFileExists(t, originalPath)
	require.NoFileExists(t, thumbnailPath)
}

func TestImageStudioJobServiceDeleteJobDoesNotDeleteRecordWhenAssetRemovalFails(t *testing.T) {
	dir := t.TempDir()
	nonEmptyDir := filepath.Join(dir, "asset-dir")
	require.NoError(t, os.Mkdir(nonEmptyDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(nonEmptyDir, "child"), []byte("x"), 0o600))

	repo := &imageStudioJobDeleteRepoStub{
		job: &ImageStudioJob{
			ID:           44,
			UserID:       7,
			OriginalPath: nonEmptyDir,
		},
	}
	svc := NewImageStudioJobService(repo, nil)

	err := svc.DeleteJob(context.Background(), 7, 44)

	require.Error(t, err)
	require.Zero(t, repo.deletedID)
}

func TestImageStudioJobServiceEstimateCostUsesGatewayDefaultImagePrice(t *testing.T) {
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	openAIGateway := NewOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
		NewBillingService(cfg, nil), nil, &BillingCacheService{}, nil,
		&DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	svc := &ImageStudioJobService{openAIGateway: openAIGateway}
	groupID := int64(11)

	cost, err := svc.EstimateCost(context.Background(), &APIKey{
		ID:      12,
		GroupID: &groupID,
		Group: &Group{
			ID:             groupID,
			RateMultiplier: 1,
		},
		User: &User{ID: 13},
	}, "gpt-image-1", "1024x1024")

	require.NoError(t, err)
	require.NotNil(t, cost)
	require.Positive(t, cost.ActualCost)
}

type imageStudioJobDeleteRepoStub struct {
	job           *ImageStudioJob
	deletedID     int64
	deletedUserID int64
}

func (r *imageStudioJobDeleteRepoStub) Create(context.Context, ImageStudioJobCreateInput) (*ImageStudioJob, error) {
	panic("unexpected Create call")
}

func (r *imageStudioJobDeleteRepoStub) GetByID(context.Context, int64) (*ImageStudioJob, error) {
	panic("unexpected GetByID call")
}

func (r *imageStudioJobDeleteRepoStub) GetByIDForUser(_ context.Context, id, userID int64) (*ImageStudioJob, error) {
	if r.job == nil || r.job.ID != id || r.job.UserID != userID {
		return nil, ErrImageStudioJobNotFound
	}
	return r.job, nil
}

func (r *imageStudioJobDeleteRepoStub) ListByUser(context.Context, int64, int, int) (*ImageStudioJobList, error) {
	panic("unexpected ListByUser call")
}

func (r *imageStudioJobDeleteRepoStub) CountStatusByUser(context.Context, int64) (*ImageStudioJobStats, error) {
	panic("unexpected CountStatusByUser call")
}

func (r *imageStudioJobDeleteRepoStub) DeleteByIDForUser(_ context.Context, id, userID int64) error {
	r.deletedID = id
	r.deletedUserID = userID
	return nil
}

func (r *imageStudioJobDeleteRepoStub) ListRunnableJobs(context.Context, int) ([]ImageStudioJob, error) {
	panic("unexpected ListRunnableJobs call")
}

func (r *imageStudioJobDeleteRepoStub) MarkRunning(context.Context, int64, time.Time) (bool, error) {
	panic("unexpected MarkRunning call")
}

func (r *imageStudioJobDeleteRepoStub) MarkSettling(context.Context, int64, json.RawMessage, string, string, string, int64, int, int, time.Time) error {
	panic("unexpected MarkSettling call")
}

func (r *imageStudioJobDeleteRepoStub) ClaimSettling(context.Context, int64, time.Time, time.Time) (bool, error) {
	panic("unexpected ClaimSettling call")
}

func (r *imageStudioJobDeleteRepoStub) UpdateHeartbeat(context.Context, int64, time.Time) error {
	panic("unexpected UpdateHeartbeat call")
}

func (r *imageStudioJobDeleteRepoStub) MarkRetryable(context.Context, int64, time.Time, string, string) error {
	panic("unexpected MarkRetryable call")
}

func (r *imageStudioJobDeleteRepoStub) MarkSettlementRetryable(context.Context, int64, time.Time, string, string) error {
	panic("unexpected MarkSettlementRetryable call")
}

func (r *imageStudioJobDeleteRepoStub) MarkSucceeded(context.Context, int64, time.Time, float64, string, string, string, int64, int, int, *time.Time) error {
	panic("unexpected MarkSucceeded call")
}

func (r *imageStudioJobDeleteRepoStub) MarkFailed(context.Context, int64, time.Time, string, string) error {
	panic("unexpected MarkFailed call")
}

func (r *imageStudioJobDeleteRepoStub) ListExpiredAssets(context.Context, time.Time, int) ([]ImageStudioJob, error) {
	panic("unexpected ListExpiredAssets call")
}

func (r *imageStudioJobDeleteRepoStub) MarkAssetsDeleted(context.Context, int64, time.Time) error {
	panic("unexpected MarkAssetsDeleted call")
}
