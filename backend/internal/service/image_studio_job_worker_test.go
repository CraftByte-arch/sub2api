package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestImageStudioJobServiceRejectsInvalidStoredEditInputsBeforeExecution(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	validUntil := now.Add(time.Hour)
	expiredAt := now.Add(-time.Second)
	deletedAt := now.Add(-time.Minute)
	png := imageStudioTestPNG(t, 2, 2, false)
	jpeg := imageStudioTestJPEG(t, 2, 2)
	mask := imageStudioTestPNG(t, 2, 2, true)

	tests := []struct {
		name     string
		prepare  func(t *testing.T) (ImageStudioInputStorage, ImageStudioJob)
		wantCode string
	}{
		{
			name: "expired",
			prepare: func(t *testing.T) (ImageStudioInputStorage, ImageStudioJob) {
				store, staged := stageImageStudioWorkerInputs(t, [][]byte{png}, nil, 1<<20)
				return store, storedImageStudioEditJob(staged, &expiredAt)
			},
			wantCode: ImageStudioInputCodeExpired,
		},
		{
			name: "deleted",
			prepare: func(t *testing.T) (ImageStudioInputStorage, ImageStudioJob) {
				store, staged := stageImageStudioWorkerInputs(t, [][]byte{png}, nil, 1<<20)
				job := storedImageStudioEditJob(staged, &validUntil)
				job.InputDeletedAt = &deletedAt
				return store, job
			},
			wantCode: ImageStudioInputCodeMissing,
		},
		{
			name: "missing expiration metadata",
			prepare: func(t *testing.T) (ImageStudioInputStorage, ImageStudioJob) {
				store, staged := stageImageStudioWorkerInputs(t, [][]byte{png}, nil, 1<<20)
				return store, storedImageStudioEditJob(staged, nil)
			},
			wantCode: ImageStudioInputCodeInvalid,
		},
		{
			name: "zero paths",
			prepare: func(t *testing.T) (ImageStudioInputStorage, ImageStudioJob) {
				return NewImageStudioInputStore(t.TempDir(), 1<<20), ImageStudioJob{
					ID: 39, UserID: 42, APIKeyID: 41, Mode: ImageStudioJobModeEdit,
					Status: ImageStudioJobStatusQueued, InputExpiresAt: &validUntil,
					RequestPayload: json.RawMessage(`{"model":"gpt-image-2","prompt":"edit"}`),
				}
			},
			wantCode: ImageStudioInputCodeInvalid,
		},
		{
			name: "five paths",
			prepare: func(t *testing.T) (ImageStudioInputStorage, ImageStudioJob) {
				paths := []string{
					"inputs/upload-five/image-01.png", "inputs/upload-five/image-02.png",
					"inputs/upload-five/image-03.png", "inputs/upload-five/image-04.png",
					"inputs/upload-five/image-05.png",
				}
				return NewImageStudioInputStore(t.TempDir(), 1<<20), ImageStudioJob{
					ID: 39, UserID: 42, APIKeyID: 41, Mode: ImageStudioJobModeEdit,
					Status: ImageStudioJobStatusQueued, InputImagePaths: paths, InputExpiresAt: &validUntil,
					RequestPayload: json.RawMessage(`{"model":"gpt-image-2","prompt":"edit"}`),
				}
			},
			wantCode: ImageStudioInputCodeInvalid,
		},
		{
			name: "missing file",
			prepare: func(t *testing.T) (ImageStudioInputStorage, ImageStudioJob) {
				store, staged := stageImageStudioWorkerInputs(t, [][]byte{png}, nil, 1<<20)
				require.NoError(t, os.Remove(filepath.Join(store.Root(), filepath.FromSlash(staged.ImagePaths[0]))))
				return store, storedImageStudioEditJob(staged, &validUntil)
			},
			wantCode: ImageStudioInputCodeMissing,
		},
		{
			name: "corrupt file",
			prepare: func(t *testing.T) (ImageStudioInputStorage, ImageStudioJob) {
				store, staged := stageImageStudioWorkerInputs(t, [][]byte{png}, nil, 1<<20)
				require.NoError(t, os.WriteFile(filepath.Join(store.Root(), filepath.FromSlash(staged.ImagePaths[0])), []byte("not an image"), 0o600))
				return store, storedImageStudioEditJob(staged, &validUntil)
			},
			wantCode: ImageStudioInputCodeInvalid,
		},
		{
			name: "detected mime changed after staging",
			prepare: func(t *testing.T) (ImageStudioInputStorage, ImageStudioJob) {
				store, staged := stageImageStudioWorkerInputs(t, [][]byte{png}, nil, 1<<20)
				require.NoError(t, os.WriteFile(filepath.Join(store.Root(), filepath.FromSlash(staged.ImagePaths[0])), jpeg, 0o600))
				return store, storedImageStudioEditJob(staged, &validUntil)
			},
			wantCode: ImageStudioInputCodeInvalid,
		},
		{
			name: "oversized after staging",
			prepare: func(t *testing.T) (ImageStudioInputStorage, ImageStudioJob) {
				dataDir := t.TempDir()
				stagingStore := NewImageStudioInputStore(dataDir, 1<<20)
				staged, err := stagingStore.StageEditInputs(context.Background(), []UploadedFile{{Reader: bytes.NewReader(png), ContentType: "image/png"}}, nil)
				require.NoError(t, err)
				return NewImageStudioInputStore(dataDir, int64(len(png)-1)), storedImageStudioEditJob(staged, &validUntil)
			},
			wantCode: ImageStudioInputCodeInvalid,
		},
		{
			name: "unsafe path",
			prepare: func(t *testing.T) (ImageStudioInputStorage, ImageStudioJob) {
				store, staged := stageImageStudioWorkerInputs(t, [][]byte{png}, nil, 1<<20)
				staged.ImagePaths[0] = "../outside/image-01.png"
				return store, storedImageStudioEditJob(staged, &validUntil)
			},
			wantCode: ImageStudioInputCodePathInvalid,
		},
		{
			name: "mixed upload directories",
			prepare: func(t *testing.T) (ImageStudioInputStorage, ImageStudioJob) {
				dataDir := t.TempDir()
				store := NewImageStudioInputStore(dataDir, 1<<20)
				first, err := store.StageEditInputs(context.Background(), []UploadedFile{{Reader: bytes.NewReader(png), ContentType: "image/png"}}, nil)
				require.NoError(t, err)
				second, err := store.StageEditInputs(context.Background(), []UploadedFile{{Reader: bytes.NewReader(png), ContentType: "image/png"}}, nil)
				require.NoError(t, err)
				first.ImagePaths = append(first.ImagePaths, second.ImagePaths[0])
				return store, storedImageStudioEditJob(first, &validUntil)
			},
			wantCode: ImageStudioInputCodePathInvalid,
		},
		{
			name: "mask dimensions changed after staging",
			prepare: func(t *testing.T) (ImageStudioInputStorage, ImageStudioJob) {
				store, staged := stageImageStudioWorkerInputs(t, [][]byte{png}, mask, 1<<20)
				mismatched := imageStudioTestPNG(t, 3, 2, true)
				require.NoError(t, os.WriteFile(filepath.Join(store.Root(), filepath.FromSlash(*staged.MaskPath)), mismatched, 0o600))
				return store, storedImageStudioEditJob(staged, &validUntil)
			},
			wantCode: ImageStudioInputCodeInvalid,
		},
		{
			name: "unknown storage failure",
			prepare: func(t *testing.T) (ImageStudioInputStorage, ImageStudioJob) {
				path := "inputs/upload-unavailable/image-01.png"
				return &imageStudioWorkerStorageStub{openErr: errors.New("mount unavailable")}, storedImageStudioEditJob(&StagedEditInputs{ImagePaths: []string{path}}, &validUntil)
			},
			wantCode: ImageStudioInputCodeStorageUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, job := tt.prepare(t)
			repo := &imageStudioWorkerRepoStub{failExpiredRunningChanged: tt.name == "expired"}
			apiKeys := &imageStudioAPIKeyRepoStub{apiKey: validImageStudioWorkerAPIKey()}
			executor := &recordingImageStudioJobExecutor{err: errors.New("executor should not run")}
			svc := newImageStudioWorkerTestService(repo, store, apiKeys, executor, now)

			svc.processJob(context.Background(), job)

			if tt.name == "expired" {
				require.Zero(t, repo.markFailedCalls)
				require.Equal(t, 1, repo.failExpiredRunningCalls)
				require.Equal(t, 1, repo.markInputsDeletedCalls)
			} else {
				require.Equal(t, 1, repo.markFailedCalls)
				require.Equal(t, tt.wantCode, repo.failedErrorCode)
			}
			require.Zero(t, repo.markRetryableCalls)
			require.True(t, repo.retryAt.IsZero(), "terminal input errors must not set next_attempt_at")
			require.Zero(t, apiKeys.calls, "input validation must happen before API key/account selection")
			require.Zero(t, executor.calls, "invalid input must never reach account selection or upstream execution")
		})
	}
}

func TestImageStudioJobServicePassesOpenedStoredInputsToExecutorInOrder(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Hour)
	first := imageStudioTestPNG(t, 2, 2, false)
	mask := imageStudioTestPNG(t, 2, 2, true)

	for _, tt := range []struct {
		count    int
		withMask bool
	}{{count: 1}, {count: 4, withMask: true}} {
		t.Run(fmt.Sprintf("%d images mask=%t", tt.count, tt.withMask), func(t *testing.T) {
			images := make([][]byte, tt.count)
			for i := range images {
				images[i] = first
			}
			var optionalMask []byte
			if tt.withMask {
				optionalMask = mask
			}
			store, staged := stageImageStudioWorkerInputs(t, images, optionalMask, 1<<20)
			repo := &imageStudioWorkerRepoStub{}
			apiKeys := &imageStudioAPIKeyRepoStub{apiKey: validImageStudioWorkerAPIKey()}
			executor := &recordingImageStudioJobExecutor{err: errors.New("stop after capture")}
			svc := newImageStudioWorkerTestService(repo, store, apiKeys, executor, now)

			svc.processJob(context.Background(), storedImageStudioEditJob(staged, &expiresAt))

			require.Equal(t, 1, executor.calls)
			require.Equal(t, staged.ImagePaths, executor.imagePaths)
			require.Equal(t, staged.MaskPath, executor.maskPath)
			require.Len(t, executor.openFiles, tt.count+boolToInt(tt.withMask))
			require.True(t, executor.handlesLive, "executor must receive live handles")
			for _, file := range executor.openFiles {
				_, err := file.Stat()
				require.Error(t, err, "worker must close every handle after execution")
			}
			require.Equal(t, 1, repo.markFailedCalls)
			require.Equal(t, "upstream_failed", repo.failedErrorCode)
		})
	}
}

