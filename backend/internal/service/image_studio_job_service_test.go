package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestImageStudioJobServiceCreateEditJobStagesInputsAndSanitizesPayload(t *testing.T) {
	now := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	maskPath := "inputs/upload-test/mask.png"
	store := &imageStudioCreateStorageStub{staged: &StagedEditInputs{
		UploadID:   "upload-test",
		ImagePaths: []string{"inputs/upload-test/image-01.png", "inputs/upload-test/image-02.webp"},
		MaskPath:   &maskPath,
	}}
	repo := &imageStudioJobDeleteRepoStub{}
	svc := NewImageStudioJobService(repo, nil, store, func() time.Time { return now })

	job, err := svc.CreateEditJob(context.Background(), ImageStudioJobCreateInput{
		UserID:       7,
		APIKeyID:     8,
		Mode:         ImageStudioJobModeEdit,
		Prompt:       "replace the sky",
		Model:        "gpt-image-2",
		OutputFormat: "webp",
		RequestPayload: json.RawMessage(`{
			"model":"gpt-image-2","prompt":"replace the sky","size":"1024x1024",
			"quality":"high","background":"transparent","style":"vivid",
			"moderation":"low","input_fidelity":"high","output_format":"webp",
			"output_compression":72,"response_format":"b64_json",
			"images":[{"image_url":"data:image/png;base64,SECRET"}],
			"mask":{"image_url":"data:image/png;base64,MASK"},"unknown":"drop-me"
		}`),
	}, []UploadedFile{
		{Reader: strings.NewReader("first"), ContentType: "image/png"},
		{Reader: strings.NewReader("second"), ContentType: "image/webp"},
	}, &UploadedFile{Reader: strings.NewReader("mask"), ContentType: "image/png"})

	require.NoError(t, err)
	require.NotNil(t, job)
	require.Len(t, store.images, 2)
	require.NotNil(t, store.mask)
	require.Equal(t, ImageStudioJobModeEdit, repo.lastCreateInput.Mode)
	require.Equal(t, store.staged.ImagePaths, repo.lastCreateInput.InputImagePaths)
	require.Equal(t, store.staged.MaskPath, repo.lastCreateInput.InputMaskPath)
	require.NotNil(t, repo.lastCreateInput.InputExpiresAt)
	require.Equal(t, now.Add(DefaultImageStudioInputRetentionHours*time.Hour), *repo.lastCreateInput.InputExpiresAt)
	require.JSONEq(t, `{
		"model":"gpt-image-2","prompt":"replace the sky","size":"1024x1024",
		"quality":"high","background":"transparent","style":"vivid",
		"moderation":"low","input_fidelity":"high","output_format":"webp",
		"output_compression":72,"response_format":"b64_json"
	}`, string(repo.lastCreateInput.RequestPayload))
	require.NotContains(t, string(repo.lastCreateInput.RequestPayload), "SECRET")
	require.NotContains(t, string(repo.lastCreateInput.RequestPayload), "MASK")
}

func TestImageStudioJobServiceCreateEditJobUsesConfiguredInputRetention(t *testing.T) {
	now := time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC)
	settingSvc := NewSettingService(&imageStudioCreateSettingRepoStub{values: map[string]string{
		SettingKeyImageStudioInputRetentionHours: "6",
	}}, &config.Config{})
	store := &imageStudioCreateStorageStub{staged: &StagedEditInputs{
		UploadID: "upload-test", ImagePaths: []string{"inputs/upload-test/image-01.png"},
	}}
	repo := &imageStudioJobDeleteRepoStub{}
	svc := NewImageStudioJobService(repo, settingSvc, store, func() time.Time { return now })

	_, err := svc.CreateEditJob(context.Background(), validImageStudioEditCreateInput(), []UploadedFile{{
		Reader: bytes.NewReader([]byte("image")), ContentType: "image/png",
	}}, nil)

	require.NoError(t, err)
	require.NotNil(t, repo.lastCreateInput.InputExpiresAt)
	require.Equal(t, now.Add(6*time.Hour), *repo.lastCreateInput.InputExpiresAt)
}

