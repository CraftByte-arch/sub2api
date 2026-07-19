//go:build integration

package repository

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestImageStudioEditStorageRealCreateAndWorkerProtocols(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("api key uses ordered four-image multipart spool", func(t *testing.T) {
		fixture := newImageStudioEditIntegrationFixture(t, service.AccountTypeAPIKey)
		imageBytes := [][]byte{
			imageStudioIntegrationPNG(t, color.NRGBA{R: 0x11, A: 0xff}),
			imageStudioIntegrationPNG(t, color.NRGBA{G: 0x22, A: 0xff}),
			imageStudioIntegrationPNG(t, color.NRGBA{B: 0x33, A: 0xff}),
			imageStudioIntegrationPNG(t, color.NRGBA{R: 0x44, G: 0x55, A: 0xff}),
		}
		mask := imageStudioIntegrationMaskPNG(t)

		job, responseBody := fixture.createThroughHandler(t, imageBytes, mask)
		assertImageStudioIntegrationCreate(t, fixture.store, job, responseBody, imageBytes, true)

		fixture.startWorker(t)
		completed := fixture.waitForTerminalOrSettling(t, job.ID)
		require.Equal(t, service.ImageStudioJobStatusSucceeded, completed.Status)
		require.NotNil(t, completed.InputDeletedAt)
		assertImageStudioIntegrationInputsDeleted(t, fixture.store, job)
		require.FileExists(t, completed.OriginalPath)
		require.FileExists(t, completed.ThumbnailPath)
		require.NotNil(t, completed.ExpiresAt, "output retention must remain independent from input deletion")
		require.True(t, completed.ExpiresAt.After(*completed.InputDeletedAt))
		require.NotEmpty(t, completed.SettlementPayload)
		require.NotContains(t, string(completed.SettlementPayload), "data:image")

		captured := fixture.upstream.snapshot(t)
		require.Equal(t, "/v1/images/edits", captured.URL.URL.Path)
		require.Contains(t, captured.ContentType, "multipart/form-data")
		fields, uploadedImages, uploadedMask := parseImageStudioIntegrationMultipart(t, captured)
		require.Equal(t, "gpt-image-2", fields["model"])
		require.Equal(t, "replace the background", fields["prompt"])
		require.Equal(t, imageBytes, uploadedImages)
		require.Equal(t, mask, uploadedMask)

		expiredAssets, err := fixture.repo.ListExpiredAssets(context.Background(), time.Now(), 10)
		require.NoError(t, err)
		require.NotContains(t, imageStudioIntegrationJobIDs(expiredAssets), job.ID)
	})

	t.Run("api key one-image output downloads through authenticated handler", func(t *testing.T) {
		fixture := newImageStudioEditIntegrationFixture(t, service.AccountTypeAPIKey)
		imageBytes := [][]byte{imageStudioIntegrationPNG(t, color.NRGBA{R: 0x5a, G: 0x2c, A: 0xff})}

		job, _ := fixture.createThroughHandler(t, imageBytes, nil)
		fixture.startWorker(t)
		completed := fixture.waitForTerminalOrSettling(t, job.ID)
		require.Equal(t, service.ImageStudioJobStatusSucceeded, completed.Status)
		require.NotNil(t, completed.InputDeletedAt)
		assertImageStudioIntegrationInputsDeleted(t, fixture.store, job)

		captured := fixture.upstream.snapshot(t)
		require.Equal(t, "/v1/images/edits", captured.URL.URL.Path)
		_, uploadedImages, uploadedMask := parseImageStudioIntegrationMultipart(t, captured)
		require.Equal(t, imageBytes, uploadedImages)
		require.Empty(t, uploadedMask)

		h := handler.NewImageStudioJobHandler(fixture.jobService, fixture.apiKeySvc)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/image-studio/jobs/%d/original", job.ID), nil)
		c.Params = gin.Params{{Key: "id", Value: strconv.FormatInt(job.ID, 10)}}
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: fixture.userID})
		h.GetOriginal(c)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Equal(t, fixture.upstream.output, recorder.Body.Bytes())
	})

	t.Run("oauth uses request-local responses edit representation", func(t *testing.T) {
		fixture := newImageStudioEditIntegrationFixture(t, service.AccountTypeOAuth)
		imageBytes := [][]byte{
			imageStudioIntegrationPNG(t, color.NRGBA{R: 0x66, A: 0xff}),
			imageStudioIntegrationPNG(t, color.NRGBA{G: 0x77, A: 0xff}),
			imageStudioIntegrationPNG(t, color.NRGBA{B: 0x88, A: 0xff}),
			imageStudioIntegrationPNG(t, color.NRGBA{R: 0x99, G: 0xaa, A: 0xff}),
		}
		mask := imageStudioIntegrationMaskPNG(t)

		job, _ := fixture.createThroughHandler(t, imageBytes, mask)
		fixture.startWorker(t)
		completed := fixture.waitForTerminalOrSettling(t, job.ID)
		require.Equal(t, service.ImageStudioJobStatusSucceeded, completed.Status)
		require.NotNil(t, completed.InputDeletedAt)
		assertImageStudioIntegrationInputsDeleted(t, fixture.store, job)
		require.FileExists(t, completed.OriginalPath)

		captured := fixture.upstream.snapshot(t)
		require.Equal(t, "/backend-api/codex/responses", captured.URL.URL.Path)
		require.Equal(t, "application/json", captured.ContentType)
		require.Equal(t, "edit", gjson.GetBytes(captured.Body, "tools.0.action").String())
		for i := range imageBytes {
			want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBytes[i])
			require.Equal(t, want, gjson.GetBytes(captured.Body, fmt.Sprintf("input.0.content.%d.image_url", i+1)).String())
		}
		wantMask := "data:image/png;base64," + base64.StdEncoding.EncodeToString(mask)
		require.Equal(t, wantMask, gjson.GetBytes(captured.Body, "tools.0.input_image_mask.image_url").String())
		require.NotContains(t, string(completed.RequestPayload), "data:")
	})
}

