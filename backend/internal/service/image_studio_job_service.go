package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	defaultImageStudioAsyncConcurrency = 2
	defaultImageStudioRetentionValue   = 0
	defaultImageStudioRetentionUnit    = ImageStudioRetentionUnitDay
	imageStudioWorkerTick              = 2 * time.Second
	imageStudioCleanupTick             = 30 * time.Minute
	imageStudioAssetBaseDir            = "image-studio"
	imageStudioInputOrphanGrace        = time.Hour
	imageStudioMultipartSpoolGrace     = 10 * time.Minute
)

type ImageStudioJobService struct {
	repo                 ImageStudioJobRepository
	settingService       *SettingService
	inputStore           ImageStudioInputStorage
	executor             imageStudioJobExecutor
	openAIGateway        *OpenAIGatewayService
	apiKeyService        *APIKeyService
	billingCacheService  *BillingCacheService
	subscriptionResolver imageStudioSubscriptionResolver
	stopCh               chan struct{}
	stopOnce             sync.Once
	wg                   sync.WaitGroup
	running              int32
	now                  func() time.Time
	syncAssetFile        func(*os.File) error
	renameAssetFile      func(string, string) error
}

type imageStudioSubscriptionResolver interface {
	GetByID(ctx context.Context, id int64) (*UserSubscription, error)
	GetActiveSubscription(ctx context.Context, userID, groupID int64) (*UserSubscription, error)
}

func NewImageStudioJobService(repo ImageStudioJobRepository, settingService *SettingService, inputStore ImageStudioInputStorage, now func() time.Time) *ImageStudioJobService {
	if now == nil {
		now = time.Now
	}
	return &ImageStudioJobService{
		repo:           repo,
		settingService: settingService,
		inputStore:     inputStore,
		stopCh:         make(chan struct{}),
		now:            now,
	}
}

func (s *ImageStudioJobService) Start() {
	if s == nil || s.repo == nil {
		return
	}
	s.wg.Add(2)
	go s.runQueueLoop()
	go s.runCleanupLoop()
}

func (s *ImageStudioJobService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *ImageStudioJobService) CreateJob(ctx context.Context, input ImageStudioJobCreateInput) (*ImageStudioJob, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("image studio job service is not configured")
	}
	if input.UserID <= 0 || input.APIKeyID <= 0 {
		return nil, fmt.Errorf("user_id and api_key_id are required")
	}
	if strings.TrimSpace(input.Mode) == "" {
		input.Mode = ImageStudioJobModeGenerate
	}
	if strings.TrimSpace(input.OutputFormat) == "" {
		input.OutputFormat = "png"
	}
	if !json.Valid(input.RequestPayload) {
		return nil, fmt.Errorf("request payload must be valid json")
	}
	return s.repo.Create(ctx, input)
}

func (s *ImageStudioJobService) CreateEditJob(ctx context.Context, input ImageStudioJobCreateInput, images []UploadedFile, mask *UploadedFile) (*ImageStudioJob, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("image studio job service is not configured")
	}
	if input.UserID <= 0 || input.APIKeyID <= 0 {
		return nil, infraerrors.BadRequest("IMAGE_STUDIO_JOB_INVALID", "user_id and api_key_id are required")
	}
	if strings.TrimSpace(input.Mode) != ImageStudioJobModeEdit {
		return nil, infraerrors.BadRequest("IMAGE_STUDIO_JOB_INVALID", "multipart image studio jobs must use edit mode")
	}
	if strings.TrimSpace(input.OutputFormat) == "" {
		input.OutputFormat = "png"
	}
	if s.inputStore == nil {
		return nil, imageStudioInputCreateError(inputStorageError(errors.New("image studio input store is not configured")))
	}

	staged, err := s.inputStore.StageEditInputs(ctx, images, mask)
	if err != nil {
		return nil, imageStudioInputCreateError(err)
	}
	rollback := func(cause error) error {
		if cleanupErr := s.inputStore.RemoveInputs(staged.ImagePaths, staged.MaskPath); cleanupErr != nil {
			return errors.Join(cause, cleanupErr)
		}
		return cause
	}

	sanitized, err := sanitizeImageStudioEditPayload(input.RequestPayload)
	if err != nil {
		return nil, rollback(infraerrors.BadRequest("IMAGE_STUDIO_JOB_INVALID", "image studio edit metadata is invalid").WithCause(err))
	}
	now := s.now()
	inputExpiresAt := now.Add(time.Duration(s.InputRetentionHours(ctx)) * time.Hour)
	input.RequestPayload = sanitized
	input.InputImagePaths = append([]string(nil), staged.ImagePaths...)
	input.InputMaskPath = cloneImageStudioString(staged.MaskPath)
	input.InputExpiresAt = &inputExpiresAt
	input.InputDeletedAt = nil

	job, err := s.repo.Create(ctx, input)
	if err != nil {
		return nil, rollback(err)
	}
	return job, nil
}