func TestImageStudioJobServiceForwardSelectedAPIKeyEditAlwaysCleansMultipartSpool(t *testing.T) {
	imageBytes := imageStudioTestPNG(t, 2, 2, false)
	maskBytes := imageStudioTestPNG(t, 2, 2, true)

	for _, tt := range []struct {
		name        string
		cancel      bool
		upstream    *http.Response
		upstreamErr error
		wantErr     bool
		parseImage  bool
	}{
		{
			name: "success",
			upstream: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(
				`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(imageBytes) + `"}]}`,
			))},
		},
		{
			name:        "transport error",
			upstreamErr: errors.New("connection reset"),
			wantErr:     true,
		},
		{
			name: "failover response",
			upstream: &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(
				`{"error":{"message":"rate limited"}}`,
			))},
			wantErr: true,
		},
		{
			name:        "canceled",
			cancel:      true,
			upstreamErr: context.Canceled,
			wantErr:     true,
		},
		{
			name: "response image parse failure after provider success",
			upstream: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(
				`{"data":[{"b64_json":"not-base64"}]}`,
			))},
			parseImage: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store, staged := stageImageStudioWorkerInputs(t, [][]byte{imageBytes}, maskBytes, 1<<20)
			opened, err := store.OpenInputs(staged.ImagePaths, staged.MaskPath)
			require.NoError(t, err)
			t.Cleanup(func() { _ = opened.Close() })
			input := &imageStudioExecutionInput{
				Payload:    []byte(`{"model":"gpt-image-1","prompt":"edit","output_format":"png"}`),
				EditInputs: opened,
			}
			parsed := &OpenAIImagesRequest{
				Endpoint: openAIImagesEditsEndpoint, Model: "gpt-image-1", Prompt: "edit", N: 1,
				SizeTier: "1024x1024", Multipart: true, HasMask: true, RequiredCapability: OpenAIImagesCapabilityNative,
			}
			upstream := &httpUpstreamRecorder{resp: tt.upstream, err: tt.upstreamErr}
			gateway := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			svc := &ImageStudioJobService{inputStore: store, openAIGateway: gateway}
			account := &Account{
				ID: 81, Name: "image-apikey", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test-api-key", "base_url": "https://image-upstream.example/v1"},
			}
			recorder := httptest.NewRecorder()
			ginCtx, _ := gin.CreateTestContext(recorder)
			ginCtx.Request = httptest.NewRequest(http.MethodPost, openAIImagesEditsEndpoint, nil)
			ctx := context.Background()
			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			result, err := svc.forwardSelectedAPIKeyEdit(ctx, ginCtx, account, input, parsed, "")
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
			}
			if tt.parseImage {
				_, _, decodeErr := decodeImageStudioResponseImage(recorder.Body.Bytes(), "png")
				require.Error(t, decodeErr)
			}
			uploadDir := filepath.Dir(filepath.Join(store.Root(), filepath.FromSlash(staged.ImagePaths[0])))
			matches, globErr := filepath.Glob(filepath.Join(uploadDir, ".spool-*.multipart"))
			require.NoError(t, globErr)
			require.Empty(t, matches)
			if upstream.lastReq != nil {
				require.Equal(t, openAIImagesEditsEndpoint, upstream.lastReq.URL.Path)
				require.Contains(t, upstream.lastReq.Header.Get("Content-Type"), "multipart/form-data")
				require.Positive(t, upstream.lastReq.ContentLength)
			}
		})
	}
}

func TestImageStudioJobServiceClosesInputsWithoutOverwritingCompletedUpstreamResult(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Hour)
	path := "inputs/upload-close/image-01.png"

	t.Run("close failure before executor is terminal storage unavailable", func(t *testing.T) {
		file := openImageStudioWorkerTestFile(t)
		require.NoError(t, file.Close())
		store := &imageStudioWorkerStorageStub{opened: &OpenedEditInputs{Images: []OpenedEditInput{{File: file, Path: path, ContentType: "image/png"}}}}
		repo := &imageStudioWorkerRepoStub{}
		apiKeys := &imageStudioAPIKeyRepoStub{err: errors.New("api key database unavailable")}
		executor := &recordingImageStudioJobExecutor{}
		svc := newImageStudioWorkerTestService(repo, store, apiKeys, executor, now)

		svc.processJob(context.Background(), storedImageStudioEditJob(&StagedEditInputs{ImagePaths: []string{path}}, &expiresAt))

		require.Equal(t, 1, repo.markFailedCalls)
		require.Equal(t, ImageStudioInputCodeStorageUnavailable, repo.failedErrorCode)
		require.Zero(t, repo.markRetryableCalls)
		require.Zero(t, executor.calls)
	})

	t.Run("close failure after retryable executor error preserves retry", func(t *testing.T) {
		store, staged := stageImageStudioWorkerInputs(t, [][]byte{imageStudioTestPNG(t, 2, 2, false)}, nil, 1<<20)
		repo := &imageStudioWorkerRepoStub{}
		apiKeys := &imageStudioAPIKeyRepoStub{apiKey: validImageStudioWorkerAPIKey()}
		executor := &recordingImageStudioJobExecutor{err: &UpstreamFailoverError{StatusCode: 429}, closeBeforeReturn: true}
		svc := newImageStudioWorkerTestService(repo, store, apiKeys, executor, now)

		svc.processJob(context.Background(), storedImageStudioEditJob(staged, &expiresAt))

		require.Zero(t, repo.markFailedCalls)
		require.Equal(t, 1, repo.markRetryableCalls)
		require.Equal(t, "upstream_failed", repo.retryErrorCode)
		require.Contains(t, repo.retryErrorMessage, "file already closed")
		require.FileExists(t, filepath.Join(store.Root(), filepath.FromSlash(staged.ImagePaths[0])))
	})

	t.Run("close failure after successful executor does not replace result", func(t *testing.T) {
		t.Setenv("DATA_DIR", t.TempDir())
		file := openImageStudioWorkerTestFile(t)
		store := &imageStudioWorkerStorageStub{opened: &OpenedEditInputs{Images: []OpenedEditInput{{File: file, Path: path, ContentType: "image/png"}}}}
		repo := &imageStudioWorkerRepoStub{}
		apiKeys := &imageStudioAPIKeyRepoStub{apiKey: validImageStudioWorkerAPIKey()}
		resultImage := imageStudioTestPNG(t, 2, 2, false)
		executor := &recordingImageStudioJobExecutor{
			closeBeforeReturn: true,
			outcome: &imageStudioForwardOutcome{
				result:    &OpenAIForwardResult{Model: "gpt-image-2", ImageCount: 1, ImageSize: "1024x1024"},
				rawBody:   []byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(resultImage) + `"}]}`),
				accountID: 77,
			},
		}
		svc := newImageStudioWorkerTestService(repo, store, apiKeys, executor, now)
		job := storedImageStudioEditJob(&StagedEditInputs{ImagePaths: []string{path}}, &expiresAt)
		job.OutputFormat = "png"

		svc.processJob(context.Background(), job)

		require.Zero(t, repo.markFailedCalls, "a close error must not replace a completed upstream result")
		require.Equal(t, 1, repo.markSettlingCalls)
		require.Equal(t, 1, repo.markSettlementRetryableCalls)
	})
}

func TestImageStudioJobServiceStoredEditProviderFailureStillRetries(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Hour)
	tests := []struct {
		name string
		err  error
	}{
		{name: "429", err: &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}},
		{name: "503", err: &UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable}},
		{name: "transport", err: errors.New("connection reset by peer")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, staged := stageImageStudioWorkerInputs(t, [][]byte{imageStudioTestPNG(t, 2, 2, false)}, nil, 1<<20)
			repo := &imageStudioWorkerRepoStub{}
			apiKeys := &imageStudioAPIKeyRepoStub{apiKey: validImageStudioWorkerAPIKey()}
			executor := &recordingImageStudioJobExecutor{err: tt.err}
			svc := newImageStudioWorkerTestService(repo, store, apiKeys, executor, now)

			svc.processJob(context.Background(), storedImageStudioEditJob(staged, &expiresAt))

			require.Equal(t, 1, repo.markRetryableCalls)
			require.Equal(t, "upstream_failed", repo.retryErrorCode)
			require.False(t, repo.retryAt.IsZero())
			require.Zero(t, repo.markFailedCalls)
			require.Equal(t, 1, executor.calls)
			for _, file := range executor.openFiles {
				_, err := file.Stat()
				require.Error(t, err)
			}
			for _, relativePath := range staged.ImagePaths {
				require.FileExists(t, filepath.Join(store.Root(), filepath.FromSlash(relativePath)), "retryable provider errors must retain inputs")
			}
		})
	}
}

func TestImageStudioJobServiceExpiredRunningFailureBecomesTerminalAndDeletesInputs(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(-time.Second)
	store := &imageStudioWorkerStorageStub{}
	repo := &imageStudioWorkerRepoStub{failExpiredRunningChanged: true}
	svc := &ImageStudioJobService{repo: repo, inputStore: store, now: func() time.Time { return now }}
	job := ImageStudioJob{ID: 39, Status: ImageStudioJobStatusRunning, Mode: ImageStudioJobModeEdit,
		InputImagePaths: []string{"inputs/upload-expired/image-01.png"}, InputExpiresAt: &expiresAt, MaxAttempts: 3}

	svc.handleJobError(context.Background(), job, "upstream_failed", &UpstreamFailoverError{StatusCode: 429})

	require.Equal(t, 1, repo.failExpiredRunningCalls)
	require.Zero(t, repo.markRetryableCalls)
	require.Equal(t, 1, store.removeCalls)
	require.Equal(t, 1, repo.markInputsDeletedCalls)
}

