package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBuildOpenAIStoredEditUploadsPreservesOrderAndMask(t *testing.T) {
	inputs := &OpenedEditInputs{
		Images: []OpenedEditInput{
			openOAuthEditTestInput(t, "image-01.png", "image/png", []byte("first")),
			openOAuthEditTestInput(t, "image-02.webp", "image/webp", []byte("second")),
			openOAuthEditTestInput(t, "image-03.jpg", "image/jpeg", []byte("third")),
			openOAuthEditTestInput(t, "image-04.png", "image/png", []byte("fourth")),
		},
	}
	mask := openOAuthEditTestInput(t, "mask.webp", "image/webp", []byte("mask"))
	inputs.Mask = &mask
	t.Cleanup(func() { _ = inputs.Close() })

	uploads, maskUpload, err := buildOpenAIStoredEditUploads(context.Background(), inputs, openAIImagesStoredEditMaxBytes)

	require.NoError(t, err)
	require.Len(t, uploads, 4)
	require.Equal(t, []byte("first"), uploads[0].Data)
	require.Equal(t, []byte("second"), uploads[1].Data)
	require.Equal(t, []byte("third"), uploads[2].Data)
	require.Equal(t, []byte("fourth"), uploads[3].Data)
	require.Equal(t, []string{"image/png", "image/webp", "image/jpeg", "image/png"}, []string{
		uploads[0].ContentType, uploads[1].ContentType, uploads[2].ContentType, uploads[3].ContentType,
	})
	require.NotNil(t, maskUpload)
	require.Equal(t, "mask", maskUpload.FieldName)
	require.Equal(t, "image/webp", maskUpload.ContentType)
	require.Equal(t, []byte("mask"), maskUpload.Data)
}

func TestBuildOpenAIStoredEditUploadsAcceptsSingleImage(t *testing.T) {
	input := openOAuthEditTestInput(t, "image-01.png", "image/png", []byte("single"))
	t.Cleanup(func() { _ = input.File.Close() })

	uploads, mask, err := buildOpenAIStoredEditUploads(
		context.Background(), &OpenedEditInputs{Images: []OpenedEditInput{input}}, openAIImagesStoredEditMaxBytes,
	)

	require.NoError(t, err)
	require.Len(t, uploads, 1)
	require.Equal(t, []byte("single"), uploads[0].Data)
	require.Nil(t, mask)
}

func TestBuildOpenAIStoredEditUploadsEnforcesTotalLimit(t *testing.T) {
	inputs := &OpenedEditInputs{Images: []OpenedEditInput{
		openOAuthEditTestInput(t, "image-01.png", "image/png", []byte("1234")),
		openOAuthEditTestInput(t, "image-02.png", "image/png", []byte("5678")),
	}}
	t.Cleanup(func() { _ = inputs.Close() })

	_, _, err := buildOpenAIStoredEditUploads(context.Background(), inputs, 7)

	require.ErrorIs(t, err, ErrImageStudioInputTooLarge)
	var inputErr *ImageStudioInputError
	require.ErrorAs(t, err, &inputErr)
	require.Equal(t, ImageStudioInputCodeInvalid, inputErr.Code)
}

func TestBuildOpenAIStoredEditUploadsRejectsOversizedFileBeforeRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.png")
	file, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, file.Truncate(openAIImageMaxUploadPartSize+1))
	require.NoError(t, file.Close())
	file, err = os.Open(path)
	require.NoError(t, err)
	inputs := &OpenedEditInputs{Images: []OpenedEditInput{{File: file, Path: "inputs/upload-test/image-01.png", ContentType: "image/png"}}}
	t.Cleanup(func() { _ = inputs.Close() })

	_, _, err = buildOpenAIStoredEditUploads(context.Background(), inputs, openAIImagesStoredEditMaxBytes)

	require.ErrorIs(t, err, ErrImageStudioInputTooLarge)
}

