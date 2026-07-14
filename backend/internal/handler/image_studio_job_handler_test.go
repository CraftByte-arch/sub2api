package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBuildImageStudioJobPayload_UsesImagesGenerationFormat(t *testing.T) {
	payload, err := buildImageStudioJobPayload(imageStudioCreateJobRequest{
		Prompt:       "draw a red fox",
		Model:        "gpt-image-2",
		Size:         "1024x1024",
		OutputFormat: "webp",
		Quality:      "high",
		Background:   "transparent",
		Moderation:   "low",
	}, service.ImageStudioJobModeGenerate)
	require.NoError(t, err)
	require.True(t, json.Valid(payload))

	require.Equal(t, "gpt-image-2", gjson.GetBytes(payload, "model").String())
	require.Equal(t, "draw a red fox", gjson.GetBytes(payload, "prompt").String())
	require.Equal(t, "1024x1024", gjson.GetBytes(payload, "size").String())
	require.Equal(t, "b64_json", gjson.GetBytes(payload, "response_format").String())
	require.Equal(t, "webp", gjson.GetBytes(payload, "output_format").String())
	require.Equal(t, "high", gjson.GetBytes(payload, "quality").String())
	require.Equal(t, "transparent", gjson.GetBytes(payload, "background").String())
	require.Equal(t, "low", gjson.GetBytes(payload, "moderation").String())
	require.False(t, gjson.GetBytes(payload, "input").Exists())
	require.False(t, gjson.GetBytes(payload, "tools").Exists())
	require.False(t, gjson.GetBytes(payload, "tool_choice").Exists())
}

func TestImageStudioJobResponseDoesNotExposeInputPaths(t *testing.T) {
	maskPath := "inputs/private-upload/mask.png"
	expiresAt := time.Now().Add(24 * time.Hour)
	deletedAt := time.Now()
	handler := &ImageStudioJobHandler{}

	raw, err := json.Marshal(handler.toJobResponse(&service.ImageStudioJob{
		ID:              39,
		InputImagePaths: []string{"inputs/private-upload/image-01.webp"},
		InputMaskPath:   &maskPath,
		InputExpiresAt:  &expiresAt,
		InputDeletedAt:  &deletedAt,
	}))

	require.NoError(t, err)
	require.NotContains(t, string(raw), "input_image_paths")
	require.NotContains(t, string(raw), "input_mask_path")
	require.NotContains(t, string(raw), "input_expires_at")
	require.NotContains(t, string(raw), "input_deleted_at")
	require.NotContains(t, string(raw), "private-upload")
}

func TestBuildImageStudioJobPayload_UsesImagesEditFormat(t *testing.T) {
	payload, err := buildImageStudioJobPayload(imageStudioCreateJobRequest{
		Prompt:        "replace the sky",
		Model:         "gpt-image-2",
		OutputFormat:  "png",
		ImageDataURLs: []string{"data:image/png;base64,aW1hZ2U="},
		MaskDataURL:   "data:image/png;base64,bWFzaw==",
	}, service.ImageStudioJobModeEdit)
	require.NoError(t, err)
	require.True(t, json.Valid(payload))

	require.Equal(t, "gpt-image-2", gjson.GetBytes(payload, "model").String())
	require.Equal(t, "replace the sky", gjson.GetBytes(payload, "prompt").String())
	require.Equal(t, "data:image/png;base64,aW1hZ2U=", gjson.GetBytes(payload, "images.0.image_url").String())
	require.Equal(t, "data:image/png;base64,bWFzaw==", gjson.GetBytes(payload, "mask.image_url").String())
	require.Equal(t, "b64_json", gjson.GetBytes(payload, "response_format").String())
	require.Equal(t, "png", gjson.GetBytes(payload, "output_format").String())
	require.False(t, gjson.GetBytes(payload, "input").Exists())
	require.False(t, gjson.GetBytes(payload, "tools").Exists())
	require.False(t, gjson.GetBytes(payload, "tool_choice").Exists())
}