func TestImageStudioJobServicePersistAssetsRequiresDurableSyncBeforeReturning(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	svc := &ImageStudioJobService{syncAssetFile: func(*os.File) error { return errors.New("sync failed") }}

	_, _, _, _, _, err := svc.persistAssets(39, imageStudioTestPNG(t, 2, 2, false), "image/png")

	require.ErrorContains(t, err, "sync failed")
	require.NoDirExists(t, filepath.Join(os.Getenv("DATA_DIR"), imageStudioAssetBaseDir, "39"))
}

func TestImageStudioJobServiceDeletesInputsOnlyAfterSettlingIsDurable(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Hour)
	resultImage := imageStudioTestPNG(t, 2, 2, false)

	t.Run("mark settling succeeds", func(t *testing.T) {
		t.Setenv("DATA_DIR", t.TempDir())
		store, staged := stageImageStudioWorkerInputs(t, [][]byte{resultImage}, nil, 1<<20)
		repo := &imageStudioWorkerRepoStub{}
		executor := successfulImageStudioWorkerExecutor(resultImage)
		svc := newImageStudioWorkerTestService(repo, store, &imageStudioAPIKeyRepoStub{apiKey: validImageStudioWorkerAPIKey()}, executor, now)

		svc.processJob(context.Background(), storedImageStudioEditJob(staged, &expiresAt))

		require.Equal(t, 1, repo.markSettlingCalls)
		require.Equal(t, 1, repo.markInputsDeletedCalls)
		require.NoFileExists(t, filepath.Join(store.Root(), filepath.FromSlash(staged.ImagePaths[0])))
	})

	t.Run("mark settling fails", func(t *testing.T) {
		t.Setenv("DATA_DIR", t.TempDir())
		store, staged := stageImageStudioWorkerInputs(t, [][]byte{resultImage}, nil, 1<<20)
		repo := &imageStudioWorkerRepoStub{markSettlingErr: errors.New("database unavailable")}
		executor := successfulImageStudioWorkerExecutor(resultImage)
		svc := newImageStudioWorkerTestService(repo, store, &imageStudioAPIKeyRepoStub{apiKey: validImageStudioWorkerAPIKey()}, executor, now)

		svc.processJob(context.Background(), storedImageStudioEditJob(staged, &expiresAt))

		require.Equal(t, 1, repo.markSettlingCalls)
		require.Zero(t, repo.markInputsDeletedCalls)
		require.FileExists(t, filepath.Join(store.Root(), filepath.FromSlash(staged.ImagePaths[0])))
	})

	t.Run("input timestamp failure keeps durable assets and settlement", func(t *testing.T) {
		dataDir := t.TempDir()
		t.Setenv("DATA_DIR", dataDir)
		store, staged := stageImageStudioWorkerInputs(t, [][]byte{resultImage}, nil, 1<<20)
		repo := &imageStudioWorkerRepoStub{markInputsDeletedErr: errors.New("mark deleted unavailable")}
		executor := successfulImageStudioWorkerExecutor(resultImage)
		svc := newImageStudioWorkerTestService(repo, store, &imageStudioAPIKeyRepoStub{apiKey: validImageStudioWorkerAPIKey()}, executor, now)

		svc.processJob(context.Background(), storedImageStudioEditJob(staged, &expiresAt))

		require.Equal(t, 1, executor.calls)
		require.Equal(t, 1, repo.markSettlingCalls)
		require.Equal(t, 1, repo.markInputsDeletedCalls)
		require.Equal(t, 1, repo.markSettlementRetryableCalls, "input timestamp failure must not block settlement")
		require.FileExists(t, filepath.Join(dataDir, imageStudioAssetBaseDir, "39", "original.png"))
	})

	t.Run("input remove failure keeps settlement and does not resend upstream", func(t *testing.T) {
		t.Setenv("DATA_DIR", t.TempDir())
		actualStore, staged := stageImageStudioWorkerInputs(t, [][]byte{resultImage}, nil, 1<<20)
		store := &imageStudioRemoveFailStorage{ImageStudioInputStorage: actualStore, err: errors.New("remove unavailable")}
		repo := &imageStudioWorkerRepoStub{}
		executor := successfulImageStudioWorkerExecutor(resultImage)
		svc := newImageStudioWorkerTestService(repo, store, &imageStudioAPIKeyRepoStub{apiKey: validImageStudioWorkerAPIKey()}, executor, now)

		svc.processJob(context.Background(), storedImageStudioEditJob(staged, &expiresAt))

		require.Equal(t, 1, executor.calls)
		require.Equal(t, 1, repo.markSettlingCalls)
		require.Zero(t, repo.markInputsDeletedCalls)
		require.Equal(t, 1, repo.markSettlementRetryableCalls)
		require.FileExists(t, filepath.Join(actualStore.Root(), filepath.FromSlash(staged.ImagePaths[0])))
	})
}

func TestImageStudioJobServiceSettlingRetryNeverOpensInputs(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	store := &imageStudioWorkerStorageStub{}
	repo := &imageStudioWorkerRepoStub{claimSettling: true}
	svc := &ImageStudioJobService{
		repo: repo, inputStore: store, now: func() time.Time { return now },
		apiKeyService: NewAPIKeyService(&imageStudioAPIKeyRepoStub{err: ErrAPIKeyNotFound}, nil, nil, nil, nil, nil, nil),
	}

	svc.processJob(context.Background(), ImageStudioJob{
		ID: 39, Status: ImageStudioJobStatusSettling,
		InputImagePaths: []string{"inputs/upload-retry/image-01.png"},
	})

	require.Zero(t, store.openCalls)
	require.Equal(t, 1, store.removeCalls)
	require.Equal(t, 1, repo.markInputsDeletedCalls)
}

func successfulImageStudioWorkerExecutor(imageBytes []byte) *recordingImageStudioJobExecutor {
	return &recordingImageStudioJobExecutor{outcome: &imageStudioForwardOutcome{
		result:    &OpenAIForwardResult{Model: "gpt-image-2", ImageCount: 1, ImageSize: "1024x1024"},
		rawBody:   []byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(imageBytes) + `"}]}`),
		accountID: 77,
	}}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func openImageStudioWorkerTestFile(t *testing.T) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.png")
	require.NoError(t, os.WriteFile(path, imageStudioTestPNG(t, 2, 2, false), 0o600))
	file, err := os.Open(path)
	require.NoError(t, err)
	return file
}

func stageImageStudioWorkerInputs(t *testing.T, images [][]byte, mask []byte, maxBytes int64) (*ImageStudioInputStore, *StagedEditInputs) {
	t.Helper()
	store := NewImageStudioInputStore(t.TempDir(), maxBytes)
	uploads := make([]UploadedFile, len(images))
	for i := range images {
		uploads[i] = UploadedFile{Reader: bytes.NewReader(images[i]), ContentType: "image/png"}
	}
	var maskUpload *UploadedFile
	if mask != nil {
		maskUpload = &UploadedFile{Reader: bytes.NewReader(mask), ContentType: "image/png"}
	}
	staged, err := store.StageEditInputs(context.Background(), uploads, maskUpload)
	require.NoError(t, err)
	return store, staged
}

func storedImageStudioEditJob(staged *StagedEditInputs, expiresAt *time.Time) ImageStudioJob {
	return ImageStudioJob{
		ID: 39, UserID: 42, APIKeyID: 41, Mode: ImageStudioJobModeEdit, Status: ImageStudioJobStatusQueued,
		RequestPayload:  json.RawMessage(`{"model":"gpt-image-2","prompt":"edit"}`),
		InputImagePaths: append([]string(nil), staged.ImagePaths...),
		InputMaskPath:   cloneImageStudioString(staged.MaskPath),
		InputExpiresAt:  expiresAt,
		MaxAttempts:     3,
	}
}

func validImageStudioWorkerAPIKey() *APIKey {
	groupID := int64(12)
	return &APIKey{
		ID: 41, UserID: 42, Status: StatusAPIKeyActive, GroupID: &groupID,
		Group: &Group{ID: groupID, Platform: PlatformOpenAI, AllowImageGeneration: true, RateMultiplier: 1},
		User:  &User{ID: 42, Status: StatusActive},
	}
}

func newImageStudioWorkerTestService(
	repo ImageStudioJobRepository,
	store ImageStudioInputStorage,
	apiKeys *imageStudioAPIKeyRepoStub,
	executor imageStudioJobExecutor,
	now time.Time,
) *ImageStudioJobService {
	settings := NewSettingService(&imageStudioCreateSettingRepoStub{values: map[string]string{
		SettingKeyImageStudioAvailableGroupIDs: `[12]`,
	}}, &config.Config{})
	svc := NewImageStudioJobService(repo, settings, store, func() time.Time { return now })
	svc.SetRuntimeDependencies(nil, NewAPIKeyService(apiKeys, nil, nil, nil, nil, nil, nil), nil, nil)
	svc.executor = executor
	return svc
}

type imageStudioWorkerStorageStub struct {
	ImageStudioInputStorage
	opened      *OpenedEditInputs
	openErr     error
	openCalls   int
	removeErr   error
	removeCalls int
}

type imageStudioRemoveFailStorage struct {
	ImageStudioInputStorage
	err error
}

func (s *imageStudioRemoveFailStorage) RemoveInputs([]string, *string) error {
	return s.err
}

func (s *imageStudioWorkerStorageStub) OpenInputs([]string, *string) (*OpenedEditInputs, error) {
	s.openCalls++
	return s.opened, s.openErr
}

func (s *imageStudioWorkerStorageStub) RemoveInputs([]string, *string) error {
	s.removeCalls++
	return s.removeErr
}

type recordingImageStudioJobExecutor struct {
	err               error
	outcome           *imageStudioForwardOutcome
	closeBeforeReturn bool
	calls             int
	editInputs        *OpenedEditInputs
	imagePaths        []string
	maskPath          *string
	openFiles         []*os.File
	handlesLive       bool
	payload           []byte
}