func TestImageStudioEditStorageRealLifecycleCleanupAndDelete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fixture := newImageStudioEditIntegrationFixture(t, service.AccountTypeAPIKey)
	ctx := context.Background()
	imageBytes := [][]byte{imageStudioIntegrationPNG(t, color.NRGBA{R: 0xcc, A: 0xff})}

	expiredService := service.NewImageStudioJobService(
		fixture.repo,
		fixture.settings,
		fixture.store,
		func() time.Time { return time.Now().Add(-48 * time.Hour) },
	)
	expired, err := expiredService.CreateEditJob(ctx, service.ImageStudioJobCreateInput{
		UserID: fixture.userID, APIKeyID: fixture.apiKeyID, Mode: service.ImageStudioJobModeEdit,
		Prompt: "expired", Model: "gpt-image-2", Size: "1024x1024", OutputFormat: "png",
		RequestPayload: json.RawMessage(`{"model":"gpt-image-2","prompt":"expired","output_format":"png"}`),
	}, []service.UploadedFile{{Reader: bytes.NewReader(imageBytes[0]), ContentType: "image/png"}}, nil)
	require.NoError(t, err)
	require.FileExists(t, imageStudioIntegrationStoredPath(fixture.store, expired.InputImagePaths[0]))

	fixture.startWorker(t)
	fixture.triggerCleanup()
	expiredRow := fixture.waitForInputsDeleted(t, expired.ID, 3*time.Second)
	fixture.jobService.Stop()
	require.Equal(t, service.ImageStudioJobStatusFailed, expiredRow.Status)
	require.Equal(t, service.ImageStudioInputCodeExpired, expiredRow.ErrorCode)
	require.NotNil(t, expiredRow.InputDeletedAt)
	require.NoFileExists(t, imageStudioIntegrationStoredPath(fixture.store, expired.InputImagePaths[0]))

	deletable, err := fixture.jobService.CreateEditJob(ctx, service.ImageStudioJobCreateInput{
		UserID: fixture.userID, APIKeyID: fixture.apiKeyID, Mode: service.ImageStudioJobModeEdit,
		Prompt: "delete", Model: "gpt-image-2", Size: "1024x1024", OutputFormat: "png",
		RequestPayload: json.RawMessage(`{"model":"gpt-image-2","prompt":"delete","output_format":"png"}`),
	}, []service.UploadedFile{{Reader: bytes.NewReader(imageBytes[0]), ContentType: "image/png"}}, nil)
	require.NoError(t, err)
	inputPath := imageStudioIntegrationStoredPath(fixture.store, deletable.InputImagePaths[0])
	require.FileExists(t, inputPath)

	assetDir := filepath.Join(fixture.dataDir, "image-studio", strconv.FormatInt(deletable.ID, 10))
	require.NoError(t, os.MkdirAll(assetDir, 0o755))
	originalPath := filepath.Join(assetDir, "original.png")
	thumbnailPath := filepath.Join(assetDir, "thumbnail.jpg")
	require.NoError(t, os.WriteFile(originalPath, imageBytes[0], 0o644))
	require.NoError(t, os.WriteFile(thumbnailPath, []byte("thumbnail"), 0o644))

	running, err := fixture.repo.MarkRunning(ctx, deletable.ID, time.Now())
	require.NoError(t, err)
	require.True(t, running)
	require.NoError(t, fixture.repo.MarkSettling(ctx, deletable.ID, json.RawMessage(`{"version":1}`), originalPath, thumbnailPath, "image/png", int64(len(imageBytes[0])), 2, 2, time.Now()))
	expiresAt := time.Now().Add(time.Hour)
	require.NoError(t, fixture.repo.MarkSucceeded(ctx, deletable.ID, time.Now(), 0, originalPath, thumbnailPath, "image/png", int64(len(imageBytes[0])), 2, 2, &expiresAt))

	require.NoError(t, fixture.jobService.DeleteJob(ctx, fixture.userID, deletable.ID))
	_, err = fixture.repo.GetByID(ctx, deletable.ID)
	require.ErrorIs(t, err, service.ErrImageStudioJobNotFound)
	require.NoFileExists(t, inputPath)
	require.NoDirExists(t, assetDir)
}

