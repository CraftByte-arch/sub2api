package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	job, err := s.repo.GetByIDForUser(ctx, id, userID)
	if err != nil {
		return err
	}
	if err := removeImageStudioAsset(job.OriginalPath); err != nil {
		return err
	}
	if err := removeImageStudioAsset(job.ThumbnailPath); err != nil {
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
			s.cleanupExpiredAssets(context.Background())
		}
	}
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