func (e *recordingImageStudioJobExecutor) Execute(_ context.Context, _ ImageStudioJob, _ *APIKey, input *imageStudioExecutionInput) (*imageStudioForwardOutcome, error) {
	e.calls++
	e.editInputs = input.EditInputs
	e.payload = append([]byte(nil), input.Payload...)
	e.handlesLive = true
	if input.EditInputs != nil {
		for i := range input.EditInputs.Images {
			opened := input.EditInputs.Images[i]
			e.imagePaths = append(e.imagePaths, opened.Path)
			e.openFiles = append(e.openFiles, opened.File)
			if _, err := opened.File.Stat(); err != nil {
				e.handlesLive = false
			}
		}
		if input.EditInputs.Mask != nil {
			e.maskPath = cloneImageStudioString(&input.EditInputs.Mask.Path)
			e.openFiles = append(e.openFiles, input.EditInputs.Mask.File)
			if _, err := input.EditInputs.Mask.File.Stat(); err != nil {
				e.handlesLive = false
			}
		}
	}
	if e.closeBeforeReturn && input.EditInputs != nil {
		_ = input.EditInputs.Close()
	}
	return e.outcome, e.err
}

type blockingImageStudioJobExecutor struct {
	entered chan struct{}
	release chan struct{}
	err     error
	calls   int
}

func (e *blockingImageStudioJobExecutor) Execute(context.Context, ImageStudioJob, *APIKey, *imageStudioExecutionInput) (*imageStudioForwardOutcome, error) {
	e.calls++
	close(e.entered)
	<-e.release
	return nil, e.err
}

func TestImageStudioJobServiceGenerationBypassesInputStorage(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	store := &imageStudioWorkerStorageStub{openErr: errors.New("must not open generation input")}
	repo := &imageStudioWorkerRepoStub{}
	apiKeys := &imageStudioAPIKeyRepoStub{apiKey: validImageStudioWorkerAPIKey()}
	executor := &recordingImageStudioJobExecutor{err: errors.New("stop after capture")}
	svc := newImageStudioWorkerTestService(repo, store, apiKeys, executor, now)

	svc.processJob(context.Background(), ImageStudioJob{
		ID: 39, UserID: 42, APIKeyID: 41, Mode: ImageStudioJobModeGenerate, Status: ImageStudioJobStatusQueued,
		RequestPayload: json.RawMessage(`{"model":"gpt-image-2","prompt":"draw"}`),
	})

	require.Zero(t, store.openCalls)
	require.Equal(t, 1, executor.calls)
	require.Nil(t, executor.editInputs)
}

func TestImageStudioJobServiceWorkerPausesClaimsUntilStorageRecovers(t *testing.T) {
	prober := &imageStudioInputStorageProberStub{errors: []error{errors.New("mount unavailable"), nil}}
	health := NewImageStudioInputStorageHealth(prober, time.Minute)
	require.Error(t, health.Probe(context.Background()))
	repo := &imageStudioWorkerHealthRepoStub{}
	svc := NewImageStudioJobService(repo, nil, nil, time.Now, health)

	svc.drainQueueOnce(context.Background())
	require.Zero(t, repo.listCalls)
	require.Zero(t, repo.markRunningCalls)
	require.Zero(t, repo.claimSettlingCalls)

	require.NoError(t, health.Probe(context.Background()))
	svc.drainQueueOnce(context.Background())
	require.Equal(t, 1, repo.listCalls)
	require.Zero(t, repo.markRunningCalls)
	require.Zero(t, repo.claimSettlingCalls)
}

func TestImageStudioJobServiceRechecksStorageHealthAtExecutionAndSettlementClaims(t *testing.T) {
	health := NewImageStudioInputStorageHealth(&imageStudioInputStorageProberStub{errors: []error{errors.New("mount unavailable")}}, time.Minute)
	require.Error(t, health.Probe(context.Background()))
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	repo := &imageStudioWorkerRepoStub{claimSettling: true}
	executor := &recordingImageStudioJobExecutor{}
	svc := newImageStudioWorkerTestService(repo, nil, &imageStudioAPIKeyRepoStub{apiKey: validImageStudioWorkerAPIKey()}, executor, now)
	svc.SetInputStorageHealth(health)

	svc.processJob(context.Background(), ImageStudioJob{
		ID: 39, UserID: 42, APIKeyID: 41, Mode: ImageStudioJobModeGenerate, Status: ImageStudioJobStatusQueued,
		RequestPayload: json.RawMessage(`{"model":"gpt-image-2"}`),
	})
	svc.processJob(context.Background(), ImageStudioJob{
		ID: 40, UserID: 42, APIKeyID: 41, Status: ImageStudioJobStatusSettling,
	})

	require.Zero(t, repo.markRunningCalls)
	require.Zero(t, repo.claimSettlingCalls)
	require.Zero(t, executor.calls)
}

func TestImageStudioJobServicePeriodicProbeRecoveryResumesExactlyOneClaim(t *testing.T) {
	prober := &imageStudioInputStorageProberStub{errors: []error{errors.New("mount unavailable"), nil}}
	health := NewImageStudioInputStorageHealth(prober, time.Minute)
	healthTicker := &imageStudioInputStorageHealthTickerStub{ticks: make(chan time.Time, 1)}
	health.newTicker = func(time.Duration) imageStudioInputStorageHealthTicker { return healthTicker }
	baseRepo := &imageStudioWorkerRepoStub{}
	repo := &imageStudioWorkerAutoRecoveryRepo{
		imageStudioWorkerRepoStub: baseRepo,
		jobs: []ImageStudioJob{{
			ID: 39, UserID: 42, APIKeyID: 41, Mode: ImageStudioJobModeGenerate, Status: ImageStudioJobStatusQueued,
			RequestPayload: json.RawMessage(`{"model":"gpt-image-2"}`),
		}},
		claims: make(chan struct{}, 2),
	}
	executor := &blockingImageStudioJobExecutor{
		entered: make(chan struct{}), release: make(chan struct{}), err: errors.New("stop after capture"),
	}
	svc := newImageStudioWorkerTestService(repo, nil, &imageStudioAPIKeyRepoStub{apiKey: validImageStudioWorkerAPIKey()}, executor, time.Now())
	svc.SetInputStorageHealth(health)
	queueTicker := &imageStudioInputStorageHealthTickerStub{ticks: make(chan time.Time, 2)}
	svc.newQueueTicker = func(time.Duration) imageStudioInputStorageHealthTicker { return queueTicker }
	svc.Start()
	defer svc.Stop()
	require.False(t, health.Available())

	healthTicker.ticks <- time.Now()
	require.Eventually(t, health.Available, time.Second, time.Millisecond)
	queueTicker.ticks <- time.Now()
	select {
	case <-repo.claims:
	case <-time.After(time.Second):
		t.Fatal("worker did not resume claim after storage recovered")
	}
	<-executor.entered
	queueTicker.ticks <- time.Now()
	select {
	case <-repo.claims:
		t.Fatal("recovered job was claimed more than once")
	case <-time.After(25 * time.Millisecond):
	}
	close(executor.release)
}

func TestImageStudioJobServiceUnhealthyProbeDoesNotCancelAlreadyRunningWork(t *testing.T) {
	health := NewImageStudioInputStorageHealth(&imageStudioInputStorageProberStub{errors: []error{nil, errors.New("mount unavailable")}}, time.Minute)
	require.NoError(t, health.Probe(context.Background()))
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	repo := &imageStudioWorkerRepoStub{}
	apiKeys := &imageStudioAPIKeyRepoStub{apiKey: validImageStudioWorkerAPIKey()}
	executor := &blockingImageStudioJobExecutor{
		entered: make(chan struct{}), release: make(chan struct{}), err: errors.New("stop after capture"),
	}
	svc := newImageStudioWorkerTestService(repo, nil, apiKeys, executor, now)
	svc.SetInputStorageHealth(health)

	done := make(chan struct{})
	go func() {
		svc.processJob(context.Background(), ImageStudioJob{
			ID: 39, UserID: 42, APIKeyID: 41, Mode: ImageStudioJobModeGenerate, Status: ImageStudioJobStatusQueued,
			RequestPayload: json.RawMessage(`{"model":"gpt-image-2","prompt":"draw"}`),
		})
		close(done)
	}()
	<-executor.entered
	require.Error(t, health.Probe(context.Background()))
	close(executor.release)
	<-done

	require.Equal(t, 1, executor.calls)
	require.Equal(t, 1, repo.markRunningCalls)
}

func TestImageStudioJobServiceMaterializedLegacyInputsEnterStoredExecutionPath(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	store := NewImageStudioInputStore(t.TempDir(), 1<<20)
	repo := &imageStudioWorkerRepoStub{}
	apiKeys := &imageStudioAPIKeyRepoStub{apiKey: validImageStudioWorkerAPIKey()}
	executor := &recordingImageStudioJobExecutor{err: errors.New("stop after capture")}
	svc := newImageStudioWorkerTestService(repo, store, apiKeys, executor, now)
	dataURL := imageStudioLegacyDataURL("image/png", imageStudioTestPNG(t, 2, 2, false))

	svc.processJob(context.Background(), ImageStudioJob{
		ID: 39, UserID: 42, APIKeyID: 41, Mode: ImageStudioJobModeEdit, Status: ImageStudioJobStatusQueued,
		RequestPayload: json.RawMessage(`{"model":"gpt-image-2","prompt":"edit","images":[{"image_url":"` + dataURL + `"}]}`),
	})

	require.Equal(t, 1, repo.persistLegacyCalls)
	require.Equal(t, 1, executor.calls)
	require.Equal(t, repo.legacyPaths, executor.imagePaths)
	require.NotEmpty(t, executor.imagePaths)
	require.NotContains(t, string(executor.payload), "data:image/")
	require.NotContains(t, string(executor.payload), `"images"`)
}