func TestImageStudioJobServiceCreateEditJobRollsBackWhenRepositoryCreateFails(t *testing.T) {
	repoFailure := errors.New("insert failed")
	store := &imageStudioCreateStorageStub{staged: &StagedEditInputs{
		UploadID: "upload-test", ImagePaths: []string{"inputs/upload-test/image-01.png"},
	}}
	repo := &imageStudioJobDeleteRepoStub{createErr: repoFailure}
	svc := NewImageStudioJobService(repo, nil, store, time.Now)

	job, err := svc.CreateEditJob(context.Background(), validImageStudioEditCreateInput(), []UploadedFile{{
		Reader: bytes.NewReader([]byte("image")), ContentType: "image/png",
	}}, nil)

	require.Nil(t, job)
	require.ErrorIs(t, err, repoFailure)
	require.Equal(t, store.staged.ImagePaths, store.removedPaths)
	require.Equal(t, store.staged.MaskPath, store.removedMask)
}

func TestImageStudioJobServiceCreateEditJobPreservesRepositoryAndRollbackFailures(t *testing.T) {
	repoFailure := errors.New("insert failed")
	rollbackFailure := inputStorageError(errors.New("cleanup failed"))
	store := &imageStudioCreateStorageStub{
		staged:    &StagedEditInputs{UploadID: "upload-test", ImagePaths: []string{"inputs/upload-test/image-01.png"}},
		removeErr: rollbackFailure,
	}
	svc := NewImageStudioJobService(&imageStudioJobDeleteRepoStub{createErr: repoFailure}, nil, store, time.Now)

	job, err := svc.CreateEditJob(context.Background(), validImageStudioEditCreateInput(), []UploadedFile{{
		Reader: bytes.NewReader([]byte("image")), ContentType: "image/png",
	}}, nil)

	require.Nil(t, job)
	require.ErrorIs(t, err, repoFailure)
	require.ErrorIs(t, err, rollbackFailure)
	require.ErrorIs(t, err, ErrImageStudioInputStorageUnavailable)
}

func TestImageStudioJobServiceCreateEditJobMapsStorageFailures(t *testing.T) {
	tests := []struct {
		name       string
		stageError error
		wantCode   int
	}{
		{name: "validation", stageError: inputInvalidError(ErrImageStudioInputInvalid), wantCode: 400},
		{name: "storage unavailable", stageError: inputStorageError(errors.New("disk offline")), wantCode: 503},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewImageStudioJobService(&imageStudioJobDeleteRepoStub{}, nil, &imageStudioCreateStorageStub{stageErr: tt.stageError}, time.Now)
			job, err := svc.CreateEditJob(context.Background(), validImageStudioEditCreateInput(), []UploadedFile{{
				Reader: bytes.NewReader([]byte("image")), ContentType: "image/png",
			}}, nil)
			require.Nil(t, job)
			require.Equal(t, tt.wantCode, infraerrors.Code(err))
		})
	}
}

func TestImageStudioJobServiceRejectsNewJobsBeforeRepositoryOrStorageWhenUnhealthy(t *testing.T) {
	prober := &imageStudioInputStorageProberStub{errors: []error{errors.New("mount unavailable")}}
	health := NewImageStudioInputStorageHealth(prober, time.Minute)
	require.Error(t, health.Probe(context.Background()))
	store := &imageStudioCreateStorageStub{staged: &StagedEditInputs{
		UploadID: "upload-test", ImagePaths: []string{"inputs/upload-test/image-01.png"},
	}}
	repo := &imageStudioJobDeleteRepoStub{}
	svc := NewImageStudioJobService(repo, nil, store, time.Now, health)

	generated, generateErr := svc.CreateJob(context.Background(), ImageStudioJobCreateInput{
		UserID: 7, APIKeyID: 8, Mode: ImageStudioJobModeGenerate,
		RequestPayload: json.RawMessage(`{"model":"gpt-image-2"}`),
	})
	edited, editErr := svc.CreateEditJob(context.Background(), validImageStudioEditCreateInput(), []UploadedFile{{
		Reader: strings.NewReader("image"), ContentType: "image/png",
	}}, nil)

	require.Nil(t, generated)
	require.Nil(t, edited)
	for _, err := range []error{generateErr, editErr} {
		require.Equal(t, 503, infraerrors.Code(err))
		require.ErrorIs(t, err, ErrImageStudioInputStorageUnavailable)
	}
	require.Zero(t, repo.createCalls)
	require.Empty(t, store.images)
}

func validImageStudioEditCreateInput() ImageStudioJobCreateInput {
	return ImageStudioJobCreateInput{
		UserID: 7, APIKeyID: 8, Mode: ImageStudioJobModeEdit, Prompt: "edit",
		Model: "gpt-image-2", OutputFormat: "png",
		RequestPayload: json.RawMessage(`{"model":"gpt-image-2","prompt":"edit","output_format":"png","response_format":"b64_json"}`),
	}
}