func TestImageStudioEditStorageRealDurableCleanupRetryBeforeTTL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fixture := newImageStudioEditIntegrationFixture(t, service.AccountTypeAPIKey)
	ctx := context.Background()
	imageBytes := imageStudioIntegrationPNG(t, color.NRGBA{R: 0x31, G: 0x72, A: 0xff})

	job, err := fixture.jobService.CreateEditJob(ctx, service.ImageStudioJobCreateInput{
		UserID: fixture.userID, APIKeyID: fixture.apiKeyID, Mode: service.ImageStudioJobModeEdit,
		Prompt: "durable cleanup retry", Model: "gpt-image-2", Size: "1024x1024", OutputFormat: "png",
		RequestPayload: json.RawMessage(`{"model":"gpt-image-2","prompt":"durable cleanup retry","output_format":"png"}`),
	}, []service.UploadedFile{{Reader: bytes.NewReader(imageBytes), ContentType: "image/png"}}, nil)
	require.NoError(t, err)
	require.NotNil(t, job.InputExpiresAt)
	require.True(t, job.InputExpiresAt.After(time.Now().Add(23*time.Hour)))
	inputPath := imageStudioIntegrationStoredPath(fixture.store, job.InputImagePaths[0])
	require.FileExists(t, inputPath)

	running, err := fixture.repo.MarkRunning(ctx, job.ID, time.Now())
	require.NoError(t, err)
	require.True(t, running)
	assetDir := filepath.Join(fixture.dataDir, "image-studio", strconv.FormatInt(job.ID, 10))
	require.NoError(t, os.MkdirAll(assetDir, 0o755))
	originalPath := filepath.Join(assetDir, "original.png")
	thumbnailPath := filepath.Join(assetDir, "thumbnail.jpg")
	require.NoError(t, os.WriteFile(originalPath, imageBytes, 0o644))
	require.NoError(t, os.WriteFile(thumbnailPath, []byte("thumbnail"), 0o644))
	require.NoError(t, fixture.repo.MarkSettling(ctx, job.ID, json.RawMessage(`{"version":1}`), originalPath, thumbnailPath, "image/png", int64(len(imageBytes)), 2, 2, time.Now()))
	outputExpiresAt := time.Now().Add(time.Hour)
	require.NoError(t, fixture.repo.MarkSucceeded(ctx, job.ID, time.Now(), 0, originalPath, thumbnailPath, "image/png", int64(len(imageBytes)), 2, 2, &outputExpiresAt))

	// This is the durable state left behind when the first RemoveInputs attempt fails.
	fixture.startWorker(t)
	fixture.triggerCleanup()
	cleaned := fixture.waitForInputsDeleted(t, job.ID, 3*time.Second)
	require.Equal(t, service.ImageStudioJobStatusSucceeded, cleaned.Status)
	require.NoFileExists(t, inputPath)
	require.True(t, cleaned.InputExpiresAt.After(*cleaned.InputDeletedAt), "durable cleanup retry must not wait for input TTL")
}