func TestBuildOpenAIStoredEditUploadsClassifiesSeekAndCancellation(t *testing.T) {
	t.Run("seek", func(t *testing.T) {
		input := openOAuthEditTestInput(t, "image-01.png", "image/png", []byte("image"))
		require.NoError(t, input.File.Close())
		_, _, err := buildOpenAIStoredEditUploads(context.Background(), &OpenedEditInputs{Images: []OpenedEditInput{input}}, openAIImagesStoredEditMaxBytes)
		require.Error(t, err)
		var inputErr *ImageStudioInputError
		require.ErrorAs(t, err, &inputErr)
		require.Equal(t, ImageStudioInputCodeStorageUnavailable, inputErr.Code)
	})

	t.Run("read", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "image-01.png")
		require.NoError(t, os.WriteFile(path, []byte("image"), 0o600))
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		require.NoError(t, err)
		t.Cleanup(func() { _ = file.Close() })
		input := OpenedEditInput{File: file, Path: "inputs/upload-test/image-01.png", ContentType: "image/png"}

		_, _, err = buildOpenAIStoredEditUploads(context.Background(), &OpenedEditInputs{Images: []OpenedEditInput{input}}, openAIImagesStoredEditMaxBytes)

		require.Error(t, err)
		var inputErr *ImageStudioInputError
		require.ErrorAs(t, err, &inputErr)
		require.Equal(t, ImageStudioInputCodeStorageUnavailable, inputErr.Code)
	})

	t.Run("canceled", func(t *testing.T) {
		input := openOAuthEditTestInput(t, "image-01.png", "image/png", []byte("image"))
		t.Cleanup(func() { _ = input.File.Close() })
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, err := buildOpenAIStoredEditUploads(ctx, &OpenedEditInputs{Images: []OpenedEditInput{input}}, openAIImagesStoredEditMaxBytes)
		require.ErrorIs(t, err, context.Canceled)
		var inputErr *ImageStudioInputError
		require.ErrorAs(t, err, &inputErr)
		require.Equal(t, ImageStudioInputCodeStorageUnavailable, inputErr.Code)
	})
}

func TestOpenAIGatewayServiceForwardImagesOAuthEditLocalReadFailureDoesNotCallUpstream(t *testing.T) {
	input := openOAuthEditTestInput(t, "image-01.png", "image/png", []byte("image"))
	require.NoError(t, input.File.Close())
	upstream := &httpUpstreamRecorder{}
	gateway := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{
		ID: 3, Name: "openai-oauth", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "token-123"},
	}
	parsed := &OpenAIImagesRequest{Endpoint: openAIImagesEditsEndpoint, Model: "gpt-image-2", Prompt: "edit", N: 1}
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, openAIImagesEditsEndpoint, nil)

	_, err := gateway.ForwardImagesOAuthEdit(
		context.Background(), ginCtx, account, parsed, &OpenedEditInputs{Images: []OpenedEditInput{input}}, "",
	)

	require.Error(t, err)
	var inputErr *ImageStudioInputError
	require.ErrorAs(t, err, &inputErr)
	require.Equal(t, ImageStudioInputCodeStorageUnavailable, inputErr.Code)
	require.Nil(t, upstream.lastReq)
}

func TestImageStudioJobServiceExecutionInputFailureIsTerminal(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Hour)
	store, staged := stageImageStudioWorkerInputs(t, [][]byte{imageStudioTestPNG(t, 2, 2, false)}, nil, 1<<20)
	repo := &imageStudioWorkerRepoStub{}
	apiKeys := &imageStudioAPIKeyRepoStub{apiKey: validImageStudioWorkerAPIKey()}
	executor := &recordingImageStudioJobExecutor{err: inputStorageError(io.ErrUnexpectedEOF)}
	svc := newImageStudioWorkerTestService(repo, store, apiKeys, executor, now)

	svc.processJob(context.Background(), storedImageStudioEditJob(staged, &expiresAt))

	require.Equal(t, 1, executor.calls)
	require.Equal(t, 1, repo.markFailedCalls)
	require.Equal(t, ImageStudioInputCodeStorageUnavailable, repo.failedErrorCode)
	require.Zero(t, repo.markRetryableCalls)
}