func TestImageStudioJobServiceDeleteJobRemovesAssetsAndRecord(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	dir := filepath.Join(dataDir, imageStudioAssetBaseDir, "44")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	originalPath := filepath.Join(dir, "original.png")
	thumbnailPath := filepath.Join(dir, "thumbnail.jpg")
	require.NoError(t, os.WriteFile(originalPath, []byte("original"), 0o600))
	require.NoError(t, os.WriteFile(thumbnailPath, []byte("thumbnail"), 0o600))
	inputPath := "inputs/upload-delete/image-01.png"
	events := make([]string, 0, 2)
	store := &imageStudioDeleteStorageStub{events: &events}

	repo := &imageStudioJobDeleteRepoStub{
		job: &ImageStudioJob{
			ID:              44,
			UserID:          7,
			InputImagePaths: []string{inputPath},
			OriginalPath:    originalPath,
			ThumbnailPath:   thumbnailPath,
		},
		events:            &events,
		pathsExpectedGone: []string{originalPath, thumbnailPath},
	}
	svc := NewImageStudioJobService(repo, nil, store, time.Now)

	err := svc.DeleteJob(context.Background(), 7, 44)

	require.NoError(t, err)
	require.Equal(t, int64(44), repo.deletedID)
	require.Equal(t, int64(7), repo.deletedUserID)
	require.Equal(t, []string{inputPath}, store.removedPaths)
	require.NoFileExists(t, originalPath)
	require.NoFileExists(t, thumbnailPath)
	require.NoDirExists(t, dir)
	require.Equal(t, []string{"inputs", "row"}, events)
}

func TestImageStudioJobServiceDeleteJobDoesNotDeleteRecordWhenAssetRemovalFails(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	outside := filepath.Join(t.TempDir(), "must-remain.png")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o600))

	repo := &imageStudioJobDeleteRepoStub{
		job: &ImageStudioJob{
			ID:           44,
			UserID:       7,
			OriginalPath: outside,
		},
	}
	svc := NewImageStudioJobService(repo, nil, nil, time.Now)

	err := svc.DeleteJob(context.Background(), 7, 44)

	require.Error(t, err)
	require.Zero(t, repo.deletedID)
	require.FileExists(t, outside, "database path pollution must not delete outside the job output directory")
}

func TestImageStudioJobServiceDeleteJobKeepsRowWhenInputRemovalFails(t *testing.T) {
	repo := &imageStudioJobDeleteRepoStub{job: &ImageStudioJob{
		ID: 44, UserID: 7, InputImagePaths: []string{"inputs/upload-delete/image-01.png"},
	}}
	store := &imageStudioDeleteStorageStub{removeErr: errors.New("input io failed")}
	svc := NewImageStudioJobService(repo, nil, store, time.Now)

	err := svc.DeleteJob(context.Background(), 7, 44)

	require.ErrorContains(t, err, "input io failed")
	require.Zero(t, repo.deletedID)
}

func TestImageStudioJobServiceDeleteJobRejectsRunningOwnership(t *testing.T) {
	repo := &imageStudioJobDeleteRepoStub{claimErr: ErrImageStudioJobBusy}
	store := &imageStudioDeleteStorageStub{}
	svc := NewImageStudioJobService(repo, nil, store, time.Now)

	err := svc.DeleteJob(context.Background(), 7, 44)

	require.ErrorIs(t, err, ErrImageStudioJobBusy)
	require.Empty(t, store.removedPaths)
	require.Zero(t, repo.deletedID)
}

func TestImageStudioJobServiceCleanupInputsIsBoundedAndIsolatesFailures(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	repo := &imageStudioLifecycleRepoStub{
		expiredQueued: []ImageStudioJob{{ID: 1, InputImagePaths: []string{"inputs/upload-one/image-01.png"}}, {ID: 2, InputImagePaths: []string{"inputs/upload-two/image-01.png"}}},
		expiredTerminal: []ImageStudioJob{
			{ID: 3, Status: ImageStudioJobStatusFailed, InputImagePaths: []string{"inputs/upload-three/image-01.png"}},
			{ID: 4, Status: ImageStudioJobStatusRunning, InputImagePaths: []string{"inputs/upload-running/image-01.png"}},
		},
	}
	store := &imageStudioLifecycleStorageStub{removeErrByDir: map[string]error{"upload-one": errors.New("first remove failed")}}
	svc := NewImageStudioJobService(repo, nil, store, func() time.Time { return now })

	svc.cleanupExpiredInputs(context.Background())

	require.Equal(t, 50, repo.expireLimit)
	require.Equal(t, 50, repo.listLimit)
	require.Equal(t, []int64{2, 3}, repo.markedInputIDs, "a single filesystem failure must not stop the batch")
	require.Equal(t, []string{"upload-one", "upload-two", "upload-three"}, store.removedDirs)
}