func (s *ImageStudioJobService) InputRetentionHours(ctx context.Context) int {
	if s == nil || s.settingService == nil {
		return DefaultImageStudioInputRetentionHours
	}
	settings, err := s.settingService.GetAllSettings(ctx)
	if err != nil || settings == nil || settings.ImageStudioInputRetentionHours <= 0 {
		return DefaultImageStudioInputRetentionHours
	}
	return settings.ImageStudioInputRetentionHours
}

func sanitizeImageStudioEditPayload(raw json.RawMessage) (json.RawMessage, error) {
	if !json.Valid(raw) {
		return nil, fmt.Errorf("request payload must be valid json")
	}
	var source map[string]json.RawMessage
	if err := json.Unmarshal(raw, &source); err != nil || source == nil {
		return nil, fmt.Errorf("request payload must be a json object")
	}
	allowedStrings := []string{
		"model", "prompt", "size", "quality", "background", "style", "moderation",
		"input_fidelity", "output_format", "response_format",
	}
	sanitized := make(map[string]any, len(allowedStrings)+1)
	for _, key := range allowedStrings {
		value, ok := source[key]
		if !ok {
			continue
		}
		var scalar string
		if err := json.Unmarshal(value, &scalar); err != nil {
			return nil, fmt.Errorf("%s must be a string", key)
		}
		sanitized[key] = scalar
	}
	if value, ok := source["output_compression"]; ok {
		var scalar int
		if err := json.Unmarshal(value, &scalar); err != nil {
			return nil, fmt.Errorf("output_compression must be an integer")
		}
		sanitized["output_compression"] = scalar
	}
	return json.Marshal(sanitized)
}

func imageStudioInputCreateError(err error) error {
	if errors.Is(err, ErrImageStudioInputStorageUnavailable) {
		return infraerrors.ServiceUnavailable(
			"IMAGE_STUDIO_INPUT_STORAGE_UNAVAILABLE",
			"image studio input storage is unavailable",
		).WithCause(err)
	}
	return infraerrors.BadRequest("IMAGE_STUDIO_INPUT_INVALID", "image studio edit input is invalid").WithCause(err)
}

func cloneImageStudioString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func (s *ImageStudioJobService) ListJobs(ctx context.Context, userID int64, page, pageSize int) (*ImageStudioJobList, error) {
	return s.repo.ListByUser(ctx, userID, page, pageSize)
}

func (s *ImageStudioJobService) JobStats(ctx context.Context, userID int64) (*ImageStudioJobStats, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("image studio job service is not configured")
	}
	return s.repo.CountStatusByUser(ctx, userID)
}

func (s *ImageStudioJobService) GetJob(ctx context.Context, userID, id int64) (*ImageStudioJob, error) {
	return s.repo.GetByIDForUser(ctx, id, userID)
}

func (s *ImageStudioJobService) DeleteJob(ctx context.Context, userID, id int64) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("image studio job service is not configured")
	}
	job, err := s.repo.ClaimDeletingByIDForUser(ctx, id, userID)
	if err != nil {
		return err
	}
	if len(job.InputImagePaths) != 0 || job.InputMaskPath != nil {
		if s.inputStore == nil {
			return fmt.Errorf("image studio input store is not configured")
		}
		if err := s.inputStore.RemoveInputs(job.InputImagePaths, job.InputMaskPath); err != nil {
			return fmt.Errorf("remove image studio job inputs: %w", err)
		}
	}
	if err := s.removeImageStudioJobAssets(*job); err != nil {
		return err
	}
	return s.repo.DeleteByIDForUser(ctx, id, userID)
}

