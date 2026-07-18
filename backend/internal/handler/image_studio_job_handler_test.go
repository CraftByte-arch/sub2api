package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCopyImageStudioMultipartFileMapsDestinationWriteFailureTo503(t *testing.T) {
	err := copyImageStudioMultipartFile(&imageStudioFailingMultipartWriter{err: syscall.ENOSPC}, bytes.NewReader([]byte("image")))

	require.Equal(t, http.StatusServiceUnavailable, infraerrors.Code(err))
	require.ErrorIs(t, err, syscall.ENOSPC)
}

func TestParseImageStudioMultipartRejectsFifthImageWithoutDrainingIt(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("api_key_id", "1001"))
	require.NoError(t, writer.WriteField("mode", "edit"))
	require.NoError(t, writer.WriteField("model", "gpt-image-2"))
	for i := 0; i < 5; i++ {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", `form-data; name="image"; filename="ignored.bin"`)
		header.Set("Content-Type", "image/png")
		part, err := writer.CreatePart(header)
		require.NoError(t, err)
		if i < 4 {
			_, err = part.Write([]byte("small"))
		} else {
			_, err = part.Write(bytes.Repeat([]byte("x"), 1<<20))
		}
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	reader := &imageStudioCountingReader{Reader: bytes.NewReader(body.Bytes())}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/image-studio/jobs", reader)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())

	_, _, _, _, err := parseImageStudioEditMultipart(c, newImageStudioMultipartTempFile)

	require.Error(t, err)
	require.Less(t, reader.read, body.Len()-(512<<10))
}

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

func TestBuildImageStudioJobPayload_SanitizesEditPayload(t *testing.T) {
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
	require.False(t, gjson.GetBytes(payload, "images").Exists())
	require.False(t, gjson.GetBytes(payload, "mask").Exists())
	require.Equal(t, "b64_json", gjson.GetBytes(payload, "response_format").String())
	require.Equal(t, "png", gjson.GetBytes(payload, "output_format").String())
	require.False(t, gjson.GetBytes(payload, "input").Exists())
	require.False(t, gjson.GetBytes(payload, "tools").Exists())
	require.False(t, gjson.GetBytes(payload, "tool_choice").Exists())
}

func TestImageStudioJobHandlerCreateAcceptsOrderedMultipartImages(t *testing.T) {
	for _, count := range []int{1, 4} {
		t.Run(strconv.Itoa(count)+" images", func(t *testing.T) {
			store := service.NewImageStudioInputStore(t.TempDir(), 1<<20)
			repo := &imageStudioHandlerJobRepoStub{}
			handler := newImageStudioCreateHandler(t, repo, store)
			images := make([][]byte, count)
			parts := []imageStudioMultipartTestPart{
				{name: "api_key_id", value: "1001"},
				{name: "mode", value: "edit"},
				{name: "prompt", value: "replace the sky"},
				{name: "model", value: "gpt-image-2"},
				{name: "size", value: "1024x1024"},
				{name: "quality", value: "high"},
				{name: "background", value: "transparent"},
				{name: "style", value: "vivid"},
				{name: "moderation", value: "low"},
				{name: "input_fidelity", value: "high"},
				{name: "output_format", value: "webp"},
				{name: "output_compression", value: "72"},
			}
			for i := range images {
				images[i] = imageStudioHandlerTestPNG(t, i+2, i+2, color.NRGBA{R: uint8(30 + i), A: 255})
				parts = append(parts, imageStudioMultipartTestPart{name: "image", contentType: "image/png", data: images[i]})
			}

			rec := performImageStudioMultipartCreate(t, handler, parts)

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.True(t, repo.created)
			require.Len(t, repo.lastInput.InputImagePaths, count)
			for i, relativePath := range repo.lastInput.InputImagePaths {
				stored, err := os.ReadFile(filepath.Join(store.Root(), filepath.FromSlash(relativePath)))
				require.NoError(t, err)
				require.Equal(t, images[i], stored)
			}
			require.JSONEq(t, `{
				"model":"gpt-image-2","prompt":"replace the sky","size":"1024x1024",
				"quality":"high","background":"transparent","style":"vivid",
				"moderation":"low","input_fidelity":"high","output_format":"webp",
				"output_compression":72,"response_format":"b64_json"
			}`, string(repo.lastInput.RequestPayload))
			require.NotContains(t, rec.Body.String(), "input_image_paths")
			require.NotContains(t, rec.Body.String(), "inputs/")
		})
	}
}