func TestImageStudioEditStorageRealStaleLegacyRecoveryRedactsPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fixture := newImageStudioEditIntegrationFixture(t, service.AccountTypeAPIKey)
	ctx := context.Background()
	imageDataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageStudioIntegrationPNG(t, color.NRGBA{B: 0x91, A: 0xff}))
	maskDataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageStudioIntegrationMaskPNG(t))
	legacyPayload, err := json.Marshal(map[string]any{
		"model": "gpt-image-2", "prompt": "legacy recovery",
		"images": []map[string]string{{"image_url": imageDataURL}},
		"mask":   map[string]string{"image_url": maskDataURL},
	})
	require.NoError(t, err)

	job, err := fixture.repo.Create(ctx, service.ImageStudioJobCreateInput{
		UserID: fixture.userID, APIKeyID: fixture.apiKeyID, Mode: service.ImageStudioJobModeEdit,
		Prompt: "legacy recovery", Model: "gpt-image-2", Size: "1024x1024", OutputFormat: "png",
		RequestPayload: legacyPayload,
	})
	require.NoError(t, err)
	running, err := fixture.repo.MarkRunning(ctx, job.ID, time.Now())
	require.NoError(t, err)
	require.True(t, running)
	completedAt := time.Now()
	changed, err := fixture.repo.MarkStaleRunningFailed(ctx, job.ID, completedAt, completedAt.Add(time.Minute))
	require.NoError(t, err)
	require.True(t, changed)

	terminal, err := fixture.repo.GetByID(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, service.ImageStudioJobStatusFailed, terminal.Status)
	require.Equal(t, "worker_interrupted", terminal.ErrorCode)
	require.NotContains(t, string(terminal.RequestPayload), `"images"`)
	require.NotContains(t, string(terminal.RequestPayload), `"mask"`)
	require.NotContains(t, string(terminal.RequestPayload), "data:image")
}

type imageStudioEditIntegrationFixture struct {
	dataDir      string
	userID       int64
	groupID      int64
	apiKeyID     int64
	repo         service.ImageStudioJobRepository
	store        *service.ImageStudioInputStore
	settings     *service.SettingService
	jobService   *service.ImageStudioJobService
	upstream     *imageStudioIntegrationUpstream
	apiKeySvc    *service.APIKeyService
	accountType  string
	cleanupTicks chan time.Time
}