func (s *ImageStudioJobService) ResolveRetention(now time.Time) *time.Time {
	value, unit := s.AsyncRetentionSettings(context.Background())
	if value <= 0 {
		return nil
	}
	var expiresAt time.Time
	switch unit {
	case ImageStudioRetentionUnitHour:
		expiresAt = now.Add(time.Duration(value) * time.Hour)
	default:
		expiresAt = now.Add(time.Duration(value) * 24 * time.Hour)
	}
	return &expiresAt
}

func (s *ImageStudioJobService) AsyncConcurrency(ctx context.Context) int {
	if s == nil || s.settingService == nil {
		return defaultImageStudioAsyncConcurrency
	}
	settings, err := s.settingService.GetAllSettings(ctx)
	if err != nil || settings == nil || settings.ImageStudioAsyncConcurrency <= 0 {
		return defaultImageStudioAsyncConcurrency
	}
	return settings.ImageStudioAsyncConcurrency
}

func (s *ImageStudioJobService) AsyncRetentionSettings(ctx context.Context) (int, string) {
	if s == nil || s.settingService == nil {
		return defaultImageStudioRetentionValue, defaultImageStudioRetentionUnit
	}
	settings, err := s.settingService.GetAllSettings(ctx)
	if err != nil || settings == nil {
		return defaultImageStudioRetentionValue, defaultImageStudioRetentionUnit
	}
	value := settings.ImageStudioRetentionValue
	unit := normalizeImageStudioRetentionUnit(settings.ImageStudioRetentionUnit)
	if value < 0 {
		value = 0
	}
	return value, unit
}

func (s *ImageStudioJobService) AssetBaseDir() string {
	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		dataDir = "/app/data"
	}
	return filepath.Join(dataDir, imageStudioAssetBaseDir)
}

func (s *ImageStudioJobService) EstimateCost(ctx context.Context, apiKey *APIKey, model, size string) (*CostBreakdown, error) {
	if s == nil || s.openAIGateway == nil || s.openAIGateway.billingService == nil {
		return nil, fmt.Errorf("openai gateway billing is not configured")
	}
	if apiKey == nil || apiKey.User == nil || apiKey.Group == nil {
		return nil, fmt.Errorf("api key billing context is incomplete")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, fmt.Errorf("model is required")
	}
	_, imageMultiplier, _, _ := s.openAIGateway.resolveOpenAIUsageMultipliers(ctx, apiKey.User, apiKey)
	result := &OpenAIForwardResult{
		Model:      model,
		ImageCount: 1,
		ImageSize:  NormalizeImageBillingTierOrDefault(size),
	}
	return s.openAIGateway.calculateOpenAIImageCost(ctx, model, apiKey, result, imageMultiplier), nil
}

func (s *ImageStudioJobService) runQueueLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(imageStudioWorkerTick)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.drainQueueOnce(context.Background())
		}
	}
}

func (s *ImageStudioJobService) runCleanupLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(imageStudioCleanupTick)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanupExpiredInputs(context.Background())
			s.cleanupExpiredAssets(context.Background())
		}
	}
}

func (s *ImageStudioJobService) cleanupExpiredInputs(ctx context.Context) {
	if s == nil || s.repo == nil || s.inputStore == nil {
		return
	}
	now := s.now()
	if queued, err := s.repo.ExpireQueuedInputs(ctx, now, 50); err != nil {
		slog.Warn("image_studio_input_cleanup_query_failed", "phase", "expire_queued", "error_kind", imageStudioInputCleanupLogValue(err))
	} else {
		s.cleanupImageStudioInputJobs(ctx, queued, now)
	}
	if terminal, err := s.repo.ListExpiredInputs(ctx, now, 50); err != nil {
		slog.Warn("image_studio_input_cleanup_query_failed", "phase", "list_terminal", "error_kind", imageStudioInputCleanupLogValue(err))
	} else {
		s.cleanupImageStudioInputJobs(ctx, terminal, now)
	}

	cleaner, ok := s.inputStore.(imageStudioInputOrphanCleaner)
	if !ok || cleaner == nil {
		return
	}
	referenced, err := s.repo.ListReferencedInputDirs(ctx)
	if err != nil {
		slog.Warn("image_studio_input_cleanup_query_failed", "phase", "list_references", "error_kind", imageStudioInputCleanupLogValue(err))
		return
	}
	runningRepo, ok := s.repo.(interface {
		ListRunningInputDirs(context.Context) (map[string]struct{}, error)
	})
	if !ok {
		return
	}
	running, err := runningRepo.ListRunningInputDirs(ctx)
	if err != nil {
		slog.Warn("image_studio_input_cleanup_query_failed", "phase", "list_running", "error_kind", imageStudioInputCleanupLogValue(err))
		return
	}
	result, err := cleaner.CleanupOrphans(ImageStudioInputCleanupOptions{
		Now: now, OrphanGrace: imageStudioInputOrphanGrace, SpoolGrace: imageStudioMultipartSpoolGrace,
		Limit: 50, ReferencedDirs: referenced, RunningDirs: running,
	})
	if err != nil {
		slog.Warn("image_studio_input_orphan_cleanup_failed", "scanned", result.Scanned, "orphan_dirs_deleted", result.OrphanDirsDeleted, "stale_spools_deleted", result.StaleSpoolsDeleted, "error_kind", imageStudioInputCleanupLogValue(err))
	}
}