func TestImageStudioJobHandlerCreateAcceptsSingleMask(t *testing.T) {
	store := service.NewImageStudioInputStore(t.TempDir(), 1<<20)
	repo := &imageStudioHandlerJobRepoStub{}
	handler := newImageStudioCreateHandler(t, repo, store)
	imageBytes := imageStudioHandlerTestPNG(t, 3, 2, color.NRGBA{R: 50, A: 255})
	maskBytes := imageStudioHandlerTestPNG(t, 3, 2, color.NRGBA{A: 0})

	rec := performImageStudioMultipartCreate(t, handler, append(validImageStudioMultipartScalars(),
		imageStudioMultipartTestPart{name: "image", contentType: "image/png", data: imageBytes},
		imageStudioMultipartTestPart{name: "mask", contentType: "image/png", data: maskBytes},
	))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotNil(t, repo.lastInput.InputMaskPath)
	stored, err := os.ReadFile(filepath.Join(store.Root(), filepath.FromSlash(*repo.lastInput.InputMaskPath)))
	require.NoError(t, err)
	require.Equal(t, maskBytes, stored)
}

func TestImageStudioJobHandlerCreateRejectsMultipartShapeErrors(t *testing.T) {
	validImage := imageStudioHandlerTestPNG(t, 2, 2, color.NRGBA{R: 1, A: 255})
	validMask := imageStudioHandlerTestPNG(t, 2, 2, color.NRGBA{A: 0})
	tests := []struct {
		name  string
		parts []imageStudioMultipartTestPart
		want  string
	}{
		{name: "missing required api key", parts: []imageStudioMultipartTestPart{
			{name: "mode", value: "edit"},
			{name: "model", value: "gpt-image-2"},
			{name: "image", contentType: "image/png", data: validImage},
		}, want: "api_key_id is required"},
		{name: "zero images", parts: validImageStudioMultipartScalars(), want: "at least one image"},
		{name: "five images", parts: append(validImageStudioMultipartScalars(),
			imageStudioMultipartTestPart{name: "image", contentType: "image/png", data: validImage},
			imageStudioMultipartTestPart{name: "image", contentType: "image/png", data: validImage},
			imageStudioMultipartTestPart{name: "image", contentType: "image/png", data: validImage},
			imageStudioMultipartTestPart{name: "image", contentType: "image/png", data: validImage},
			imageStudioMultipartTestPart{name: "image", contentType: "image/png", data: validImage},
		), want: "at most four image"},
		{name: "duplicate mask", parts: append(validImageStudioMultipartScalars(),
			imageStudioMultipartTestPart{name: "image", contentType: "image/png", data: validImage},
			imageStudioMultipartTestPart{name: "mask", contentType: "image/png", data: validMask},
			imageStudioMultipartTestPart{name: "mask", contentType: "image/png", data: validMask},
		), want: "mask must appear at most once"},
		{name: "unknown scalar", parts: append(validImageStudioMultipartScalars(),
			imageStudioMultipartTestPart{name: "unknown", value: "value"},
			imageStudioMultipartTestPart{name: "image", contentType: "image/png", data: validImage},
		), want: "unknown multipart field"},
		{name: "duplicate scalar", parts: append(validImageStudioMultipartScalars(),
			imageStudioMultipartTestPart{name: "model", value: "other"},
			imageStudioMultipartTestPart{name: "image", contentType: "image/png", data: validImage},
		), want: "must appear at most once"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := service.NewImageStudioInputStore(t.TempDir(), 1<<20)
			repo := &imageStudioHandlerJobRepoStub{}
			rec := performImageStudioMultipartCreate(t, newImageStudioCreateHandler(t, repo, store), tt.parts)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), tt.want)
			require.False(t, repo.created)
			require.Empty(t, imageStudioHandlerInputDirs(t, store.Root()))
		})
	}
}