func newImageStudioEditIntegrationFixture(t *testing.T, accountType string) *imageStudioEditIntegrationFixture {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)

	group := mustCreateGroup(t, integrationEntClient, &service.Group{
		Name:     "image-studio-integration-" + accountType + "-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		Platform: service.PlatformOpenAI, RateMultiplier: 1, ImageRateMultiplier: 1,
	})
	_, err := integrationEntClient.Group.UpdateOneID(group.ID).
		SetAllowImageGeneration(true).
		SetImageRateMultiplier(1).
		Save(ctx)
	require.NoError(t, err)
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email:   fmt.Sprintf("image-studio-%s-%d@example.com", accountType, time.Now().UnixNano()),
		Balance: 100,
	})
	groupID := group.ID
	apiKey := mustCreateApiKey(t, integrationEntClient, &service.APIKey{
		UserID: user.ID, GroupID: &groupID, Key: "sk-image-studio-" + strconv.FormatInt(time.Now().UnixNano(), 10),
	})
	credentials := map[string]any{"access_token": "oauth-integration-token"}
	if accountType == service.AccountTypeAPIKey {
		credentials = map[string]any{
			"api_key":  "upstream-api-key",
			"base_url": "https://image-upstream.example/v1",
		}
	}
	account := mustCreateAccount(t, integrationEntClient, &service.Account{
		Name:     "image-studio-upstream-" + accountType + "-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		Platform: service.PlatformOpenAI, Type: accountType, Credentials: credentials,
		Extra:       map[string]any{service.OpenAIImageProtocolPreferenceExtraKey: service.OpenAIImageProtocolPreferenceImages},
		Concurrency: 2, Priority: 10, Schedulable: true,
	})
	mustBindAccountToGroup(t, integrationEntClient, account.ID, group.ID, 1)

	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM image_studio_jobs WHERE user_id = $1`, user.ID)
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM account_groups WHERE account_id = $1`, account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM accounts WHERE id = $1`, account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM api_keys WHERE id = $1`, apiKey.ID)
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, user.ID)
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM groups WHERE id = $1`, group.ID)
	})

	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	settingRepo := &imageStudioIntegrationSettingRepo{values: map[string]string{
		service.SettingKeyImageStudioAvailableGroupIDs:   fmt.Sprintf("[%d]", group.ID),
		service.SettingKeyImageStudioInputRetentionHours: "24",
		service.SettingKeyImageStudioRetentionValue:      "1",
		service.SettingKeyImageStudioRetentionUnit:       service.ImageStudioRetentionUnitHour,
	}}
	settings := service.NewSettingService(settingRepo, cfg)
	repo := NewImageStudioJobRepository(integrationEntClient, integrationDB)
	store := service.NewImageStudioInputStore(dataDir, 4<<20)
	jobService := service.NewImageStudioJobService(repo, settings, store, time.Now)
	cleanupTicks := make(chan time.Time, 1)
	jobService.SetCleanupTicksForIntegration(cleanupTicks)
	apiKeyRepo := NewAPIKeyRepository(integrationEntClient, integrationDB)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, nil, nil, nil, nil, nil, cfg)
	upstream := &imageStudioIntegrationUpstream{accountType: accountType, output: imageStudioIntegrationPNG(t, color.NRGBA{R: 0xde, G: 0xad, B: 0xbe, A: 0xff})}
	accountRepo := NewAccountRepository(integrationEntClient, integrationDB, nil)
	gateway := service.NewOpenAIGatewayService(
		accountRepo, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, &service.BillingCacheService{}, upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, settings, nil,
	)
	jobService.SetRuntimeDependencies(gateway, apiKeySvc, nil, nil)

	return &imageStudioEditIntegrationFixture{
		dataDir: dataDir, userID: user.ID, groupID: group.ID, apiKeyID: apiKey.ID,
		repo: repo, store: store, settings: settings, jobService: jobService,
		upstream: upstream, apiKeySvc: apiKeySvc, accountType: accountType,
		cleanupTicks: cleanupTicks,
	}
}

func (f *imageStudioEditIntegrationFixture) createThroughHandler(t *testing.T, images [][]byte, mask []byte) (*service.ImageStudioJob, []byte) {
	t.Helper()
	h := handler.NewImageStudioJobHandler(f.jobService, f.apiKeySvc)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{
		"api_key_id": strconv.FormatInt(f.apiKeyID, 10), "mode": service.ImageStudioJobModeEdit,
		"prompt": "replace the background", "model": "gpt-image-2", "size": "1024x1024",
		"output_format": "png", "quality": "high", "input_fidelity": "high",
	} {
		require.NoError(t, writer.WriteField(key, value))
	}
	for i := range images {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "image", "filename": fmt.Sprintf("reference-%d.png", i+1)}))
		header.Set("Content-Type", "image/png")
		part, err := writer.CreatePart(header)
		require.NoError(t, err)
		_, err = part.Write(images[i])
		require.NoError(t, err)
	}
	if mask != nil {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "mask", "filename": "mask.png"}))
		header.Set("Content-Type", "image/png")
		part, err := writer.CreatePart(header)
		require.NoError(t, err)
		_, err = part.Write(mask)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/image-studio/jobs", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: f.userID})
	h.Create(c)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	jobID := gjson.GetBytes(recorder.Body.Bytes(), "data.id").Int()
	require.Positive(t, jobID, recorder.Body.String())
	job, err := f.repo.GetByID(context.Background(), jobID)
	require.NoError(t, err)
	return job, recorder.Body.Bytes()
}

func (f *imageStudioEditIntegrationFixture) startWorker(t *testing.T) {
	t.Helper()
	f.jobService.Start()
	t.Cleanup(f.jobService.Stop)
}

func (f *imageStudioEditIntegrationFixture) triggerCleanup() {
	f.cleanupTicks <- time.Now()
}

func (f *imageStudioEditIntegrationFixture) waitForTerminalOrSettling(t *testing.T, id int64) *service.ImageStudioJob {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		job, err := f.repo.GetByID(context.Background(), id)
		require.NoError(t, err)
		if job.Status == service.ImageStudioJobStatusSucceeded || job.Status == service.ImageStudioJobStatusFailed {
			return job
		}
		time.Sleep(100 * time.Millisecond)
	}
	job, err := f.repo.GetByID(context.Background(), id)
	require.NoError(t, err)
	t.Fatalf("job %d did not complete, status=%s error=%s: %s", id, job.Status, job.ErrorCode, job.ErrorMessage)
	return nil
}

func (f *imageStudioEditIntegrationFixture) waitForInputsDeleted(t *testing.T, id int64, timeout time.Duration) *service.ImageStudioJob {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, err := f.repo.GetByID(context.Background(), id)
		require.NoError(t, err)
		if job.InputDeletedAt != nil {
			return job
		}
		time.Sleep(50 * time.Millisecond)
	}
	job, err := f.repo.GetByID(context.Background(), id)
	require.NoError(t, err)
	t.Fatalf("job %d inputs were not deleted, status=%s error=%s", id, job.Status, job.ErrorCode)
	return nil
}

func assertImageStudioIntegrationCreate(t *testing.T, store *service.ImageStudioInputStore, job *service.ImageStudioJob, response []byte, images [][]byte, hasMask bool) {
	t.Helper()
	require.Equal(t, service.ImageStudioJobStatusQueued, job.Status)
	require.Len(t, job.InputImagePaths, len(images))
	require.Equal(t, hasMask, job.InputMaskPath != nil)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(job.RequestPayload, &payload))
	require.NotContains(t, payload, "images")
	require.NotContains(t, payload, "mask")
	require.NotContains(t, string(job.RequestPayload), "data:")
	require.NotContains(t, string(response), "input_image_paths")
	require.NotContains(t, string(response), "input_mask_path")
	require.NotContains(t, string(response), store.Root())
	for i, path := range job.InputImagePaths {
		require.FileExists(t, imageStudioIntegrationStoredPath(store, path))
		stored, err := os.ReadFile(imageStudioIntegrationStoredPath(store, path))
		require.NoError(t, err)
		require.Equal(t, images[i], stored)
	}
	if job.InputMaskPath != nil {
		require.FileExists(t, imageStudioIntegrationStoredPath(store, *job.InputMaskPath))
	}
}

func assertImageStudioIntegrationInputsDeleted(t *testing.T, store *service.ImageStudioInputStore, job *service.ImageStudioJob) {
	t.Helper()
	for _, path := range job.InputImagePaths {
		require.NoFileExists(t, imageStudioIntegrationStoredPath(store, path))
	}
	if job.InputMaskPath != nil {
		require.NoFileExists(t, imageStudioIntegrationStoredPath(store, *job.InputMaskPath))
	}
}

type imageStudioIntegrationCapture struct {
	URL         *http.Request
	Body        []byte
	ContentType string
}

type imageStudioIntegrationUpstream struct {
	mu          sync.Mutex
	accountType string
	output      []byte
	request     *http.Request
	body        []byte
}

func (u *imageStudioIntegrationUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.mu.Lock()
	u.request = req.Clone(req.Context())
	u.body = append([]byte(nil), body...)
	u.mu.Unlock()
	if u.accountType == service.AccountTypeOAuth {
		response := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_integration\",\"created_at\":1710000002,\"usage\":{\"input_tokens\":13,\"output_tokens\":21,\"output_tokens_details\":{\"image_tokens\":8}},\"tool_usage\":{\"image_gen\":{\"images\":1}},\"output\":[{\"type\":\"image_generation_call\",\"result\":\"" + base64.StdEncoding.EncodeToString(u.output) + "\",\"output_format\":\"png\"}]}}\n\n" +
			"data: [DONE]\n\n"
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"req_oauth_integration"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
	}
	response, err := json.Marshal(map[string]any{"created": 1710000007, "data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(u.output)}}})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"req_apikey_integration"}}, Body: io.NopCloser(bytes.NewReader(response))}, nil
}

func (u *imageStudioIntegrationUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

func (u *imageStudioIntegrationUpstream) snapshot(t *testing.T) imageStudioIntegrationCapture {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	require.NotNil(t, u.request)
	return imageStudioIntegrationCapture{URL: u.request, Body: append([]byte(nil), u.body...), ContentType: u.request.Header.Get("Content-Type")}
}

func parseImageStudioIntegrationMultipart(t *testing.T, captured imageStudioIntegrationCapture) (map[string]string, [][]byte, []byte) {
	t.Helper()
	_, params, err := mime.ParseMediaType(captured.ContentType)
	require.NoError(t, err)
	reader := multipart.NewReader(bytes.NewReader(captured.Body), params["boundary"])
	fields := make(map[string]string)
	var images [][]byte
	var mask []byte
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		data, err := io.ReadAll(part)
		require.NoError(t, err)
		switch part.FormName() {
		case "image":
			images = append(images, data)
		case "mask":
			mask = data
		default:
			fields[part.FormName()] = string(data)
		}
	}
	return fields, images, mask
}

func imageStudioIntegrationPNG(t *testing.T, fill color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}
	var buffer bytes.Buffer
	require.NoError(t, png.Encode(&buffer, img))
	return buffer.Bytes()
}

func imageStudioIntegrationMaskPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{A: 0})
	img.SetNRGBA(1, 0, color.NRGBA{A: 0xff})
	img.SetNRGBA(0, 1, color.NRGBA{A: 0x80})
	img.SetNRGBA(1, 1, color.NRGBA{A: 0xff})
	var buffer bytes.Buffer
	require.NoError(t, png.Encode(&buffer, img))
	return buffer.Bytes()
}

func imageStudioIntegrationStoredPath(store *service.ImageStudioInputStore, relative string) string {
	return filepath.Join(store.Root(), filepath.FromSlash(relative))
}

func imageStudioIntegrationJobIDs(jobs []service.ImageStudioJob) []int64 {
	ids := make([]int64, 0, len(jobs))
	for i := range jobs {
		ids = append(ids, jobs[i].ID)
	}
	return ids
}

type imageStudioIntegrationSettingRepo struct {
	values map[string]string
}

func (r *imageStudioIntegrationSettingRepo) Get(_ context.Context, key string) (*service.Setting, error) {
	value, ok := r.values[key]
	if !ok {
		return nil, service.ErrSettingNotFound
	}
	return &service.Setting{Key: key, Value: value}, nil
}

func (r *imageStudioIntegrationSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func (r *imageStudioIntegrationSettingRepo) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

func (r *imageStudioIntegrationSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string)
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (r *imageStudioIntegrationSettingRepo) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func (r *imageStudioIntegrationSettingRepo) GetAll(context.Context) (map[string]string, error) {
	values := make(map[string]string, len(r.values))
	for key, value := range r.values {
		values[key] = value
	}
	return values, nil
}

func (r *imageStudioIntegrationSettingRepo) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}