func (s *ImageStudioJobService) cleanupImageStudioInputJobs(ctx context.Context, jobs []ImageStudioJob, deletedAt time.Time) {
	for _, job := range jobs {
		if job.Status == ImageStudioJobStatusRunning {
			continue
		}
		if len(job.InputImagePaths) != 0 || job.InputMaskPath != nil {
			if err := s.inputStore.RemoveInputs(job.InputImagePaths, job.InputMaskPath); err != nil {
				slog.Warn("image_studio_input_cleanup_failed", "job_id", job.ID, "stage", "remove_expired", "error_kind", imageStudioInputCleanupLogValue(err))
				continue
			}
		}
		if err := s.repo.MarkInputsDeleted(ctx, job.ID, deletedAt); err != nil {
			slog.Warn("image_studio_input_cleanup_failed", "job_id", job.ID, "stage", "mark_expired_deleted", "error_kind", imageStudioInputCleanupLogValue(err))
		}
	}
}

func (s *ImageStudioJobService) removeImageStudioJobAssets(job ImageStudioJob) error {
	baseDir, err := filepath.Abs(s.AssetBaseDir())
	if err != nil {
		return fmt.Errorf("resolve image studio asset root: %w", err)
	}
	jobDir := filepath.Join(baseDir, strconv.FormatInt(job.ID, 10))
	for _, assetPath := range []string{job.OriginalPath, job.ThumbnailPath} {
		assetPath = strings.TrimSpace(assetPath)
		if assetPath == "" {
			continue
		}
		resolved, err := filepath.Abs(assetPath)
		if err != nil || filepath.Dir(resolved) != jobDir {
			return fmt.Errorf("image studio asset path is outside the job directory")
		}
	}
	root, err := os.OpenRoot(baseDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open image studio asset root: %w", err)
	}
	defer root.Close()
	relativeDir := strconv.FormatInt(job.ID, 10)
	info, err := root.Lstat(relativeDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect image studio job assets: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("image studio job asset directory is unsafe")
	}
	if err := root.RemoveAll(relativeDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove image studio job assets: %w", err)
	}
	return nil
}

func (s *ImageStudioJobService) cleanupExpiredAssets(ctx context.Context) {
	if s == nil || s.repo == nil {
		return
	}
	items, err := s.repo.ListExpiredAssets(ctx, time.Now(), 50)
	if err != nil {
		return
	}
	for _, item := range items {
		if strings.TrimSpace(item.OriginalPath) != "" {
			_ = os.Remove(item.OriginalPath)
		}
		if strings.TrimSpace(item.ThumbnailPath) != "" {
			_ = os.Remove(item.ThumbnailPath)
		}
		_ = s.repo.MarkAssetsDeleted(ctx, item.ID, time.Now())
	}
}

func removeImageStudioAsset(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func normalizeImageStudioRetentionUnit(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ImageStudioRetentionUnitHour:
		return ImageStudioRetentionUnitHour
	default:
		return ImageStudioRetentionUnitDay
	}
}

func NormalizeImageStudioRetentionUnitForWrite(value string) string {
	return normalizeImageStudioRetentionUnit(value)
}