func TestImageStudioJobHandlerCreateKeepsGenerationJSONResponseAndPayload(t *testing.T) {
	store := service.NewImageStudioInputStore(t.TempDir(), 1<<20)
	repo := &imageStudioHandlerJobRepoStub{}
	handler := newImageStudioCreateHandler(t, repo, store)

	rec := performImageStudioJSONCreate(t, handler, `{
		"api_key_id":1001,"mode":"generate","prompt":"draw a cat","model":"gpt-image-2",
		"size":"1024x1024","quality":"high","background":"transparent","output_format":"webp",
		"response_format":"url"
	}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, int64(9001), gjson.Get(rec.Body.String(), "data.id").Int())
	require.Equal(t, service.ImageStudioJobModeGenerate, gjson.Get(rec.Body.String(), "data.mode").String())
	require.Equal(t, "draw a cat", gjson.Get(rec.Body.String(), "data.prompt").String())
	require.Equal(t, "gpt-image-2", gjson.Get(rec.Body.String(), "data.model").String())
	require.Equal(t, "1024x1024", gjson.Get(rec.Body.String(), "data.size").String())
	require.Equal(t, "webp", gjson.Get(rec.Body.String(), "data.output_format").String())
	require.JSONEq(t, `{
		"model":"gpt-image-2","prompt":"draw a cat","size":"1024x1024",
		"quality":"high","background":"transparent","output_format":"webp","response_format":"b64_json"
	}`, string(repo.lastInput.RequestPayload))
	require.Empty(t, repo.lastInput.InputImagePaths)
	require.Nil(t, repo.lastInput.InputMaskPath)
	require.Nil(t, repo.lastInput.InputExpiresAt)
}

func TestImageStudioJobHandlerCreateRollsBackRejectedFiles(t *testing.T) {
	validImage := imageStudioHandlerTestPNG(t, 4, 3, color.NRGBA{R: 5, A: 255})
	tests := []struct {
		name         string
		maxFileBytes int64
		parts        []imageStudioMultipartTestPart
	}{
		{name: "spoofed MIME", maxFileBytes: 1 << 20, parts: append(validImageStudioMultipartScalars(),
			imageStudioMultipartTestPart{name: "image", contentType: "image/png", data: []byte("not an image")},
		)},
		{name: "storage file size limit", maxFileBytes: int64(len(validImage) - 1), parts: append(validImageStudioMultipartScalars(),
			imageStudioMultipartTestPart{name: "image", contentType: "image/png", data: validImage},
		)},
		{name: "invalid mask", maxFileBytes: 1 << 20, parts: append(validImageStudioMultipartScalars(),
			imageStudioMultipartTestPart{name: "image", contentType: "image/png", data: validImage},
			imageStudioMultipartTestPart{name: "mask", contentType: "image/png", data: imageStudioHandlerTestPNG(t, 2, 2, color.NRGBA{A: 0})},
		)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := service.NewImageStudioInputStore(t.TempDir(), tt.maxFileBytes)
			repo := &imageStudioHandlerJobRepoStub{}
			rec := performImageStudioMultipartCreate(t, newImageStudioCreateHandler(t, repo, store), tt.parts)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.False(t, repo.created)
			require.Empty(t, imageStudioHandlerInputDirs(t, store.Root()))
		})
	}
}

func TestImageStudioJobHandlerCreateRollsBackInputsWhenRepositoryFails(t *testing.T) {
	multipartTempDir := t.TempDir()
	t.Setenv("TMPDIR", multipartTempDir)
	store := service.NewImageStudioInputStore(t.TempDir(), 1<<20)
	repo := &imageStudioHandlerJobRepoStub{createErr: errors.New("insert failed")}
	parts := append(validImageStudioMultipartScalars(), imageStudioMultipartTestPart{
		name: "image", contentType: "image/png", data: imageStudioHandlerTestPNG(t, 2, 2, color.NRGBA{A: 255}),
	})

	rec := performImageStudioMultipartCreate(t, newImageStudioCreateHandler(t, repo, store), parts)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	require.Empty(t, imageStudioHandlerInputDirs(t, store.Root()))
	tempEntries, err := os.ReadDir(multipartTempDir)
	require.NoError(t, err)
	require.Empty(t, tempEntries)
}

func TestImageStudioJobHandlerCreateObservesTempCloseFailureWithoutReversingSuccess(t *testing.T) {
	store := service.NewImageStudioInputStore(t.TempDir(), 1<<20)
	repo := &imageStudioHandlerJobRepoStub{}
	handler := newImageStudioCreateHandler(t, repo, store)
	closeFailure := errors.New("temp close failed")
	multipartTempDir := t.TempDir()
	handler.createMultipartTempFile = func() (imageStudioMultipartTempFile, error) {
		file, err := os.CreateTemp(multipartTempDir, "upload-*")
		if err != nil {
			return nil, err
		}
		if err := os.Remove(file.Name()); err != nil {
			_ = file.Close()
			return nil, err
		}
		return &imageStudioCloseFailingTempFile{File: file, err: closeFailure}, nil
	}
	var observed error
	handler.observeMultipartCleanupError = func(err error) { observed = err }
	parts := append(validImageStudioMultipartScalars(), imageStudioMultipartTestPart{
		name: "image", contentType: "image/png", data: imageStudioHandlerTestPNG(t, 2, 2, color.NRGBA{A: 255}),
	})

	rec := performImageStudioMultipartCreate(t, handler, parts)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, repo.createCalls)
	require.ErrorIs(t, observed, closeFailure)
	tempEntries, err := os.ReadDir(multipartTempDir)
	require.NoError(t, err)
	require.Empty(t, tempEntries)
}

func TestImageStudioJobHandlerCreateMapsInputStorageUnavailableTo503(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "data-file")
	require.NoError(t, os.WriteFile(dataPath, []byte("not a directory"), 0o600))
	store := service.NewImageStudioInputStore(dataPath, 1<<20)
	repo := &imageStudioHandlerJobRepoStub{}
	parts := append(validImageStudioMultipartScalars(), imageStudioMultipartTestPart{
		name: "image", contentType: "image/png", data: imageStudioHandlerTestPNG(t, 2, 2, color.NRGBA{A: 255}),
	})

	rec := performImageStudioMultipartCreate(t, newImageStudioCreateHandler(t, repo, store), parts)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "input storage is unavailable")
	require.False(t, repo.created)
}

func TestImageStudioJobHandlerCreateRejectsJSONEditWithCompatibilityError(t *testing.T) {
	store := service.NewImageStudioInputStore(t.TempDir(), 1<<20)
	repo := &imageStudioHandlerJobRepoStub{}
	handler := newImageStudioCreateHandler(t, repo, store)
	rec := performImageStudioJSONCreate(t, handler, `{
		"api_key_id":1001,"mode":"edit","prompt":"edit","model":"gpt-image-2",
		"image_data_urls":["data:image/png;base64,SECRET"],"mask_data_url":"data:image/png;base64,MASK"
	}`)

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "multipart/form-data")
	require.Contains(t, rec.Body.String(), "image_data_urls")
	require.False(t, repo.created)
}

func TestImageStudioJobHandlerCreateRejectsMultipartGeneration(t *testing.T) {
	store := service.NewImageStudioInputStore(t.TempDir(), 1<<20)
	repo := &imageStudioHandlerJobRepoStub{}
	parts := validImageStudioMultipartScalars()
	parts[1].value = "generate"
	parts = append(parts, imageStudioMultipartTestPart{
		name: "image", contentType: "image/png", data: imageStudioHandlerTestPNG(t, 2, 2, color.NRGBA{A: 255}),
	})

	rec := performImageStudioMultipartCreate(t, newImageStudioCreateHandler(t, repo, store), parts)

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "only supports edit mode")
	require.False(t, repo.created)
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
			jobService := service.NewImageStudioJobService(jobRepo, settingSvc, nil, time.Now)
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

type imageStudioMultipartTestPart struct {
	name        string
	value       string
	contentType string
	data        []byte
}

type imageStudioFailingMultipartWriter struct {
	err error
}

type imageStudioCountingReader struct {
	*bytes.Reader
	read int
}

func (r *imageStudioCountingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += n
	return n, err
}

func (w *imageStudioFailingMultipartWriter) Write([]byte) (int, error) {
	return 0, w.err
}

type imageStudioCloseFailingTempFile struct {
	*os.File
	err error
}

func (f *imageStudioCloseFailingTempFile) Close() error {
	return errors.Join(f.File.Close(), f.err)
}

func validImageStudioMultipartScalars() []imageStudioMultipartTestPart {
	return []imageStudioMultipartTestPart{
		{name: "api_key_id", value: "1001"},
		{name: "mode", value: "edit"},
		{name: "prompt", value: "edit image"},
		{name: "model", value: "gpt-image-2"},
		{name: "output_format", value: "png"},
	}
}

func newImageStudioCreateHandler(t *testing.T, repo *imageStudioHandlerJobRepoStub, store service.ImageStudioInputStorage) *ImageStudioJobHandler {
	t.Helper()
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	groupID := int64(42)
	apiKeyRepo := &imageStudioHandlerAPIKeyRepoStub{apiKey: &service.APIKey{
		ID: 1001, UserID: 123, Key: "sk-user", Status: service.StatusActive, GroupID: &groupID,
		Group: &service.Group{ID: groupID, AllowImageGeneration: true, RateMultiplier: 1, ImageRateMultiplier: 1},
		User:  &service.User{ID: 123},
	}}
	settingSvc := service.NewSettingService(&imageStudioHandlerSettingRepoStub{values: map[string]string{
		service.SettingKeyImageStudioAvailableGroupIDs: `[42]`,
	}}, cfg)
	jobService := service.NewImageStudioJobService(repo, settingSvc, store, time.Now)
	openAIGateway := service.NewOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, &service.BillingCacheService{}, nil,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	jobService.SetRuntimeDependencies(openAIGateway, nil, nil, nil)
	return NewImageStudioJobHandler(
		jobService,
		service.NewAPIKeyService(apiKeyRepo, nil, nil, nil, nil, nil, cfg),
	)
}

func performImageStudioMultipartCreate(t *testing.T, handler *ImageStudioJobHandler, parts []imageStudioMultipartTestPart) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for i, part := range parts {
		if part.data == nil {
			require.NoError(t, writer.WriteField(part.name, part.value))
			continue
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", `form-data; name="`+part.name+`"; filename="client-`+strconv.Itoa(i)+`.bin"`)
		header.Set("Content-Type", part.contentType)
		filePart, err := writer.CreatePart(header)
		require.NoError(t, err)
		_, err = filePart.Write(part.data)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/image-studio/jobs", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 123})
	handler.Create(c)
	return rec
}

func performImageStudioJSONCreate(t *testing.T, handler *ImageStudioJobHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/image-studio/jobs", bytes.NewReader([]byte(body)))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 123})
	handler.Create(c)
	return rec
}

func imageStudioHandlerTestPNG(t *testing.T, width, height int, fill color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}
	var buffer bytes.Buffer
	require.NoError(t, png.Encode(&buffer, img))
	return buffer.Bytes()
}

func imageStudioHandlerInputDirs(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "inputs"))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			result = append(result, entry.Name())
		}
	}
	return result
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
	created     bool
	createCalls int
	lastInput   service.ImageStudioJobCreateInput
	createErr   error
}

func (r *imageStudioHandlerJobRepoStub) Create(ctx context.Context, input service.ImageStudioJobCreateInput) (*service.ImageStudioJob, error) {
	r.created = true
	r.createCalls++
	r.lastInput = input
	if r.createErr != nil {
		return nil, r.createErr
	}
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
func (r *imageStudioHandlerJobRepoStub) PersistLegacyInputs(context.Context, int64, []string, *string, json.RawMessage, time.Time) error {
	panic("unexpected PersistLegacyInputs call")
}
func (r *imageStudioHandlerJobRepoStub) FailLegacyInputs(context.Context, int64, json.RawMessage, time.Time) error {
	panic("unexpected FailLegacyInputs call")
}
func (r *imageStudioHandlerJobRepoStub) ExpireQueuedInputs(context.Context, time.Time, int) ([]service.ImageStudioJob, error) {
	panic("unexpected ExpireQueuedInputs call")
}
func (r *imageStudioHandlerJobRepoStub) ListExpiredInputs(context.Context, time.Time, int) ([]service.ImageStudioJob, error) {
	panic("unexpected ListExpiredInputs call")
}
func (r *imageStudioHandlerJobRepoStub) MarkInputsDeleted(context.Context, int64, time.Time) error {
	panic("unexpected MarkInputsDeleted call")
}
func (r *imageStudioHandlerJobRepoStub) ListReferencedInputDirs(context.Context) (map[string]struct{}, error) {
	panic("unexpected ListReferencedInputDirs call")
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