func TestImageStudioJobServiceLegacyStorageFailureIsTerminalBeforeExecution(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	store := &imageStudioLegacyStorageStub{materializeErr: inputStorageError(errors.New("shared storage unavailable"))}
	repo := &imageStudioWorkerRepoStub{}
	apiKeys := &imageStudioAPIKeyRepoStub{apiKey: validImageStudioWorkerAPIKey()}
	executor := &recordingImageStudioJobExecutor{}
	svc := newImageStudioWorkerTestService(repo, store, apiKeys, executor, now)
	dataURL := imageStudioLegacyDataURL("image/png", imageStudioTestPNG(t, 2, 2, false))

	svc.processJob(context.Background(), ImageStudioJob{
		ID: 39, UserID: 42, APIKeyID: 41, Mode: ImageStudioJobModeEdit, Status: ImageStudioJobStatusQueued,
		RequestPayload: json.RawMessage(`{"model":"gpt-image-2","images":[{"image_url":"` + dataURL + `"}]}`),
	})

	require.Equal(t, 1, repo.markFailedCalls)
	require.Equal(t, ImageStudioInputCodeStorageUnavailable, repo.failedErrorCode)
	require.Zero(t, repo.markRetryableCalls)
	require.Zero(t, apiKeys.calls)
	require.Zero(t, executor.calls)
}

func TestClassifyImageStudioInputFailureDoesNotReclassifyLegacyOrProviderErrors(t *testing.T) {
	code, classified := classifyImageStudioInputFailure(legacyInputInvalidError(ErrImageStudioInputInvalid))
	require.False(t, classified)
	require.Empty(t, code)

	code, classified = classifyImageStudioInputFailure(&UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable})
	require.False(t, classified)
	require.Empty(t, code)

	code, classified = classifyImageStudioInputFailure(inputMissingError(os.ErrNotExist))
	require.True(t, classified)
	require.Equal(t, ImageStudioInputCodeMissing, code)
}

func TestImageStudioJobServiceMaterializesLegacyInputsAndContinuesWithRedactedJob(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	maskPath := "inputs/upload-legacy/mask.png"
	storage := &imageStudioLegacyStorageStub{staged: &StagedEditInputs{
		UploadID: "upload-legacy",
		ImagePaths: []string{
			"inputs/upload-legacy/image-01.png",
			"inputs/upload-legacy/image-02.webp",
		},
		MaskPath: &maskPath,
	}}
	repo := &imageStudioWorkerRepoStub{}
	svc := NewImageStudioJobService(repo, nil, storage, func() time.Time { return now })
	job := ImageStudioJob{
		ID:     39,
		Mode:   ImageStudioJobModeEdit,
		Status: ImageStudioJobStatusRunning,
		RequestPayload: json.RawMessage(`{
			"model":"gpt-image-2","prompt":"restore",
			"images":[{"image_url":"data:image/png;base64,first"},{"image_url":"data:image/webp;base64,second"}],
			"mask":{"image_url":"data:image/png;base64,mask"}
		}`),
	}

	materialized, terminal, err := svc.materializeLegacyJobInputs(context.Background(), &job)

	require.NoError(t, err)
	require.True(t, materialized)
	require.False(t, terminal, "successful materialization must continue normal worker execution")
	require.Equal(t, []string{"data:image/png;base64,first", "data:image/webp;base64,second"}, storage.images)
	require.NotNil(t, storage.mask)
	require.Equal(t, "data:image/png;base64,mask", *storage.mask)
	require.Equal(t, storage.staged.ImagePaths, repo.legacyPaths)
	require.Equal(t, storage.staged.MaskPath, repo.legacyMaskPath)
	require.Equal(t, now.Add(DefaultImageStudioInputRetentionHours*time.Hour), repo.legacyExpiresAt)
	require.JSONEq(t, `{"model":"gpt-image-2","prompt":"restore"}`, string(repo.legacyRedacted))
	require.Equal(t, storage.staged.ImagePaths, job.InputImagePaths)
	require.Equal(t, storage.staged.MaskPath, job.InputMaskPath)
	require.Equal(t, repo.legacyExpiresAt, *job.InputExpiresAt)
	require.JSONEq(t, string(repo.legacyRedacted), string(job.RequestPayload))
}

func TestImageStudioJobServiceLegacyPersistFailureRemovesMaterializedInputsAndJoinsCleanupError(t *testing.T) {
	persistFailure := errors.New("persist failed")
	removeFailure := errors.New("remove failed")
	valid := imageStudioLegacyDataURL("image/png", imageStudioTestPNG(t, 2, 2, false))
	tests := []struct {
		name      string
		removeErr error
	}{
		{name: "remove succeeds"},
		{name: "remove fails", removeErr: removeFailure},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewImageStudioInputStore(t.TempDir(), 1<<20)
			if tt.removeErr != nil {
				originalRemove := storage.removeAllInRoot
				removeCalls := 0
				storage.removeAllInRoot = func(root *os.Root, path string) error {
					removeCalls++
					if removeCalls == 1 {
						return tt.removeErr
					}
					return originalRemove(root, path)
				}
			}
			repo := &imageStudioWorkerRepoStub{persistLegacyErr: persistFailure, markStaleRunningChanged: true}
			svc := NewImageStudioJobService(repo, nil, storage, time.Now)
			job := ImageStudioJob{
				ID: 39, Mode: ImageStudioJobModeEdit, Status: ImageStudioJobStatusRunning,
				RequestPayload: json.RawMessage(`{"images":[{"image_url":"` + valid + `"}]}`),
			}

			materialized, terminal, err := svc.materializeLegacyJobInputs(context.Background(), &job)

			require.False(t, materialized)
			require.False(t, terminal)
			require.ErrorIs(t, err, persistFailure)
			require.Zero(t, repo.failLegacyCalls, "transient persistence failure must remain recoverable until stale")
			if tt.removeErr != nil {
				require.ErrorIs(t, err, removeFailure)
				require.Len(t, imageStudioInputDirs(t, storage.Root()), 1)
			} else {
				require.Empty(t, imageStudioInputDirs(t, storage.Root()))
			}

			svc.recoverStaleRunningJob(context.Background(), job)
			require.Equal(t, 1, repo.markStaleRunningCalls)
			if tt.removeErr != nil {
				result, cleanupErr := storage.CleanupOrphans(ImageStudioInputCleanupOptions{
					Now: time.Now().Add(2 * time.Hour), OrphanGrace: time.Hour, SpoolGrace: 5 * time.Minute, Limit: 50,
				})
				require.NoError(t, cleanupErr)
				require.Equal(t, 1, result.OrphanDirsDeleted)
				require.Empty(t, imageStudioInputDirs(t, storage.Root()))
			}
		})
	}
}