func TestOpenAIGatewayServiceForwardImagesOAuthStoredEditUsesResponsesRepresentation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	first := imageStudioTestPNG(t, 2, 2, false)
	second := imageStudioTestJPEG(t, 2, 2)
	third := imageStudioTestPNG(t, 2, 2, true)
	fourth := imageStudioTestJPEG(t, 3, 3)
	maskBytes := imageStudioTestPNG(t, 2, 2, true)
	inputs := &OpenedEditInputs{Images: []OpenedEditInput{
		openOAuthEditTestInput(t, "image-01.png", "image/png", first),
		openOAuthEditTestInput(t, "image-02.jpg", "image/jpeg", second),
		openOAuthEditTestInput(t, "image-03.png", "image/png", third),
		openOAuthEditTestInput(t, "image-04.jpg", "image/jpeg", fourth),
	}}
	mask := openOAuthEditTestInput(t, "mask.png", "image/png", maskBytes)
	inputs.Mask = &mask
	t.Cleanup(func() { _ = inputs.Close() })

	output := imageStudioTestPNG(t, 2, 2, false)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000002,\"usage\":{\"input_tokens\":13,\"output_tokens\":21,\"output_tokens_details\":{\"image_tokens\":8}},\"tool_usage\":{\"image_gen\":{\"images\":1}},\"output\":[{\"type\":\"image_generation_call\",\"result\":\"" + base64.StdEncoding.EncodeToString(output) + "\",\"output_format\":\"png\"}]}}\n\n" +
				"data: [DONE]\n\n",
		)),
	}}
	gateway := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{
		ID: 3, Name: "openai-oauth", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "token-123"},
	}
	parsed := &OpenAIImagesRequest{
		Endpoint: openAIImagesEditsEndpoint, Model: "gpt-image-2", Prompt: "edit", N: 1,
		OutputFormat: "png", ResponseFormat: "b64_json", Multipart: true, HasMask: true,
	}
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, openAIImagesEditsEndpoint, bytes.NewReader([]byte(`{"prompt":"edit"}`)))
	ginCtx.Set("api_key", &APIKey{ID: 41})

	result, err := gateway.ForwardImagesOAuthEdit(context.Background(), ginCtx, account, parsed, inputs, "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, 13, result.Usage.InputTokens)
	require.Equal(t, 21, result.Usage.OutputTokens)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "edit", gjson.GetBytes(upstream.lastBody, "tools.0.action").String())
	require.Equal(t, openAIImagesResponsesMainModel, gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(first), gjson.GetBytes(upstream.lastBody, "input.0.content.1.image_url").String())
	require.Equal(t, "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(second), gjson.GetBytes(upstream.lastBody, "input.0.content.2.image_url").String())
	require.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(third), gjson.GetBytes(upstream.lastBody, "input.0.content.3.image_url").String())
	require.Equal(t, "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(fourth), gjson.GetBytes(upstream.lastBody, "input.0.content.4.image_url").String())
	require.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(maskBytes), gjson.GetBytes(upstream.lastBody, "tools.0.input_image_mask.image_url").String())
	require.Empty(t, parsed.Uploads, "stored bytes must remain request-local")
	require.Nil(t, parsed.MaskUpload, "stored mask bytes must remain request-local")
	require.NotContains(t, string([]byte(`{"model":"gpt-image-2","prompt":"edit"}`)), "data:")
}

func TestImageStudioOAuthSettlementPayloadContainsNoInputBytes(t *testing.T) {
	inputBase64 := base64.StdEncoding.EncodeToString([]byte("private input image"))
	jobPayload := []byte(`{"model":"gpt-image-2","prompt":"edit","size":"1024x1024"}`)
	result := &OpenAIForwardResult{
		RequestID:     "req-oauth-edit",
		ResponseID:    "resp-oauth-edit",
		Usage:         OpenAIUsage{InputTokens: 13, OutputTokens: 21, ImageOutputTokens: 8},
		Model:         "gpt-image-2",
		BillingModel:  "gpt-image-2",
		UpstreamModel: "gpt-image-2",
		ImageCount:    1,
		ImageSize:     "1024x1024",
	}

	raw, err := marshalImageStudioSettlementPayload(
		3, result, ChannelUsageFields{OriginalModel: "image-alias", ChannelMappedModel: "gpt-image-2"},
		openAIImagesEditsEndpoint, openAIResponsesEndpoint,
	)

	require.NoError(t, err)
	require.NotContains(t, string(raw), "data:")
	require.NotContains(t, string(raw), inputBase64)
	payload, restored, err := unmarshalImageStudioSettlementPayload(raw)
	require.NoError(t, err)
	require.Equal(t, openAIImagesEditsEndpoint, payload.InboundEndpoint)
	require.Equal(t, openAIResponsesEndpoint, payload.UpstreamEndpoint)
	require.Equal(t, result.Usage, restored.Usage)
	require.Equal(t, HashUsageRequestPayload(jobPayload), HashUsageRequestPayload(append([]byte(nil), jobPayload...)))
}

func openOAuthEditTestInput(t *testing.T, name, contentType string, data []byte) OpenedEditInput {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	file, err := os.Open(path)
	require.NoError(t, err)
	return OpenedEditInput{
		File:        file,
		Path:        filepath.ToSlash(filepath.Join("inputs", "upload-test", name)),
		ContentType: contentType,
	}
}