func TestImageStudioJobServiceCleanupSkipsOrphansWhenReferenceQueryFails(t *testing.T) {
	repo := &imageStudioLifecycleRepoStub{referencesErr: errors.New("database unavailable")}
	store := &imageStudioLifecycleStorageStub{}
	svc := NewImageStudioJobService(repo, nil, store, time.Now)

	svc.cleanupExpiredInputs(context.Background())

	require.Zero(t, store.cleanupCalls)
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

type imageStudioCreateStorageStub struct {
	staged       *StagedEditInputs
	stageErr     error
	removeErr    error
	images       []UploadedFile
	mask         *UploadedFile
	removedPaths []string
	removedMask  *string
}

func (s *imageStudioCreateStorageStub) StageEditInputs(_ context.Context, images []UploadedFile, mask *UploadedFile) (*StagedEditInputs, error) {
	s.images = append([]UploadedFile(nil), images...)
	s.mask = mask
	return s.staged, s.stageErr
}

func (s *imageStudioCreateStorageStub) MaterializeLegacy(context.Context, []string, *string) (*StagedEditInputs, error) {
	panic("unexpected MaterializeLegacy call")
}

func (s *imageStudioCreateStorageStub) OpenInputs([]string, *string) (*OpenedEditInputs, error) {
	panic("unexpected OpenInputs call")
}

func (s *imageStudioCreateStorageStub) RemoveInputs(paths []string, mask *string) error {
	s.removedPaths = append([]string(nil), paths...)
	s.removedMask = mask
	return s.removeErr
}

type imageStudioDeleteStorageStub struct {
	ImageStudioInputStorage
	removeErr    error
	removedPaths []string
	events       *[]string
}

func (s *imageStudioDeleteStorageStub) RemoveInputs(paths []string, _ *string) error {
	s.removedPaths = append([]string(nil), paths...)
	if s.events != nil {
		*s.events = append(*s.events, "inputs")
	}
	return s.removeErr
}

type imageStudioLifecycleRepoStub struct {
	ImageStudioJobRepository
	expiredQueued   []ImageStudioJob
	expiredTerminal []ImageStudioJob
	references      map[string]struct{}
	running         map[string]struct{}
	referencesErr   error
	runningErr      error
	expireLimit     int
	listLimit       int
	markedInputIDs  []int64
}

func (r *imageStudioLifecycleRepoStub) ExpireQueuedInputs(_ context.Context, _ time.Time, limit int) ([]ImageStudioJob, error) {
	r.expireLimit = limit
	return r.expiredQueued, nil
}

func (r *imageStudioLifecycleRepoStub) ListExpiredInputs(_ context.Context, _ time.Time, limit int) ([]ImageStudioJob, error) {
	r.listLimit = limit
	return r.expiredTerminal, nil
}

func (r *imageStudioLifecycleRepoStub) MarkInputsDeleted(_ context.Context, id int64, _ time.Time) error {
	r.markedInputIDs = append(r.markedInputIDs, id)
	return nil
}

func (r *imageStudioLifecycleRepoStub) ListReferencedInputDirs(context.Context) (map[string]struct{}, error) {
	return r.references, r.referencesErr
}

func (r *imageStudioLifecycleRepoStub) ListRunningInputDirs(context.Context) (map[string]struct{}, error) {
	return r.running, r.runningErr
}

type imageStudioLifecycleStorageStub struct {
	ImageStudioInputStorage
	removeErrByDir map[string]error
	removedDirs    []string
	cleanupCalls   int
}

func (s *imageStudioLifecycleStorageStub) RemoveInputs(paths []string, _ *string) error {
	dir := ""
	if len(paths) > 0 {
		dir = strings.Split(paths[0], "/")[1]
	}
	s.removedDirs = append(s.removedDirs, dir)
	return s.removeErrByDir[dir]
}

func (s *imageStudioLifecycleStorageStub) CleanupOrphans(ImageStudioInputCleanupOptions) (ImageStudioInputCleanupResult, error) {
	s.cleanupCalls++
	return ImageStudioInputCleanupResult{}, nil
}

type imageStudioCreateSettingRepoStub struct {
	values map[string]string
}

func (r *imageStudioCreateSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}

func (r *imageStudioCreateSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", ErrSettingNotFound
}

func (r *imageStudioCreateSettingRepoStub) Set(context.Context, string, string) error { return nil }

func (r *imageStudioCreateSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (r *imageStudioCreateSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	return nil
}

func (r *imageStudioCreateSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	return r.values, nil
}

func (r *imageStudioCreateSettingRepoStub) Delete(context.Context, string) error { return nil }

type imageStudioJobDeleteRepoStub struct {
	job               *ImageStudioJob
	deletedID         int64
	deletedUserID     int64
	createErr         error
	lastCreateInput   ImageStudioJobCreateInput
	events            *[]string
	pathsExpectedGone []string
	claimErr          error
	createCalls       int
}

func (r *imageStudioJobDeleteRepoStub) Create(_ context.Context, input ImageStudioJobCreateInput) (*ImageStudioJob, error) {
	r.createCalls++
	r.lastCreateInput = input
	if r.createErr != nil {
		return nil, r.createErr
	}
	return &ImageStudioJob{ID: 1, Mode: input.Mode, RequestPayload: input.RequestPayload}, nil
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

func (r *imageStudioJobDeleteRepoStub) ClaimDeletingByIDForUser(ctx context.Context, id, userID int64) (*ImageStudioJob, error) {
	if r.claimErr != nil {
		return nil, r.claimErr
	}
	return r.GetByIDForUser(ctx, id, userID)
}

func (r *imageStudioJobDeleteRepoStub) ListByUser(context.Context, int64, int, int) (*ImageStudioJobList, error) {
	panic("unexpected ListByUser call")
}

func (r *imageStudioJobDeleteRepoStub) CountStatusByUser(context.Context, int64) (*ImageStudioJobStats, error) {
	panic("unexpected CountStatusByUser call")
}

func (r *imageStudioJobDeleteRepoStub) DeleteByIDForUser(_ context.Context, id, userID int64) error {
	for _, path := range r.pathsExpectedGone {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("asset still exists before row delete: %s", filepath.Base(path))
		}
	}
	if r.events != nil {
		*r.events = append(*r.events, "row")
	}
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

func (r *imageStudioJobDeleteRepoStub) PersistLegacyInputs(context.Context, int64, []string, *string, json.RawMessage, time.Time) error {
	panic("unexpected PersistLegacyInputs call")
}

func (r *imageStudioJobDeleteRepoStub) FailLegacyInputs(context.Context, int64, json.RawMessage, time.Time) error {
	panic("unexpected FailLegacyInputs call")
}

func (r *imageStudioJobDeleteRepoStub) ExpireQueuedInputs(context.Context, time.Time, int) ([]ImageStudioJob, error) {
	panic("unexpected ExpireQueuedInputs call")
}

func (r *imageStudioJobDeleteRepoStub) ListExpiredInputs(context.Context, time.Time, int) ([]ImageStudioJob, error) {
	panic("unexpected ListExpiredInputs call")
}

func (r *imageStudioJobDeleteRepoStub) MarkInputsDeleted(context.Context, int64, time.Time) error {
	panic("unexpected MarkInputsDeleted call")
}

func (r *imageStudioJobDeleteRepoStub) FailExpiredRunningInputs(context.Context, int64, time.Time) (bool, error) {
	panic("unexpected FailExpiredRunningInputs call")
}

func (r *imageStudioJobDeleteRepoStub) ListReferencedInputDirs(context.Context) (map[string]struct{}, error) {
	panic("unexpected ListReferencedInputDirs call")
}

func (r *imageStudioJobDeleteRepoStub) ListRunningInputDirs(context.Context) (map[string]struct{}, error) {
	panic("unexpected ListRunningInputDirs call")
}

func (r *imageStudioJobDeleteRepoStub) MarkStaleRunningFailed(context.Context, int64, time.Time, time.Time) (bool, error) {
	panic("unexpected MarkStaleRunningFailed call")
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

func (r *imageStudioJobDeleteRepoStub) MarkSettlementFailed(context.Context, int64, time.Time, string, string) (bool, error) {
	panic("unexpected MarkSettlementFailed call")
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