func TestImageStudioJobHandler_CreateEnforcesAvailableGroups(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		allowedGroups string
		wantStatus    int
		wantCreated   bool
	}{
		{
			name:          "allowed group creates job",
			allowedGroups: `[42]`,
			wantStatus:    http.StatusOK,
			wantCreated:   true,
		},
		{
			name:          "empty allowlist rejects",
			allowedGroups: `[]`,
			wantStatus:    http.StatusForbidden,
			wantCreated:   false,
		},
		{
			name:          "missing group rejects",
			allowedGroups: `[7,8]`,
			wantStatus:    http.StatusForbidden,
			wantCreated:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Default.RateMultiplier = 1
			groupID := int64(42)
			apiKeyRepo := &imageStudioHandlerAPIKeyRepoStub{apiKey: &service.APIKey{
				ID:      1001,
				UserID:  123,
				Key:     "sk-user",
				Status:  service.StatusActive,
				GroupID: &groupID,
				Group: &service.Group{
					ID:                   groupID,
					AllowImageGeneration: true,
					RateMultiplier:       1,
					ImageRateMultiplier:  1,
				},
				User: &service.User{ID: 123},
			}}
			jobRepo := &imageStudioHandlerJobRepoStub{}
			settingSvc := service.NewSettingService(&imageStudioHandlerSettingRepoStub{values: map[string]string{
				service.SettingKeyImageStudioAvailableGroupIDs: tt.allowedGroups,
			}}, &config.Config{})
			jobService := service.NewImageStudioJobService(jobRepo, settingSvc)
			openAIGateway := service.NewOpenAIGatewayService(
				nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
				service.NewBillingService(cfg, nil), nil, &service.BillingCacheService{}, nil,
				&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
			)
			jobService.SetRuntimeDependencies(openAIGateway, nil, nil, nil)
			handler := NewImageStudioJobHandler(
				jobService,
				service.NewAPIKeyService(apiKeyRepo, nil, nil, nil, nil, nil, &config.Config{}),
			)

			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/image-studio/jobs", bytes.NewReader([]byte(`{
				"api_key_id":1001,
				"mode":"generate",
				"prompt":"draw a cat",
				"model":"gpt-image-2",
				"size":"1024x1024",
				"output_format":"png"
			}`)))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 123})

			handler.Create(c)

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCreated, jobRepo.created)
			if tt.wantCreated {
				require.Positive(t, jobRepo.lastInput.EstimatedCostUSD)
			}
			if !tt.wantCreated {
				require.Contains(t, rec.Body.String(), "API key group is not available for image studio")
			}
		})
	}
}

type imageStudioHandlerSettingRepoStub struct {
	values map[string]string
}

func (r *imageStudioHandlerSettingRepoStub) Get(ctx context.Context, key string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}

func (r *imageStudioHandlerSettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if v, ok := r.values[key]; ok {
		return v, nil
	}
	return "", service.ErrSettingNotFound
}

func (r *imageStudioHandlerSettingRepoStub) Set(ctx context.Context, key, value string) error {
	return nil
}

func (r *imageStudioHandlerSettingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if v, ok := r.values[key]; ok {
			out[key] = v
		}
	}
	return out, nil
}

func (r *imageStudioHandlerSettingRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	return nil
}

func (r *imageStudioHandlerSettingRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	return r.values, nil
}

func (r *imageStudioHandlerSettingRepoStub) Delete(ctx context.Context, key string) error {
	return nil
}

type imageStudioHandlerJobRepoStub struct {
	created   bool
	lastInput service.ImageStudioJobCreateInput
}

func (r *imageStudioHandlerJobRepoStub) Create(ctx context.Context, input service.ImageStudioJobCreateInput) (*service.ImageStudioJob, error) {
	r.created = true
	r.lastInput = input
	now := time.Now()
	return &service.ImageStudioJob{
		ID:               9001,
		UserID:           input.UserID,
		APIKeyID:         input.APIKeyID,
		Mode:             input.Mode,
		Status:           service.ImageStudioJobStatusQueued,
		Prompt:           input.Prompt,
		Model:            input.Model,
		Size:             input.Size,
		OutputFormat:     input.OutputFormat,
		EstimatedCostUSD: input.EstimatedCostUSD,
		QueuedAt:         now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}

func (r *imageStudioHandlerJobRepoStub) GetByID(ctx context.Context, id int64) (*service.ImageStudioJob, error) {
	panic("unexpected GetByID call")
}
func (r *imageStudioHandlerJobRepoStub) GetByIDForUser(ctx context.Context, id, userID int64) (*service.ImageStudioJob, error) {
	panic("unexpected GetByIDForUser call")
}
func (r *imageStudioHandlerJobRepoStub) ListByUser(ctx context.Context, userID int64, page, pageSize int) (*service.ImageStudioJobList, error) {
	panic("unexpected ListByUser call")
}
func (r *imageStudioHandlerJobRepoStub) CountStatusByUser(ctx context.Context, userID int64) (*service.ImageStudioJobStats, error) {
	panic("unexpected CountStatusByUser call")
}
func (r *imageStudioHandlerJobRepoStub) DeleteByIDForUser(ctx context.Context, id, userID int64) error {
	panic("unexpected DeleteByIDForUser call")
}
func (r *imageStudioHandlerJobRepoStub) ListRunnableJobs(ctx context.Context, limit int) ([]service.ImageStudioJob, error) {
	panic("unexpected ListRunnableJobs call")
}
func (r *imageStudioHandlerJobRepoStub) MarkRunning(ctx context.Context, id int64, startedAt time.Time) (bool, error) {
	panic("unexpected MarkRunning call")
}
func (r *imageStudioHandlerJobRepoStub) MarkStaleRunningFailed(context.Context, int64, time.Time, time.Time) (bool, error) {
	panic("unexpected MarkStaleRunningFailed call")
}
func (r *imageStudioHandlerJobRepoStub) MarkSettling(context.Context, int64, json.RawMessage, string, string, string, int64, int, int, time.Time) error {
	panic("unexpected MarkSettling call")
}
func (r *imageStudioHandlerJobRepoStub) ClaimSettling(context.Context, int64, time.Time, time.Time) (bool, error) {
	panic("unexpected ClaimSettling call")
}
func (r *imageStudioHandlerJobRepoStub) UpdateHeartbeat(ctx context.Context, id int64, heartbeatAt time.Time) error {
	panic("unexpected UpdateHeartbeat call")
}
func (r *imageStudioHandlerJobRepoStub) MarkRetryable(ctx context.Context, id int64, nextAttemptAt time.Time, errorCode, errorMessage string) error {
	panic("unexpected MarkRetryable call")
}
func (r *imageStudioHandlerJobRepoStub) MarkSettlementRetryable(context.Context, int64, time.Time, string, string) error {
	panic("unexpected MarkSettlementRetryable call")
}
func (r *imageStudioHandlerJobRepoStub) MarkSettlementFailed(context.Context, int64, time.Time, string, string) (bool, error) {
	panic("unexpected MarkSettlementFailed call")
}
func (r *imageStudioHandlerJobRepoStub) MarkSucceeded(ctx context.Context, id int64, completedAt time.Time, chargedAmountUSD float64, originalPath, thumbnailPath, mimeType string, fileSizeBytes int64, width, height int, expiresAt *time.Time) error {
	panic("unexpected MarkSucceeded call")
}
func (r *imageStudioHandlerJobRepoStub) MarkFailed(ctx context.Context, id int64, completedAt time.Time, errorCode, errorMessage string) error {
	panic("unexpected MarkFailed call")
}
func (r *imageStudioHandlerJobRepoStub) ListExpiredAssets(ctx context.Context, now time.Time, limit int) ([]service.ImageStudioJob, error) {
	panic("unexpected ListExpiredAssets call")
}
func (r *imageStudioHandlerJobRepoStub) MarkAssetsDeleted(ctx context.Context, id int64, deletedAt time.Time) error {
	panic("unexpected MarkAssetsDeleted call")
}

type imageStudioHandlerAPIKeyRepoStub struct {
	apiKey *service.APIKey
}

func (r *imageStudioHandlerAPIKeyRepoStub) Create(ctx context.Context, key *service.APIKey) error {
	panic("unexpected Create call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) GetByID(ctx context.Context, id int64) (*service.APIKey, error) {
	if r.apiKey != nil && r.apiKey.ID == id {
		clone := *r.apiKey
		return &clone, nil
	}
	return nil, service.ErrAPIKeyNotFound
}
func (r *imageStudioHandlerAPIKeyRepoStub) GetKeyAndOwnerID(ctx context.Context, id int64) (string, int64, error) {
	panic("unexpected GetKeyAndOwnerID call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) GetByKey(ctx context.Context, key string) (*service.APIKey, error) {
	panic("unexpected GetByKey call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) GetByKeyForAuth(ctx context.Context, key string) (*service.APIKey, error) {
	panic("unexpected GetByKeyForAuth call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) Update(ctx context.Context, key *service.APIKey) error {
	panic("unexpected Update call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) Delete(ctx context.Context, id int64) error {
	panic("unexpected Delete call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) DeleteWithAudit(ctx context.Context, id int64) error {
	panic("unexpected DeleteWithAudit call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) ListByUserID(ctx context.Context, userID int64, params pagination.PaginationParams, filters service.APIKeyListFilters) ([]service.APIKey, *pagination.PaginationResult, error) {
	panic("unexpected ListByUserID call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) VerifyOwnership(ctx context.Context, userID int64, apiKeyIDs []int64) ([]int64, error) {
	panic("unexpected VerifyOwnership call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) CountByUserID(ctx context.Context, userID int64) (int64, error) {
	panic("unexpected CountByUserID call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) ExistsByKey(ctx context.Context, key string) (bool, error) {
	panic("unexpected ExistsByKey call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) ListByGroupID(ctx context.Context, groupID int64, params pagination.PaginationParams) ([]service.APIKey, *pagination.PaginationResult, error) {
	panic("unexpected ListByGroupID call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) SearchAPIKeys(ctx context.Context, userID int64, keyword string, limit int) ([]service.APIKey, error) {
	panic("unexpected SearchAPIKeys call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) ClearGroupIDByGroupID(ctx context.Context, groupID int64) (int64, error) {
	panic("unexpected ClearGroupIDByGroupID call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) UpdateGroupIDByUserAndGroup(ctx context.Context, userID, oldGroupID, newGroupID int64) (int64, error) {
	panic("unexpected UpdateGroupIDByUserAndGroup call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) CountByGroupID(ctx context.Context, groupID int64) (int64, error) {
	panic("unexpected CountByGroupID call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) ListKeysByUserID(ctx context.Context, userID int64) ([]string, error) {
	panic("unexpected ListKeysByUserID call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) ListKeysByGroupID(ctx context.Context, groupID int64) ([]string, error) {
	panic("unexpected ListKeysByGroupID call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) IncrementQuotaUsed(ctx context.Context, id int64, amount float64) (float64, error) {
	panic("unexpected IncrementQuotaUsed call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) UpdateLastUsed(ctx context.Context, id int64, usedAt time.Time) error {
	panic("unexpected UpdateLastUsed call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) IncrementRateLimitUsage(ctx context.Context, id int64, cost float64) error {
	panic("unexpected IncrementRateLimitUsage call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) ResetRateLimitWindows(ctx context.Context, id int64) error {
	panic("unexpected ResetRateLimitWindows call")
}
func (r *imageStudioHandlerAPIKeyRepoStub) GetRateLimitData(ctx context.Context, id int64) (*service.APIKeyRateLimitData, error) {
	panic("unexpected GetRateLimitData call")
}