func TestImageStudioJobServiceInvalidLegacyInputFailsAtomicallyBeforeUpstream(t *testing.T) {
	valid := imageStudioLegacyDataURL("image/png", imageStudioTestPNG(t, 2, 2, false))
	tests := []struct {
		name    string
		payload json.RawMessage
	}{
		{name: "zero images", payload: json.RawMessage(`{"model":"gpt-image-2","images":[],"mask":{"image_url":"` + valid + `"}}`)},
		{name: "five images", payload: json.RawMessage(`{"images":[{"image_url":"` + valid + `"},{"image_url":"` + valid + `"},{"image_url":"` + valid + `"},{"image_url":"` + valid + `"},{"image_url":"` + valid + `"}]}`)},
		{name: "non string image URL", payload: json.RawMessage(`{"images":[{"image_url":42}]}`)},
		{name: "non data URL", payload: json.RawMessage(`{"images":[{"image_url":"plain-base64"}]}`)},
		{name: "bad base64", payload: json.RawMessage(`{"images":[{"image_url":"data:image/png;base64,%%%"}]}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &imageStudioWorkerRepoStub{}
			store := NewImageStudioInputStore(t.TempDir(), 1<<20)
			svc := NewImageStudioJobService(repo, nil, store, time.Now)

			svc.processJob(context.Background(), ImageStudioJob{
				ID: 39, Mode: ImageStudioJobModeEdit, Status: ImageStudioJobStatusQueued,
				RequestPayload: tt.payload,
			})

			require.Equal(t, 1, repo.failLegacyCalls)
			require.Equal(t, ImageStudioInputCodeLegacyInvalid, repo.failLegacyCode)
			require.NotContains(t, string(repo.failLegacyRedacted), `"images"`)
			require.NotContains(t, string(repo.failLegacyRedacted), `"mask"`)
			require.Zero(t, repo.markFailedCalls, "invalid legacy input must use the atomic terminal update")
			require.Empty(t, imageStudioInputDirs(t, store.Root()))
		})
	}
}

func TestImageStudioJobServiceIgnoresNonLegacyAndAlreadyMaterializedJobs(t *testing.T) {
	storage := &imageStudioLegacyStorageStub{}
	repo := &imageStudioWorkerRepoStub{}
	svc := NewImageStudioJobService(repo, nil, storage, time.Now)
	jobs := []ImageStudioJob{
		{ID: 1, Mode: ImageStudioJobModeGenerate, RequestPayload: json.RawMessage(`{"images":[]}`)},
		{ID: 2, Mode: ImageStudioJobModeEdit, RequestPayload: json.RawMessage(`{"model":"gpt-image-2"}`)},
		{ID: 3, Mode: ImageStudioJobModeEdit, InputImagePaths: []string{"inputs/upload-ready/image-01.png"}, RequestPayload: json.RawMessage(`{"images":[]}`)},
	}

	for i := range jobs {
		materialized, terminal, err := svc.materializeLegacyJobInputs(context.Background(), &jobs[i])
		require.NoError(t, err)
		require.False(t, materialized)
		require.False(t, terminal)
	}
	require.Zero(t, storage.materializeCalls)
	require.Zero(t, repo.persistLegacyCalls)
}

func TestImageStudioJobServiceSettleUsesUnifiedUsageAndActualCost(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	gateway := newOpenAIRecordUsageServiceWithBillingRepoForTest(
		usageRepo,
		billingRepo,
		&openAIRecordUsageUserRepoStub{},
		&openAIRecordUsageSubRepoStub{},
		nil,
	)
	gateway.accountRepo = &imageStudioAccountRepoStub{account: &Account{ID: 77, Platform: PlatformOpenAI}}

	groupID := int64(12)
	apiKey := &APIKey{
		ID:      41,
		UserID:  42,
		GroupID: &groupID,
		Group: &Group{
			ID:               groupID,
			Platform:         PlatformOpenAI,
			RateMultiplier:   1,
			SubscriptionType: SubscriptionTypeStandard,
		},
		User: &User{ID: 42},
	}
	settlementPayload, err := marshalImageStudioSettlementPayload(
		77,
		&OpenAIForwardResult{
			RequestID:  "volatile-upstream-id",
			Model:      "gpt-image-1",
			ImageCount: 1,
			ImageSize:  "1024x1024",
		},
		ChannelUsageFields{ChannelID: 8, OriginalModel: "image-alias", ChannelMappedModel: "gpt-image-1"},
		"/v1/images/generations",
		"/v1/images/generations",
	)
	require.NoError(t, err)

	repo := &imageStudioWorkerRepoStub{}
	svc := &ImageStudioJobService{repo: repo, openAIGateway: gateway}
	job := ImageStudioJob{
		ID:                39,
		UserID:            42,
		APIKeyID:          41,
		RequestPayload:    json.RawMessage(`{"model":"gpt-image-1","prompt":"draw"}`),
		SettlementPayload: settlementPayload,
		OriginalPath:      "/tmp/39/original.png",
		ThumbnailPath:     "/tmp/39/thumbnail.jpg",
		MIMEType:          "image/png",
		FileSizeBytes:     123,
		Width:             1024,
		Height:            1024,
	}

	err = svc.settleJob(context.Background(), job, apiKey)

	require.NoError(t, err)
	require.Equal(t, 1, billingRepo.calls)
	require.Equal(t, "image-studio-job:39", billingRepo.lastCmd.RequestID)
	require.Equal(t, HashUsageRequestPayload(job.RequestPayload), billingRepo.lastCmd.RequestPayloadHash)
	require.Equal(t, int64(77), billingRepo.lastCmd.AccountID)
	require.Equal(t, 1, repo.markSucceededCalls)
	require.Positive(t, repo.chargedAmountUSD)
	require.InDelta(t, usageRepo.lastLog.ActualCost, repo.chargedAmountUSD, 1e-12)
	require.Equal(t, int64(8), *usageRepo.lastLog.ChannelID)
}

func TestImageStudioJobServiceSettleResolvesActiveSubscription(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	gateway := newOpenAIRecordUsageServiceWithBillingRepoForTest(
		usageRepo,
		billingRepo,
		&openAIRecordUsageUserRepoStub{},
		&openAIRecordUsageSubRepoStub{},
		nil,
	)
	gateway.accountRepo = &imageStudioAccountRepoStub{account: &Account{ID: 77, Platform: PlatformOpenAI}}

	groupID := int64(12)
	apiKey := &APIKey{
		ID:      41,
		UserID:  42,
		GroupID: &groupID,
		Group: &Group{
			ID:               groupID,
			Platform:         PlatformOpenAI,
			RateMultiplier:   1,
			SubscriptionType: SubscriptionTypeSubscription,
		},
		User: &User{ID: 42},
	}
	subscription := &UserSubscription{ID: 91, UserID: 42, GroupID: groupID}
	resolver := &imageStudioSubscriptionResolverStub{subscription: subscription}
	settlementPayload, err := marshalImageStudioSettlementPayload(77, &OpenAIForwardResult{
		Model:      "gpt-image-1",
		ImageCount: 1,
		ImageSize:  "1024x1024",
	}, ChannelUsageFields{}, "/v1/images/generations", "/v1/images/generations")
	require.NoError(t, err)

	repo := &imageStudioWorkerRepoStub{}
	svc := &ImageStudioJobService{
		repo:                 repo,
		openAIGateway:        gateway,
		subscriptionResolver: resolver,
	}
	err = svc.settleJob(context.Background(), ImageStudioJob{
		ID:                39,
		UserID:            42,
		APIKeyID:          41,
		RequestPayload:    json.RawMessage(`{"model":"gpt-image-1"}`),
		SettlementPayload: settlementPayload,
	}, apiKey)

	require.NoError(t, err)
	require.Equal(t, 1, resolver.calls)
	require.Equal(t, int64(42), resolver.userID)
	require.Equal(t, groupID, resolver.groupID)
	require.NotNil(t, billingRepo.lastCmd.SubscriptionID)
	require.Equal(t, int64(91), *billingRepo.lastCmd.SubscriptionID)
	require.Equal(t, BillingTypeSubscription, usageRepo.lastLog.BillingType)
}

func TestImageStudioJobServiceSettleUsesPersistedSubscriptionAfterExpiry(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	gateway := newOpenAIRecordUsageServiceWithBillingRepoForTest(
		usageRepo,
		billingRepo,
		&openAIRecordUsageUserRepoStub{},
		&openAIRecordUsageSubRepoStub{},
		nil,
	)
	gateway.accountRepo = &imageStudioAccountRepoStub{account: &Account{ID: 77, Platform: PlatformOpenAI}}

	groupID := int64(12)
	apiKey := &APIKey{
		ID:      41,
		UserID:  42,
		GroupID: &groupID,
		Group: &Group{
			ID:               groupID,
			Platform:         PlatformOpenAI,
			RateMultiplier:   1,
			SubscriptionType: SubscriptionTypeSubscription,
		},
		User: &User{ID: 42},
	}
	expiredSubscription := &UserSubscription{
		ID:        91,
		UserID:    42,
		GroupID:   groupID,
		Status:    SubscriptionStatusExpired,
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	resolver := &imageStudioSubscriptionResolverStub{subscription: expiredSubscription}
	settlementPayload, err := marshalImageStudioSettlementPayloadWithSubscription(
		77,
		&OpenAIForwardResult{Model: "gpt-image-1", ImageCount: 1, ImageSize: "1024x1024"},
		ChannelUsageFields{},
		"/v1/images/generations",
		"/v1/images/generations",
		expiredSubscription,
	)
	require.NoError(t, err)

	repo := &imageStudioWorkerRepoStub{}
	svc := &ImageStudioJobService{repo: repo, openAIGateway: gateway, subscriptionResolver: resolver}
	err = svc.settleJob(context.Background(), ImageStudioJob{
		ID:                39,
		UserID:            42,
		APIKeyID:          41,
		RequestPayload:    json.RawMessage(`{"model":"gpt-image-1"}`),
		SettlementPayload: settlementPayload,
	}, apiKey)

	require.NoError(t, err)
	require.Equal(t, 1, resolver.getByIDCalls)
	require.Zero(t, resolver.calls)
	require.NotNil(t, billingRepo.lastCmd.SubscriptionID)
	require.Equal(t, expiredSubscription.ID, *billingRepo.lastCmd.SubscriptionID)
}

func TestImageStudioJobServiceSettlingRetrySkipsUpstreamAndRequeuesCompletionFailure(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	gateway := newOpenAIRecordUsageServiceWithBillingRepoForTest(
		usageRepo,
		billingRepo,
		&openAIRecordUsageUserRepoStub{},
		&openAIRecordUsageSubRepoStub{},
		nil,
	)
	gateway.accountRepo = &imageStudioAccountRepoStub{account: &Account{ID: 77, Platform: PlatformOpenAI}}

	groupID := int64(12)
	apiKey := &APIKey{
		ID:      41,
		UserID:  42,
		Status:  StatusAPIKeyActive,
		GroupID: &groupID,
		Group: &Group{
			ID:               groupID,
			Platform:         PlatformOpenAI,
			RateMultiplier:   1,
			SubscriptionType: SubscriptionTypeStandard,
		},
		User: &User{ID: 42},
	}
	apiKeyService := NewAPIKeyService(&imageStudioAPIKeyRepoStub{apiKey: apiKey}, nil, nil, nil, nil, nil, nil)
	settlementPayload, err := marshalImageStudioSettlementPayload(77, &OpenAIForwardResult{
		Model:      "gpt-image-1",
		ImageCount: 1,
		ImageSize:  "1024x1024",
	}, ChannelUsageFields{}, "/v1/images/generations", "/v1/images/generations")
	require.NoError(t, err)

	repo := &imageStudioWorkerRepoStub{claimSettling: true, markSucceededErr: errors.New("db unavailable")}
	svc := &ImageStudioJobService{repo: repo, openAIGateway: gateway, apiKeyService: apiKeyService}
	svc.processJob(context.Background(), ImageStudioJob{
		ID:                39,
		UserID:            42,
		APIKeyID:          41,
		Status:            ImageStudioJobStatusSettling,
		RequestPayload:    json.RawMessage(`{"model":"gpt-image-1"}`),
		SettlementPayload: settlementPayload,
		MaxAttempts:       3,
	})

	require.Zero(t, repo.markRunningCalls, "settling retries must not re-enter upstream generation")
	require.Equal(t, 1, repo.claimSettlingCalls)
	require.Equal(t, 1, billingRepo.calls)
	require.Equal(t, 1, repo.markSettlementRetryableCalls)
	require.Equal(t, "settlement_failed", repo.retryErrorCode)
}

func TestImageStudioJobServiceExistingReceiptCompletesWithoutMutableDependencies(t *testing.T) {
	receipt := &UsageLog{
		ID:         701,
		APIKeyID:   41,
		RequestID:  "image-studio-job:39",
		ActualCost: 0.37,
	}
	usageRepo := &openAIRecordUsageLogRepoStub{receipt: receipt}
	billingRepo := &openAIRecordUsageBillingRepoStub{}
	gateway := newOpenAIRecordUsageServiceWithBillingRepoForTest(
		usageRepo,
		billingRepo,
		&openAIRecordUsageUserRepoStub{},
		&openAIRecordUsageSubRepoStub{},
		nil,
	)
	repo := &imageStudioWorkerRepoStub{claimSettling: true}
	svc := &ImageStudioJobService{repo: repo, openAIGateway: gateway}

	svc.processJob(context.Background(), ImageStudioJob{
		ID:       39,
		UserID:   42,
		APIKeyID: 41,
		Status:   ImageStudioJobStatusSettling,
	})

	require.Equal(t, 1, usageRepo.lookupCalls)
	require.Zero(t, billingRepo.calls)
	require.Equal(t, 1, repo.markSucceededCalls)
	require.InDelta(t, receipt.ActualCost, repo.chargedAmountUSD, 1e-12)
}

func TestImageStudioJobServiceStaleRunningIsFailedWithoutUpstreamReplay(t *testing.T) {
	repo := &imageStudioWorkerRepoStub{markStaleRunningChanged: true}
	svc := &ImageStudioJobService{repo: repo}

	svc.processJob(context.Background(), ImageStudioJob{
		ID:     39,
		Status: ImageStudioJobStatusRunning,
	})

	require.Equal(t, 1, repo.markStaleRunningCalls)
	require.Zero(t, repo.markRunningCalls)
}

func TestImageStudioJobServiceRunningHeartbeatRefreshesUntilStopped(t *testing.T) {
	repo := &imageStudioWorkerRepoStub{heartbeatCh: make(chan struct{}, 2)}
	svc := &ImageStudioJobService{repo: repo}

	stop := svc.startImageStudioJobHeartbeat(context.Background(), 39, time.Millisecond)
	select {
	case <-repo.heartbeatCh:
	case <-time.After(time.Second):
		t.Fatal("heartbeat was not refreshed")
	}
	stop()
	callsAfterStop := repo.heartbeatCalls
	time.Sleep(5 * time.Millisecond)
	require.Equal(t, callsAfterStop, repo.heartbeatCalls)
}

func TestImageStudioJobServiceTerminalSettlementErrorsAreFailed(t *testing.T) {
	groupID := int64(12)
	validAPIKey := &APIKey{
		ID:      41,
		UserID:  42,
		Status:  StatusAPIKeyActive,
		GroupID: &groupID,
		Group:   &Group{ID: groupID, Platform: PlatformOpenAI, RateMultiplier: 1},
		User:    &User{ID: 42},
	}
	validPayload, err := marshalImageStudioSettlementPayload(
		77,
		&OpenAIForwardResult{Model: "gpt-image-1", ImageCount: 1, ImageSize: "1K"},
		ChannelUsageFields{},
		"/v1/images/generations",
		"/v1/images/generations",
	)
	require.NoError(t, err)
	selectedSubscription := &UserSubscription{ID: 91, UserID: 42, GroupID: groupID}
	subscriptionPayload, err := marshalImageStudioSettlementPayloadWithSubscription(
		77,
		&OpenAIForwardResult{Model: "gpt-image-1", ImageCount: 1, ImageSize: "1K"},
		ChannelUsageFields{},
		"/v1/images/generations",
		"/v1/images/generations",
		selectedSubscription,
	)
	require.NoError(t, err)

	tests := []struct {
		name            string
		payload         json.RawMessage
		apiKey          *APIKey
		apiKeyErr       error
		account         *Account
		accountErr      error
		subscription    *UserSubscription
		subscriptionErr error
	}{
		{name: "malformed payload", payload: json.RawMessage(`{"version":999}`), apiKey: validAPIKey, account: &Account{ID: 77}},
		{name: "missing api key", payload: validPayload, apiKeyErr: ErrAPIKeyNotFound, account: &Account{ID: 77}},
		{name: "missing account", payload: validPayload, apiKey: validAPIKey, accountErr: ErrAccountNotFound},
		{name: "missing persisted subscription", payload: subscriptionPayload, apiKey: validAPIKey, account: &Account{ID: 77}, subscriptionErr: ErrSubscriptionNotFound},
		{name: "subscription ownership mismatch", payload: subscriptionPayload, apiKey: validAPIKey, account: &Account{ID: 77}, subscription: &UserSubscription{ID: 91, UserID: 999, GroupID: groupID}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{}
			gateway := newOpenAIRecordUsageServiceWithBillingRepoForTest(
				usageRepo,
				&openAIRecordUsageBillingRepoStub{},
				&openAIRecordUsageUserRepoStub{},
				&openAIRecordUsageSubRepoStub{},
				nil,
			)
			gateway.accountRepo = &imageStudioAccountRepoStub{account: tt.account, err: tt.accountErr}
			apiKeyService := NewAPIKeyService(&imageStudioAPIKeyRepoStub{apiKey: tt.apiKey, err: tt.apiKeyErr}, nil, nil, nil, nil, nil, nil)
			repo := &imageStudioWorkerRepoStub{claimSettling: true}
			resolver := &imageStudioSubscriptionResolverStub{subscription: tt.subscription, err: tt.subscriptionErr}
			svc := &ImageStudioJobService{repo: repo, openAIGateway: gateway, apiKeyService: apiKeyService, subscriptionResolver: resolver}

			svc.processJob(context.Background(), ImageStudioJob{
				ID:                39,
				UserID:            42,
				APIKeyID:          41,
				Status:            ImageStudioJobStatusSettling,
				SettlementPayload: tt.payload,
			})

			require.Equal(t, 1, repo.markSettlementFailedCalls)
			require.Zero(t, repo.markSettlementRetryableCalls)
			require.Equal(t, "settlement_unrecoverable", repo.failedErrorCode)
		})
	}
}

func TestImageStudioJobServiceRetryableSettlementErrorStaysSettling(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{lookupErr: errors.New("database unavailable")}
	gateway := newOpenAIRecordUsageServiceWithBillingRepoForTest(
		usageRepo,
		&openAIRecordUsageBillingRepoStub{},
		&openAIRecordUsageUserRepoStub{},
		&openAIRecordUsageSubRepoStub{},
		nil,
	)
	repo := &imageStudioWorkerRepoStub{claimSettling: true}
	svc := &ImageStudioJobService{repo: repo, openAIGateway: gateway}

	svc.processJob(context.Background(), ImageStudioJob{ID: 39, APIKeyID: 41, Status: ImageStudioJobStatusSettling})

	require.Zero(t, repo.markSettlementFailedCalls)
	require.Equal(t, 1, repo.markSettlementRetryableCalls)
}

func TestImageStudioJobServiceMarkSettlingFailureRemovesUncommittedAssets(t *testing.T) {
	jobDir := filepath.Join(t.TempDir(), "39")
	require.NoError(t, os.MkdirAll(jobDir, 0o755))
	originalPath := filepath.Join(jobDir, "original.png")
	thumbnailPath := filepath.Join(jobDir, "thumbnail.jpg")
	require.NoError(t, os.WriteFile(originalPath, []byte("original"), 0o600))
	require.NoError(t, os.WriteFile(thumbnailPath, []byte("thumbnail"), 0o600))

	repo := &imageStudioWorkerRepoStub{markSettlingErr: errors.New("database unavailable")}
	svc := &ImageStudioJobService{repo: repo}
	err := svc.markImageStudioSettling(
		context.Background(),
		39,
		json.RawMessage(`{"version":1}`),
		originalPath,
		thumbnailPath,
		"image/png",
		int64(len("original")),
		1,
		1,
		time.Now(),
	)

	require.EqualError(t, err, "database unavailable")
	require.Equal(t, 1, repo.markSettlingCalls)
	require.NoDirExists(t, jobDir)
}

type imageStudioWorkerRepoStub struct {
	ImageStudioJobRepository
	claimSettling                bool
	markSucceededErr             error
	markSettlingErr              error
	markRunningCalls             int
	markSettlingCalls            int
	claimSettlingCalls           int
	markSucceededCalls           int
	markSettlementRetryableCalls int
	chargedAmountUSD             float64
	retryErrorCode               string
	retryErrorMessage            string
	markStaleRunningChanged      bool
	markStaleRunningCalls        int
	heartbeatCalls               int
	heartbeatCh                  chan struct{}
	markSettlementFailedCalls    int
	failedErrorCode              string
	failedErrorMessage           string
	persistLegacyCalls           int
	persistLegacyErr             error
	legacyPaths                  []string
	legacyMaskPath               *string
	legacyRedacted               json.RawMessage
	legacyExpiresAt              time.Time
	failLegacyCalls              int
	failLegacyCode               string
	failLegacyRedacted           json.RawMessage
	markFailedCalls              int
	markRetryableCalls           int
	retryAt                      time.Time
	markInputsDeletedCalls       int
	markInputsDeletedErr         error
	failExpiredRunningChanged    bool
	failExpiredRunningCalls      int
}

type imageStudioWorkerHealthRepoStub struct {
	ImageStudioJobRepository
	listCalls          int
	markRunningCalls   int
	claimSettlingCalls int
}

type imageStudioWorkerAutoRecoveryRepo struct {
	*imageStudioWorkerRepoStub
	mu     sync.Mutex
	jobs   []ImageStudioJob
	claims chan struct{}
}

func (r *imageStudioWorkerAutoRecoveryRepo) ListRunnableJobs(context.Context, int) ([]ImageStudioJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	jobs := append([]ImageStudioJob(nil), r.jobs...)
	r.jobs = nil
	return jobs, nil
}

func (r *imageStudioWorkerAutoRecoveryRepo) MarkRunning(ctx context.Context, id int64, startedAt time.Time) (bool, error) {
	acquired, err := r.imageStudioWorkerRepoStub.MarkRunning(ctx, id, startedAt)
	if err == nil && acquired {
		r.claims <- struct{}{}
	}
	return acquired, err
}

func (r *imageStudioWorkerHealthRepoStub) ListRunnableJobs(context.Context, int) ([]ImageStudioJob, error) {
	r.listCalls++
	return nil, nil
}

func (r *imageStudioWorkerHealthRepoStub) MarkRunning(context.Context, int64, time.Time) (bool, error) {
	r.markRunningCalls++
	return true, nil
}

func (r *imageStudioWorkerHealthRepoStub) ClaimSettling(context.Context, int64, time.Time, time.Time) (bool, error) {
	r.claimSettlingCalls++
	return true, nil
}

func (r *imageStudioWorkerRepoStub) FailExpiredRunningInputs(context.Context, int64, time.Time) (bool, error) {
	r.failExpiredRunningCalls++
	return r.failExpiredRunningChanged, nil
}

func (r *imageStudioWorkerRepoStub) MarkInputsDeleted(context.Context, int64, time.Time) error {
	r.markInputsDeletedCalls++
	return r.markInputsDeletedErr
}

func (r *imageStudioWorkerRepoStub) PersistLegacyInputs(_ context.Context, _ int64, paths []string, mask *string, redacted json.RawMessage, expiresAt time.Time) error {
	r.persistLegacyCalls++
	r.legacyPaths = append([]string(nil), paths...)
	r.legacyMaskPath = cloneImageStudioString(mask)
	r.legacyRedacted = append(json.RawMessage(nil), redacted...)
	r.legacyExpiresAt = expiresAt
	return r.persistLegacyErr
}

func (r *imageStudioWorkerRepoStub) FailLegacyInputs(_ context.Context, _ int64, redacted json.RawMessage, _ time.Time) error {
	r.failLegacyCalls++
	r.failLegacyCode = ImageStudioInputCodeLegacyInvalid
	r.failLegacyRedacted = append(json.RawMessage(nil), redacted...)
	return nil
}

func (r *imageStudioWorkerRepoStub) MarkFailed(_ context.Context, _ int64, _ time.Time, code, message string) error {
	r.markFailedCalls++
	r.failedErrorCode = code
	r.failedErrorMessage = message
	return nil
}

func (r *imageStudioWorkerRepoStub) MarkRetryable(_ context.Context, _ int64, nextAttemptAt time.Time, code, message string) error {
	r.markRetryableCalls++
	r.retryAt = nextAttemptAt
	r.retryErrorCode = code
	r.retryErrorMessage = message
	return nil
}

func (r *imageStudioWorkerRepoStub) UpdateHeartbeat(context.Context, int64, time.Time) error {
	r.heartbeatCalls++
	if r.heartbeatCh != nil {
		select {
		case r.heartbeatCh <- struct{}{}:
		default:
		}
	}
	return nil
}

type imageStudioLegacyStorageStub struct {
	ImageStudioInputStorage
	staged           *StagedEditInputs
	materializeErr   error
	removeErr        error
	materializeCalls int
	images           []string
	mask             *string
	removedPaths     []string
	removedMask      *string
}

func (s *imageStudioLegacyStorageStub) MaterializeLegacy(_ context.Context, images []string, mask *string) (*StagedEditInputs, error) {
	s.materializeCalls++
	s.images = append([]string(nil), images...)
	s.mask = cloneImageStudioString(mask)
	return s.staged, s.materializeErr
}

func (s *imageStudioLegacyStorageStub) RemoveInputs(paths []string, mask *string) error {
	s.removedPaths = append([]string(nil), paths...)
	s.removedMask = cloneImageStudioString(mask)
	return s.removeErr
}

func (r *imageStudioWorkerRepoStub) MarkStaleRunningFailed(context.Context, int64, time.Time, time.Time) (bool, error) {
	r.markStaleRunningCalls++
	return r.markStaleRunningChanged, nil
}

func (r *imageStudioWorkerRepoStub) MarkRunning(context.Context, int64, time.Time) (bool, error) {
	r.markRunningCalls++
	return true, nil
}

func (r *imageStudioWorkerRepoStub) MarkSettling(context.Context, int64, json.RawMessage, string, string, string, int64, int, int, time.Time) error {
	r.markSettlingCalls++
	return r.markSettlingErr
}

func (r *imageStudioWorkerRepoStub) ClaimSettling(context.Context, int64, time.Time, time.Time) (bool, error) {
	r.claimSettlingCalls++
	return r.claimSettling, nil
}

func (r *imageStudioWorkerRepoStub) MarkSucceeded(_ context.Context, _ int64, _ time.Time, chargedAmountUSD float64, _, _, _ string, _ int64, _, _ int, _ *time.Time) error {
	r.markSucceededCalls++
	r.chargedAmountUSD = chargedAmountUSD
	return r.markSucceededErr
}

func (r *imageStudioWorkerRepoStub) MarkSettlementRetryable(_ context.Context, _ int64, _ time.Time, errorCode, _ string) error {
	r.markSettlementRetryableCalls++
	r.retryErrorCode = errorCode
	return nil
}

func (r *imageStudioWorkerRepoStub) MarkSettlementFailed(_ context.Context, _ int64, _ time.Time, errorCode, _ string) (bool, error) {
	r.markSettlementFailedCalls++
	r.failedErrorCode = errorCode
	return true, nil
}

type imageStudioAccountRepoStub struct {
	AccountRepository
	account *Account
	err     error
}

func (r *imageStudioAccountRepoStub) GetByID(context.Context, int64) (*Account, error) {
	return r.account, r.err
}

type imageStudioAPIKeyRepoStub struct {
	APIKeyRepository
	apiKey *APIKey
	err    error
	calls  int
}

func (r *imageStudioAPIKeyRepoStub) GetByID(context.Context, int64) (*APIKey, error) {
	r.calls++
	return r.apiKey, r.err
}

type imageStudioSubscriptionResolverStub struct {
	subscription *UserSubscription
	err          error
	calls        int
	getByIDCalls int
	userID       int64
	groupID      int64
}

func (r *imageStudioSubscriptionResolverStub) GetByID(context.Context, int64) (*UserSubscription, error) {
	r.getByIDCalls++
	return r.subscription, r.err
}

func (r *imageStudioSubscriptionResolverStub) GetActiveSubscription(_ context.Context, userID, groupID int64) (*UserSubscription, error) {
	r.calls++
	r.userID = userID
	r.groupID = groupID
	return r.subscription, r.err
}

func TestImageStudioSettlementPayloadRoundTripPreservesBillingMetadata(t *testing.T) {
	serviceTier := "priority"
	reasoningEffort := "high"
	firstTokenMs := 125
	result := &OpenAIForwardResult{
		RequestID:       "upstream-request-id",
		ResponseID:      "response-id",
		Usage:           OpenAIUsage{InputTokens: 13, OutputTokens: 8, ImageOutputTokens: 21},
		Model:           "gpt-image-1",
		BillingModel:    "gpt-image-1",
		UpstreamModel:   "gpt-image-1-2025-01-01",
		ServiceTier:     &serviceTier,
		ReasoningEffort: &reasoningEffort,
		ResponseHeaders: http.Header{"X-Ignored": []string{"secret"}},
		Duration:        2 * time.Second,
		FirstTokenMs:    &firstTokenMs,
		ImageCount:      1,
		ImageSize:       "1024x1024",
		ImageOutputSize: "1024x1024",
		ImageSizeSource: "response",
		ImageSizeBreakdown: map[string]int{
			"1024x1024": 1,
		},
	}
	fields := ChannelUsageFields{
		ChannelID:          19,
		OriginalModel:      "image-alias",
		ChannelMappedModel: "gpt-image-1",
		BillingModelSource: BillingModelSourceChannelMapped,
		ModelMappingChain:  "image-alias->gpt-image-1->gpt-image-1-2025-01-01",
	}

	raw, err := marshalImageStudioSettlementPayload(77, result, fields, "/v1/images/generations", "/v1/images/generations")
	require.NoError(t, err)
	require.NotContains(t, string(raw), "X-Ignored")

	payload, restored, err := unmarshalImageStudioSettlementPayload(raw)
	require.NoError(t, err)
	require.Equal(t, int64(77), payload.AccountID)
	require.Equal(t, fields, payload.ChannelUsageFields)
	require.Equal(t, "/v1/images/generations", payload.InboundEndpoint)
	require.Equal(t, "/v1/images/generations", payload.UpstreamEndpoint)
	require.Equal(t, result.Usage, restored.Usage)
	require.Equal(t, result.Model, restored.Model)
	require.Equal(t, result.BillingModel, restored.BillingModel)
	require.Equal(t, result.UpstreamModel, restored.UpstreamModel)
	require.Equal(t, result.ServiceTier, restored.ServiceTier)
	require.Equal(t, result.ReasoningEffort, restored.ReasoningEffort)
	require.Equal(t, result.Duration, restored.Duration)
	require.Equal(t, result.FirstTokenMs, restored.FirstTokenMs)
	require.Equal(t, result.ImageSizeBreakdown, restored.ImageSizeBreakdown)
	require.Nil(t, restored.ResponseHeaders)
}

func TestImageStudioSettlementPayloadPreservesSubscriptionID(t *testing.T) {
	subscription := &UserSubscription{ID: 91, UserID: 42, GroupID: 12}
	raw, err := marshalImageStudioSettlementPayloadWithSubscription(
		77,
		&OpenAIForwardResult{Model: "gpt-image-1", ImageCount: 1, ImageSize: "1K"},
		ChannelUsageFields{},
		"/v1/images/generations",
		"/v1/images/generations",
		subscription,
	)
	require.NoError(t, err)

	payload, _, err := unmarshalImageStudioSettlementPayload(raw)
	require.NoError(t, err)
	require.NotNil(t, payload.SubscriptionID)
	require.Equal(t, subscription.ID, *payload.SubscriptionID)
}

func TestIsImageStudioRetryableError(t *testing.T) {
	t.Run("retryable upstream failover status", func(t *testing.T) {
		err := &UpstreamFailoverError{StatusCode: 429}
		require.True(t, isImageStudioRetryableError(err))
	})

	t.Run("retryable timeout", func(t *testing.T) {
		require.True(t, isImageStudioRetryableError(context.DeadlineExceeded))
	})

	t.Run("retryable temporary network error", func(t *testing.T) {
		require.True(t, isImageStudioRetryableError(imageStudioTemporaryNetError{}))
	})

	t.Run("non-retryable validation", func(t *testing.T) {
		require.False(t, isImageStudioRetryableError(errors.New("model is required")))
	})
}

type imageStudioTemporaryNetError struct{}

func (imageStudioTemporaryNetError) Error() string   { return "temporary network failure" }
func (imageStudioTemporaryNetError) Timeout() bool   { return false }
func (imageStudioTemporaryNetError) Temporary() bool { return true }

func TestImageStudioRetryDelay(t *testing.T) {
	require.Equal(t, 10*time.Second, imageStudioRetryDelay(1))
	require.Equal(t, 30*time.Second, imageStudioRetryDelay(2))
	require.Equal(t, 90*time.Second, imageStudioRetryDelay(3))
}
