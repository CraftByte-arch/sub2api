package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/jpeg"
	_ "image/png"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	imageStudioThumbnailMaxEdge    = 320
	imageStudioThumbnailQuality    = 82
	imageStudioMaxAttempts         = 3
	imageStudioHeartbeatInterval   = time.Minute
	imageStudioHeartbeatStaleAfter = 5 * time.Minute
)

const imageStudioSettlementPayloadVersion = 1

var (
	errImageStudioSettlementPayloadInvalid    = errors.New("image studio settlement payload is invalid")
	errImageStudioSettlementDependencyInvalid = errors.New("image studio settlement dependency is invalid")
)

type imageStudioSettlementPayload struct {
	Version            int                         `json:"version"`
	AccountID          int64                       `json:"account_id"`
	SubscriptionID     *int64                      `json:"subscription_id,omitempty"`
	Result             imageStudioSettlementResult `json:"result"`
	ChannelUsageFields ChannelUsageFields          `json:"channel_usage_fields"`
	InboundEndpoint    string                      `json:"inbound_endpoint"`
	UpstreamEndpoint   string                      `json:"upstream_endpoint"`
}

type imageStudioSettlementResult struct {
	RequestID            string         `json:"request_id"`
	ResponseID           string         `json:"response_id"`
	Usage                OpenAIUsage    `json:"usage"`
	Model                string         `json:"model"`
	BillingModel         string         `json:"billing_model"`
	UpstreamModel        string         `json:"upstream_model"`
	ServiceTier          *string        `json:"service_tier,omitempty"`
	ReasoningEffort      *string        `json:"reasoning_effort,omitempty"`
	Stream               bool           `json:"stream"`
	OpenAIWSMode         bool           `json:"openai_ws_mode"`
	DurationNanoseconds  int64          `json:"duration_nanoseconds"`
	FirstTokenMs         *int           `json:"first_token_ms,omitempty"`
	ClientDisconnect     bool           `json:"client_disconnect"`
	ImageCount           int            `json:"image_count"`
	ImageSize            string         `json:"image_size"`
	ImageInputSize       string         `json:"image_input_size"`
	ImageOutputSize      string         `json:"image_output_size"`
	ImageOutputSizes     []string       `json:"image_output_sizes,omitempty"`
	ImageSizeSource      string         `json:"image_size_source"`
	ImageSizeBreakdown   map[string]int `json:"image_size_breakdown,omitempty"`
	VideoCount           int            `json:"video_count"`
	VideoResolution      string         `json:"video_resolution"`
	VideoDurationSeconds int            `json:"video_duration_seconds"`
}

// imageStudioExecutionInput carries short-lived opened edit files to the
// protocol-specific executor without copying image bytes into persisted data.
type imageStudioExecutionInput struct {
	Payload    []byte
	EditInputs *OpenedEditInputs
}

func (i *imageStudioExecutionInput) Close() error {
	if i == nil || i.EditInputs == nil {
		return nil
	}
	return i.EditInputs.Close()
}

type imageStudioJobExecutor interface {
	Execute(ctx context.Context, job ImageStudioJob, apiKey *APIKey, input *imageStudioExecutionInput) (*imageStudioForwardOutcome, error)
}

type imageStudioGatewayJobExecutor struct {
	service *ImageStudioJobService
}

func (e *imageStudioGatewayJobExecutor) Execute(ctx context.Context, job ImageStudioJob, apiKey *APIKey, input *imageStudioExecutionInput) (*imageStudioForwardOutcome, error) {
	if e == nil || e.service == nil || input == nil {
		return nil, fmt.Errorf("image studio executor is not configured")
	}
	return e.service.forwardExecutionJob(ctx, job, apiKey, input)
}

func marshalImageStudioSettlementPayload(accountID int64, result *OpenAIForwardResult, fields ChannelUsageFields, inboundEndpoint, upstreamEndpoint string) (json.RawMessage, error) {
	return marshalImageStudioSettlementPayloadWithSubscription(accountID, result, fields, inboundEndpoint, upstreamEndpoint, nil)
}

func marshalImageStudioSettlementPayloadWithSubscription(accountID int64, result *OpenAIForwardResult, fields ChannelUsageFields, inboundEndpoint, upstreamEndpoint string, subscription *UserSubscription) (json.RawMessage, error) {
	if result == nil {
		return nil, fmt.Errorf("image studio settlement result is nil")
	}
	var subscriptionID *int64
	if subscription != nil && subscription.ID > 0 {
		value := subscription.ID
		subscriptionID = &value
	}
	payload := imageStudioSettlementPayload{
		Version:            imageStudioSettlementPayloadVersion,
		AccountID:          accountID,
		SubscriptionID:     subscriptionID,
		ChannelUsageFields: fields,
		InboundEndpoint:    inboundEndpoint,
		UpstreamEndpoint:   upstreamEndpoint,
		Result: imageStudioSettlementResult{
			RequestID:            result.RequestID,
			ResponseID:           result.ResponseID,
			Usage:                result.Usage,
			Model:                result.Model,
			BillingModel:         result.BillingModel,
			UpstreamModel:        result.UpstreamModel,
			ServiceTier:          result.ServiceTier,
			ReasoningEffort:      result.ReasoningEffort,
			Stream:               result.Stream,
			OpenAIWSMode:         result.OpenAIWSMode,
			DurationNanoseconds:  int64(result.Duration),
			FirstTokenMs:         result.FirstTokenMs,
			ClientDisconnect:     result.ClientDisconnect,
			ImageCount:           result.ImageCount,
			ImageSize:            result.ImageSize,
			ImageInputSize:       result.ImageInputSize,
			ImageOutputSize:      result.ImageOutputSize,
			ImageOutputSizes:     result.ImageOutputSizes,
			ImageSizeSource:      result.ImageSizeSource,
			ImageSizeBreakdown:   result.ImageSizeBreakdown,
			VideoCount:           result.VideoCount,
			VideoResolution:      result.VideoResolution,
			VideoDurationSeconds: result.VideoDurationSeconds,
		},
	}
	raw, err := json.Marshal(payload)
	return json.RawMessage(raw), err
}

func unmarshalImageStudioSettlementPayload(raw json.RawMessage) (*imageStudioSettlementPayload, *OpenAIForwardResult, error) {
	var payload imageStudioSettlementPayload
	if len(raw) == 0 {
		return nil, nil, fmt.Errorf("%w: payload is empty", errImageStudioSettlementPayloadInvalid)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil, fmt.Errorf("%w: decode payload: %v", errImageStudioSettlementPayloadInvalid, err)
	}
	if payload.Version != imageStudioSettlementPayloadVersion || payload.AccountID <= 0 {
		return nil, nil, errImageStudioSettlementPayloadInvalid
	}
	result := &OpenAIForwardResult{
		RequestID:            payload.Result.RequestID,
		ResponseID:           payload.Result.ResponseID,
		Usage:                payload.Result.Usage,
		Model:                payload.Result.Model,
		BillingModel:         payload.Result.BillingModel,
		UpstreamModel:        payload.Result.UpstreamModel,
		ServiceTier:          payload.Result.ServiceTier,
		ReasoningEffort:      payload.Result.ReasoningEffort,
		Stream:               payload.Result.Stream,
		OpenAIWSMode:         payload.Result.OpenAIWSMode,
		Duration:             time.Duration(payload.Result.DurationNanoseconds),
		FirstTokenMs:         payload.Result.FirstTokenMs,
		ClientDisconnect:     payload.Result.ClientDisconnect,
		ImageCount:           payload.Result.ImageCount,
		ImageSize:            payload.Result.ImageSize,
		ImageInputSize:       payload.Result.ImageInputSize,
		ImageOutputSize:      payload.Result.ImageOutputSize,
		ImageOutputSizes:     payload.Result.ImageOutputSizes,
		ImageSizeSource:      payload.Result.ImageSizeSource,
		ImageSizeBreakdown:   payload.Result.ImageSizeBreakdown,
		VideoCount:           payload.Result.VideoCount,
		VideoResolution:      payload.Result.VideoResolution,
		VideoDurationSeconds: payload.Result.VideoDurationSeconds,
	}
	return &payload, result, nil
}

func (s *ImageStudioJobService) SetRuntimeDependencies(
	openAIGateway *OpenAIGatewayService,
	apiKeyService *APIKeyService,
	billingCacheService *BillingCacheService,
	subscriptionResolver *SubscriptionService,
) {
	if s == nil {
		return
	}
	s.openAIGateway = openAIGateway
	s.apiKeyService = apiKeyService
	s.billingCacheService = billingCacheService
	s.subscriptionResolver = subscriptionResolver
}

func (s *ImageStudioJobService) drainQueueOnce(ctx context.Context) {
	if s == nil || s.repo == nil {
		return
	}
	if !s.InputStorageAvailable() {
		return
	}
	maxConcurrency := s.AsyncConcurrency(ctx)
	running := int(atomic.LoadInt32(&s.running))
	available := maxConcurrency - running
	if available <= 0 {
		return
	}
	jobs, err := s.repo.ListRunnableJobs(ctx, available)
	if err != nil {
		return
	}
	for _, job := range jobs {
		if int(atomic.LoadInt32(&s.running)) >= maxConcurrency {
			return
		}
		atomic.AddInt32(&s.running, 1)
		s.wg.Add(1)
		go func(job ImageStudioJob) {
			defer s.wg.Done()
			defer atomic.AddInt32(&s.running, -1)
			s.processJob(context.Background(), job)
		}(job)
	}
}

func (s *ImageStudioJobService) processJob(ctx context.Context, job ImageStudioJob) {
	if job.Status == ImageStudioJobStatusRunning {
		s.recoverStaleRunningJob(ctx, job)
		return
	}
	if job.Status == ImageStudioJobStatusSettling {
		s.processSettlingJob(ctx, job)
		return
	}
	if !s.InputStorageAvailable() {
		return
	}

	startedAt := time.Now()
	acquired, err := s.repo.MarkRunning(ctx, job.ID, startedAt)
	if err != nil {
		return
	}
	if !acquired {
		return
	}
	job.Status = ImageStudioJobStatusRunning
	if err := s.repo.UpdateHeartbeat(ctx, job.ID, startedAt); err != nil {
		return
	}
	stopHeartbeat := s.startImageStudioJobHeartbeat(ctx, job.ID, imageStudioHeartbeatInterval)
	defer stopHeartbeat()
	_, terminal, err := s.materializeLegacyJobInputs(ctx, &job)
	if terminal {
		return
	}
	if err != nil {
		if _, classified := classifyImageStudioInputFailure(err); classified {
			s.failImageStudioInput(ctx, job, err)
		}
		return
	}
	execution, err := s.prepareImageStudioExecution(job)
	if err != nil {
		s.failImageStudioInput(ctx, job, err)
		return
	}
	executionClosed := false
	defer func() {
		if !executionClosed {
			_ = execution.Close()
		}
	}()
	failBeforeExecution := func(code string, cause error) {
		closeErr := execution.Close()
		executionClosed = true
		if closeErr != nil {
			s.failImageStudioInput(ctx, job, inputStorageError(errors.Join(cause, closeErr)))
			return
		}
		s.failJob(ctx, job, code, cause)
	}

	apiKey, err := s.apiKeyService.GetByID(ctx, job.APIKeyID)
	if err != nil {
		failBeforeExecution("api_key_not_found", err)
		return
	}
	if apiKey.UserID != job.UserID {
		failBeforeExecution("api_key_owner_mismatch", ErrImageStudioJobInvalid)
		return
	}
	if !apiKey.IsActive() {
		failBeforeExecution("api_key_inactive", fmt.Errorf("api key is inactive"))
		return
	}
	if !GroupAllowsImageGeneration(apiKey.Group) {
		failBeforeExecution("image_generation_disabled", fmt.Errorf("%s", ImageGenerationPermissionMessage()))
		return
	}
	if err := s.ValidateAPIKeyAvailableForImageStudio(ctx, apiKey); err != nil {
		failBeforeExecution("image_studio_group_not_available", err)
		return
	}
	subscription, err := s.resolveImageStudioSubscription(ctx, apiKey)
	if err != nil {
		failBeforeExecution("subscription_unavailable", err)
		return
	}
	if s.billingCacheService != nil {
		if err := s.billingCacheService.CheckBillingEligibility(ctx, apiKey.User, apiKey, apiKey.Group, subscription, QuotaPlatform(ctx, apiKey)); err != nil {
			failBeforeExecution("billing_ineligible", err)
			return
		}
	}

	forwarded, err := s.executeImageStudioJob(ctx, job, apiKey, execution)
	closeErr := execution.Close()
	executionClosed = true
	if err != nil {
		executionErr := err
		if closeErr != nil {
			err = errors.Join(err, inputStorageError(closeErr))
		}
		if _, classified := classifyImageStudioInputFailure(executionErr); classified {
			s.failImageStudioInput(ctx, job, err)
			return
		}
		s.handleJobError(ctx, job, "upstream_failed", err)
		return
	}

	imageBytes, mimeType, err := decodeImageStudioResponseImage(forwarded.rawBody, job.OutputFormat)
	if err != nil {
		s.handleJobError(ctx, job, "invalid_image_response", err)
		return
	}

	originalPath, thumbnailPath, fileSizeBytes, width, height, err := s.persistAssets(job.ID, imageBytes, mimeType)
	if err != nil {
		s.handleJobError(ctx, job, "asset_persist_failed", err)
		return
	}

	settlementPayload, err := marshalImageStudioSettlementPayloadWithSubscription(
		forwarded.accountID,
		forwarded.result,
		forwarded.channelUsageFields,
		forwarded.inboundEndpoint,
		forwarded.upstreamEndpoint,
		subscription,
	)
	if err != nil {
		removeUncommittedImageStudioAssets(originalPath, thumbnailPath)
		s.handleJobError(ctx, job, "settlement_payload_failed", err)
		return
	}
	leaseAt := time.Now()
	if err := s.markImageStudioSettling(ctx, job.ID, settlementPayload, originalPath, thumbnailPath, mimeType, fileSizeBytes, width, height, leaseAt); err != nil {
		s.handleJobError(ctx, job, "settlement_persist_failed", err)
		return
	}
	s.cleanupDurableImageStudioInputs(ctx, job)

	job.Status = ImageStudioJobStatusSettling
	job.SettlementPayload = settlementPayload
	job.OriginalPath = originalPath
	job.ThumbnailPath = thumbnailPath
	job.MIMEType = mimeType
	job.FileSizeBytes = fileSizeBytes
	job.Width = width
	job.Height = height
	if err := s.settleJob(ctx, job, apiKey); err != nil {
		s.handleImageStudioSettlementError(ctx, job, err)
	}
}

func (s *ImageStudioJobService) prepareImageStudioExecution(job ImageStudioJob) (*imageStudioExecutionInput, error) {
	execution := &imageStudioExecutionInput{Payload: append([]byte(nil), job.RequestPayload...)}
	if job.Mode != ImageStudioJobModeEdit {
		return execution, nil
	}
	if job.InputDeletedAt != nil {
		return nil, inputMissingError(ErrImageStudioInputMissing)
	}
	if job.InputExpiresAt == nil {
		return nil, inputInvalidError(ErrImageStudioInputInvalid)
	}
	if !job.InputExpiresAt.After(s.now()) {
		return nil, inputExpiredError()
	}
	if s.inputStore == nil {
		return nil, inputStorageError(errors.New("image studio input store is not configured"))
	}
	opened, err := s.inputStore.OpenInputs(job.InputImagePaths, job.InputMaskPath)
	if err != nil {
		if _, classified := classifyImageStudioInputFailure(err); !classified {
			err = inputStorageError(err)
		}
		return nil, err
	}
	if opened == nil {
		return nil, inputStorageError(errors.New("image studio input store returned no inputs"))
	}
	execution.EditInputs = opened
	return execution, nil
}

func (s *ImageStudioJobService) executeImageStudioJob(ctx context.Context, job ImageStudioJob, apiKey *APIKey, input *imageStudioExecutionInput) (*imageStudioForwardOutcome, error) {
	if s.executor != nil {
		return s.executor.Execute(ctx, job, apiKey, input)
	}
	return (&imageStudioGatewayJobExecutor{service: s}).Execute(ctx, job, apiKey, input)
}

func (s *ImageStudioJobService) failImageStudioInput(ctx context.Context, job ImageStudioJob, err error) {
	code, ok := classifyImageStudioInputFailure(err)
	if !ok {
		code = ImageStudioInputCodeStorageUnavailable
		err = inputStorageError(err)
	}
	s.failJob(ctx, job, code, err)
}

func classifyImageStudioInputFailure(err error) (string, bool) {
	var inputErr *ImageStudioInputError
	if errors.As(err, &inputErr) && inputErr != nil {
		switch inputErr.Code {
		case ImageStudioInputCodeLegacyInvalid:
			return "", false
		case ImageStudioInputCodeExpired, ImageStudioInputCodeMissing, ImageStudioInputCodeInvalid,
			ImageStudioInputCodePathInvalid, ImageStudioInputCodeStorageUnavailable:
			return inputErr.Code, true
		}
	}
	switch {
	case errors.Is(err, ErrImageStudioInputExpired):
		return ImageStudioInputCodeExpired, true
	case errors.Is(err, ErrImageStudioInputMissing):
		return ImageStudioInputCodeMissing, true
	case errors.Is(err, ErrImageStudioInputPathInvalid):
		return ImageStudioInputCodePathInvalid, true
	case errors.Is(err, ErrImageStudioInputInvalid), errors.Is(err, ErrImageStudioInputTooLarge), errors.Is(err, ErrImageStudioInputDimensionsTooLarge):
		return ImageStudioInputCodeInvalid, true
	case errors.Is(err, ErrImageStudioInputStorageUnavailable):
		return ImageStudioInputCodeStorageUnavailable, true
	default:
		return "", false
	}
}

func (s *ImageStudioJobService) materializeLegacyJobInputs(ctx context.Context, job *ImageStudioJob) (materialized, terminal bool, retErr error) {
	if s == nil || job == nil || job.Mode != ImageStudioJobModeEdit || len(job.InputImagePaths) != 0 || job.InputMaskPath != nil {
		return false, false, nil
	}
	images, mask, redacted, present, err := parseImageStudioLegacyPayload(job.RequestPayload)
	if !present {
		return false, false, err
	}
	failInvalid := func(invalidErr error) (bool, bool, error) {
		if failErr := s.repo.FailLegacyInputs(ctx, job.ID, redacted, s.now()); failErr != nil {
			return false, true, errors.Join(invalidErr, failErr)
		}
		return false, true, nil
	}
	if err != nil {
		return failInvalid(err)
	}
	if s.inputStore == nil {
		return false, false, inputStorageError(errors.New("image studio input store is not configured"))
	}
	staged, err := s.inputStore.MaterializeLegacy(ctx, images, mask)
	if err != nil {
		if errors.Is(err, ErrImageStudioLegacyInputInvalid) {
			return failInvalid(err)
		}
		return false, false, err
	}
	if staged == nil || len(staged.ImagePaths) == 0 {
		return false, false, inputStorageError(errors.New("legacy image studio materialization returned no inputs"))
	}
	expiresAt := s.now().Add(time.Duration(s.InputRetentionHours(ctx)) * time.Hour)
	if err := s.repo.PersistLegacyInputs(ctx, job.ID, staged.ImagePaths, staged.MaskPath, redacted, expiresAt); err != nil {
		cleanupErr := s.inputStore.RemoveInputs(staged.ImagePaths, staged.MaskPath)
		return false, false, errors.Join(err, cleanupErr)
	}
	job.InputImagePaths = append([]string(nil), staged.ImagePaths...)
	job.InputMaskPath = cloneImageStudioString(staged.MaskPath)
	job.InputExpiresAt = &expiresAt
	job.InputDeletedAt = nil
	job.RequestPayload = append(json.RawMessage(nil), redacted...)
	return true, false, nil
}

func parseImageStudioLegacyPayload(raw json.RawMessage) (images []string, mask *string, redacted json.RawMessage, present bool, retErr error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil || payload == nil {
		return nil, nil, nil, false, legacyInputInvalidError(ErrImageStudioInputInvalid)
	}
	rawImages, hasImages := payload["images"]
	rawMask, hasMask := payload["mask"]
	present = hasImages || hasMask
	if !present {
		return nil, nil, nil, false, nil
	}
	delete(payload, "images")
	delete(payload, "mask")
	redactedBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, nil, true, legacyInputInvalidError(ErrImageStudioInputInvalid)
	}
	redacted = json.RawMessage(redactedBytes)
	if !hasImages {
		return nil, nil, redacted, true, legacyInputInvalidError(ErrImageStudioInputInvalid)
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(rawImages, &entries); err != nil {
		return nil, nil, redacted, true, legacyInputInvalidError(ErrImageStudioInputInvalid)
	}
	images = make([]string, len(entries))
	for i := range entries {
		value, err := imageStudioLegacyImageURL(entries[i])
		if err != nil {
			return nil, nil, redacted, true, err
		}
		images[i] = value
	}
	if hasMask {
		value, err := imageStudioLegacyImageURL(rawMask)
		if err != nil {
			return nil, nil, redacted, true, err
		}
		mask = &value
	}
	return images, mask, redacted, true, nil
}

func imageStudioLegacyImageURL(raw json.RawMessage) (string, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return "", legacyInputInvalidError(ErrImageStudioInputInvalid)
	}
	rawURL, ok := object["image_url"]
	if !ok {
		return "", legacyInputInvalidError(ErrImageStudioInputInvalid)
	}
	var value string
	if err := json.Unmarshal(rawURL, &value); err != nil {
		return "", legacyInputInvalidError(ErrImageStudioInputInvalid)
	}
	return value, nil
}

func (s *ImageStudioJobService) markImageStudioSettling(ctx context.Context, jobID int64, settlementPayload json.RawMessage, originalPath, thumbnailPath, mimeType string, fileSizeBytes int64, width, height int, leaseAt time.Time) error {
	if err := s.repo.MarkSettling(ctx, jobID, settlementPayload, originalPath, thumbnailPath, mimeType, fileSizeBytes, width, height, leaseAt); err != nil {
		removeUncommittedImageStudioAssets(originalPath, thumbnailPath)
		return err
	}
	return nil
}

func (s *ImageStudioJobService) recoverStaleRunningJob(ctx context.Context, job ImageStudioJob) {
	if s.expireRunningImageStudioInputs(ctx, job) {
		return
	}
	now := time.Now()
	_, _ = s.repo.MarkStaleRunningFailed(ctx, job.ID, now, now.Add(-imageStudioHeartbeatStaleAfter))
}

func (s *ImageStudioJobService) startImageStudioJobHeartbeat(ctx context.Context, jobID int64, interval time.Duration) func() {
	if interval <= 0 {
		interval = imageStudioHeartbeatInterval
	}
	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case heartbeatAt := <-ticker.C:
				_ = s.repo.UpdateHeartbeat(context.Background(), jobID, heartbeatAt)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

type imageStudioForwardOutcome struct {
	result             *OpenAIForwardResult
	rawBody            []byte
	accountID          int64
	channelUsageFields ChannelUsageFields
	inboundEndpoint    string
	upstreamEndpoint   string
}

func (s *ImageStudioJobService) forwardJob(ctx context.Context, job ImageStudioJob, apiKey *APIKey, body []byte) (*imageStudioForwardOutcome, error) {
	return s.forwardExecutionJob(ctx, job, apiKey, &imageStudioExecutionInput{Payload: body})
}

func (s *ImageStudioJobService) forwardExecutionJob(ctx context.Context, job ImageStudioJob, apiKey *APIKey, input *imageStudioExecutionInput) (*imageStudioForwardOutcome, error) {
	if s == nil || s.openAIGateway == nil {
		return nil, fmt.Errorf("openai gateway service is not configured")
	}
	if input == nil {
		return nil, fmt.Errorf("image studio execution input is required")
	}
	body := input.Payload

	endpoint := openAIImagesGenerationsEndpoint
	if job.Mode == ImageStudioJobModeEdit {
		endpoint = openAIImagesEditsEndpoint
	}
	parseEndpoint := endpoint
	storedEdit := job.Mode == ImageStudioJobModeEdit && input.EditInputs != nil
	if storedEdit {
		parseEndpoint = openAIImagesGenerationsEndpoint
	}
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest("POST", parseEndpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ginCtx.Request = req
	ginCtx.Set("api_key", apiKey)

	parsed, err := s.openAIGateway.ParseOpenAIImagesRequest(ginCtx, body)
	if err != nil {
		if imageStudioPayloadLooksLikeResponses(body) {
			return s.forwardResponsesJob(ctx, job, apiKey, body)
		}
		return nil, err
	}
	if storedEdit {
		parsed.Endpoint = openAIImagesEditsEndpoint
		parsed.Multipart = true
		parsed.HasMask = input.EditInputs.Mask != nil
		parsed.RequiredCapability = OpenAIImagesCapabilityNative
		ginCtx.Request.URL.Path = openAIImagesEditsEndpoint
	}
	requestModel := parsed.Model
	channelMapping, _ := s.openAIGateway.ResolveChannelMappingAndRestrict(ctx, apiKey.GroupID, requestModel)
	requestCtx := WithOpenAIImageGenerationIntent(ctx)
	selection, _, err := s.openAIGateway.SelectAccountWithSchedulerForImages(
		requestCtx,
		apiKey.GroupID,
		parsed.StickySessionSeed(),
		requestModel,
		nil,
		parsed.RequiredCapability,
	)
	if err != nil {
		return nil, err
	}
	if selection == nil || selection.Account == nil {
		return nil, fmt.Errorf("no available image account")
	}
	if selection.ReleaseFunc != nil {
		defer selection.ReleaseFunc()
	}

	var result *OpenAIForwardResult
	if storedEdit && selection.Account.Type == AccountTypeAPIKey {
		result, err = s.forwardSelectedAPIKeyEdit(requestCtx, ginCtx, selection.Account, input, parsed, channelMapping.MappedModel)
	} else if storedEdit && selection.Account.Type == AccountTypeOAuth {
		result, err = s.openAIGateway.ForwardImagesOAuthEdit(
			requestCtx, ginCtx, selection.Account, parsed, input.EditInputs, channelMapping.MappedModel,
		)
	} else {
		result, err = s.openAIGateway.ForwardImages(requestCtx, ginCtx, selection.Account, body, parsed, channelMapping.MappedModel)
	}
	if err != nil {
		return nil, err
	}
	if recorder.Code >= 400 {
		return nil, fmt.Errorf("upstream request failed with status %d", recorder.Code)
	}
	upstreamEndpoint := endpoint
	if storedEdit && selection.Account.Type == AccountTypeOAuth {
		upstreamEndpoint = openAIResponsesEndpoint
	}
	return &imageStudioForwardOutcome{
		result:             result,
		rawBody:            append([]byte(nil), recorder.Body.Bytes()...),
		accountID:          selection.Account.ID,
		channelUsageFields: channelMapping.ToUsageFields(requestModel, result.UpstreamModel),
		inboundEndpoint:    endpoint,
		upstreamEndpoint:   upstreamEndpoint,
	}, nil
}

func (s *ImageStudioJobService) forwardSelectedAPIKeyEdit(
	ctx context.Context,
	ginCtx *gin.Context,
	account *Account,
	input *imageStudioExecutionInput,
	parsed *OpenAIImagesRequest,
	channelMappedModel string,
) (*OpenAIForwardResult, error) {
	if s == nil || s.openAIGateway == nil || input == nil || input.EditInputs == nil {
		return nil, fmt.Errorf("image studio API Key edit executor is not configured")
	}
	builder, ok := s.inputStore.(imageStudioEditMultipartSpoolBuilder)
	if !ok || builder == nil {
		return nil, inputStorageError(errors.New("image studio multipart spool builder is not configured"))
	}
	_, upstreamModel, err := resolveOpenAIImagesAPIKeyModels(account, parsed, channelMappedModel)
	if err != nil {
		return nil, err
	}
	spool, err := builder.BuildEditMultipartSpool(input.EditInputs, input.Payload, upstreamModel)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cleanupErr := spool.Cleanup(); cleanupErr != nil {
			slog.Warn("image_studio_multipart_spool_cleanup_failed", "error_kind", imageStudioMultipartCleanupLogValue(cleanupErr))
		}
	}()
	return s.openAIGateway.ForwardImagesAPIKeyEdit(
		ctx, ginCtx, account, spool.Reader, spool.ContentType, spool.ContentLength, parsed, channelMappedModel,
	)
}

func imageStudioPayloadLooksLikeResponses(body []byte) bool {
	if !gjson.ValidBytes(body) {
		return false
	}
	if gjson.GetBytes(body, "input").Exists() || gjson.GetBytes(body, "tools").Exists() || gjson.GetBytes(body, "tool_choice").Exists() {
		return true
	}
	return false
}

func (s *ImageStudioJobService) forwardResponsesJob(ctx context.Context, job ImageStudioJob, apiKey *APIKey, body []byte) (*imageStudioForwardOutcome, error) {
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest("POST", openAIResponsesEndpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ginCtx.Request = req
	ginCtx.Set("api_key", apiKey)

	requestModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if requestModel == "" {
		return nil, fmt.Errorf("responses model is required")
	}
	originalModel := requestModel
	channelMapping, _ := s.openAIGateway.ResolveChannelMappingAndRestrict(ctx, apiKey.GroupID, requestModel)
	forwardBody := body
	if channelMapping.Mapped && strings.TrimSpace(channelMapping.MappedModel) != "" {
		forwardBody = s.openAIGateway.ReplaceModelInBody(body, channelMapping.MappedModel)
		requestModel = channelMapping.MappedModel
	}
	selection, _, err := s.openAIGateway.SelectAccountWithSchedulerForImageProtocol(
		ctx,
		apiKey.GroupID,
		"",
		"",
		requestModel,
		nil,
		OpenAIUpstreamTransportAny,
		OpenAIEndpointCapabilityChatCompletions,
		OpenAIImageProtocolPreferenceResponses,
		false,
	)
	if err != nil {
		return nil, err
	}
	if selection == nil || selection.Account == nil {
		return nil, fmt.Errorf("no available image account")
	}
	if selection.ReleaseFunc != nil {
		defer selection.ReleaseFunc()
	}

	result, err := s.openAIGateway.Forward(ctx, ginCtx, selection.Account, forwardBody)
	if err != nil {
		return nil, err
	}
	if recorder.Code >= 400 {
		return nil, fmt.Errorf("upstream request failed with status %d", recorder.Code)
	}
	return &imageStudioForwardOutcome{
		result:             result,
		rawBody:            append([]byte(nil), recorder.Body.Bytes()...),
		accountID:          selection.Account.ID,
		channelUsageFields: channelMapping.ToUsageFields(originalModel, result.UpstreamModel),
		inboundEndpoint:    openAIResponsesEndpoint,
		upstreamEndpoint:   openAIResponsesEndpoint,
	}, nil
}

func (s *ImageStudioJobService) processSettlingJob(ctx context.Context, job ImageStudioJob) {
	if !s.InputStorageAvailable() {
		return
	}
	leaseAt := time.Now()
	claimed, err := s.repo.ClaimSettling(ctx, job.ID, leaseAt, leaseAt.Add(-5*time.Minute))
	if err != nil || !claimed {
		return
	}
	s.cleanupDurableImageStudioInputs(ctx, job)
	if receipt, err := s.findImageStudioUsageReceipt(ctx, job); err == nil {
		if err := s.markImageStudioSucceeded(ctx, job, receipt.ActualCost); err != nil {
			s.handleImageStudioSettlementError(ctx, job, err)
		}
		return
	} else if !errors.Is(err, ErrUsageLogNotFound) {
		s.handleImageStudioSettlementError(ctx, job, fmt.Errorf("load image studio usage receipt: %w", err))
		return
	}
	apiKey, err := s.apiKeyService.GetByID(ctx, job.APIKeyID)
	if err != nil {
		s.handleImageStudioSettlementError(ctx, job, fmt.Errorf("load api key: %w", err))
		return
	}
	if apiKey == nil || apiKey.UserID != job.UserID || apiKey.User == nil || apiKey.Group == nil {
		s.handleImageStudioSettlementError(ctx, job, fmt.Errorf("%w: api key snapshot is incomplete", errImageStudioSettlementDependencyInvalid))
		return
	}
	if err := s.settleJob(ctx, job, apiKey); err != nil {
		s.handleImageStudioSettlementError(ctx, job, err)
	}
}

func (s *ImageStudioJobService) cleanupDurableImageStudioInputs(ctx context.Context, job ImageStudioJob) {
	if len(job.InputImagePaths) == 0 && job.InputMaskPath == nil {
		return
	}
	if job.InputDeletedAt != nil {
		return
	}
	if s == nil || s.inputStore == nil {
		slog.Warn("image_studio_input_cleanup_failed", "job_id", job.ID, "stage", "remove", "error_kind", "store_unavailable")
		return
	}
	if err := s.inputStore.RemoveInputs(job.InputImagePaths, job.InputMaskPath); err != nil {
		slog.Warn("image_studio_input_cleanup_failed", "job_id", job.ID, "stage", "remove", "error_kind", imageStudioInputCleanupLogValue(err))
		return
	}
	if err := s.repo.MarkInputsDeleted(ctx, job.ID, time.Now()); err != nil {
		slog.Warn("image_studio_input_cleanup_failed", "job_id", job.ID, "stage", "mark_deleted", "error_kind", imageStudioInputCleanupLogValue(err))
	}
}

func imageStudioInputCleanupLogValue(err error) string {
	var inputErr *ImageStudioInputError
	if errors.As(err, &inputErr) && inputErr != nil && strings.TrimSpace(inputErr.Code) != "" {
		return inputErr.Code
	}
	if errors.Is(err, context.Canceled) {
		return "context_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "context_deadline_exceeded"
	}
	return "operation_failed"
}

func (s *ImageStudioJobService) settleJob(ctx context.Context, job ImageStudioJob, apiKey *APIKey) error {
	if s == nil || s.openAIGateway == nil || s.openAIGateway.accountRepo == nil {
		return fmt.Errorf("openai gateway settlement dependencies are not configured")
	}
	payload, result, err := unmarshalImageStudioSettlementPayload(job.SettlementPayload)
	if err != nil {
		return err
	}
	account, err := s.openAIGateway.accountRepo.GetByID(ctx, payload.AccountID)
	if err != nil {
		return fmt.Errorf("load settlement account: %w", err)
	}
	if account == nil || account.ID != payload.AccountID {
		return fmt.Errorf("%w: settlement account %d not found", errImageStudioSettlementDependencyInvalid, payload.AccountID)
	}
	subscription, err := s.resolveImageStudioSettlementSubscription(ctx, apiKey, payload.SubscriptionID)
	if err != nil {
		return err
	}
	result.RequestID = fmt.Sprintf("image-studio-job:%d", job.ID)
	usageLog, err := s.openAIGateway.recordUsageDetailed(ctx, &OpenAIRecordUsageInput{
		Result:             result,
		APIKey:             apiKey,
		User:               apiKey.User,
		Account:            account,
		Subscription:       subscription,
		InboundEndpoint:    payload.InboundEndpoint,
		UpstreamEndpoint:   payload.UpstreamEndpoint,
		RequestPayloadHash: HashUsageRequestPayload(job.RequestPayload),
		APIKeyService:      s.apiKeyService,
		QuotaPlatform:      PlatformFromAPIKey(apiKey),
		ChannelUsageFields: payload.ChannelUsageFields,
	})
	if err != nil {
		return fmt.Errorf("record image studio usage: %w", err)
	}
	return s.markImageStudioSucceeded(ctx, job, usageLog.ActualCost)
}

func (s *ImageStudioJobService) findImageStudioUsageReceipt(ctx context.Context, job ImageStudioJob) (*UsageLog, error) {
	if s == nil || s.openAIGateway == nil || s.openAIGateway.usageLogRepo == nil {
		return nil, ErrUsageLogNotFound
	}
	return s.openAIGateway.usageLogRepo.GetByRequestIDAndAPIKey(
		ctx,
		fmt.Sprintf("image-studio-job:%d", job.ID),
		job.APIKeyID,
	)
}

func (s *ImageStudioJobService) markImageStudioSucceeded(ctx context.Context, job ImageStudioJob, chargedAmountUSD float64) error {
	completedAt := time.Now()
	if err := s.repo.MarkSucceeded(
		ctx,
		job.ID,
		completedAt,
		chargedAmountUSD,
		job.OriginalPath,
		job.ThumbnailPath,
		job.MIMEType,
		job.FileSizeBytes,
		job.Width,
		job.Height,
		s.ResolveRetention(completedAt),
	); err != nil {
		return fmt.Errorf("mark image studio job succeeded: %w", err)
	}
	return nil
}

func (s *ImageStudioJobService) resolveImageStudioSubscription(ctx context.Context, apiKey *APIKey) (*UserSubscription, error) {
	if apiKey == nil || apiKey.Group == nil || !apiKey.Group.IsSubscriptionType() {
		return nil, nil
	}
	if apiKey.GroupID == nil || s.subscriptionResolver == nil {
		return nil, fmt.Errorf("subscription service is not configured")
	}
	subscription, err := s.subscriptionResolver.GetActiveSubscription(ctx, apiKey.UserID, *apiKey.GroupID)
	if err != nil {
		return nil, fmt.Errorf("get active subscription: %w", err)
	}
	if subscription == nil {
		return nil, ErrSubscriptionNotFound
	}
	return subscription, nil
}

func (s *ImageStudioJobService) resolveImageStudioSettlementSubscription(ctx context.Context, apiKey *APIKey, subscriptionID *int64) (*UserSubscription, error) {
	if subscriptionID == nil {
		return s.resolveImageStudioSubscription(ctx, apiKey)
	}
	if apiKey == nil || apiKey.GroupID == nil || s.subscriptionResolver == nil {
		return nil, fmt.Errorf("subscription service is not configured")
	}
	subscription, err := s.subscriptionResolver.GetByID(ctx, *subscriptionID)
	if err != nil {
		return nil, fmt.Errorf("get settlement subscription: %w", err)
	}
	if subscription == nil || subscription.ID != *subscriptionID {
		return nil, ErrSubscriptionNotFound
	}
	if subscription.UserID != apiKey.UserID || subscription.GroupID != *apiKey.GroupID {
		return nil, fmt.Errorf("%w: settlement subscription ownership mismatch", errImageStudioSettlementDependencyInvalid)
	}
	return subscription, nil
}

func (s *ImageStudioJobService) requeueSettlement(ctx context.Context, job ImageStudioJob, err error) {
	message := ""
	if err != nil {
		message = err.Error()
	}
	nextAttemptAt := time.Now().Add(imageStudioRetryDelay(job.AttemptCount + 1))
	_ = s.repo.MarkSettlementRetryable(ctx, job.ID, nextAttemptAt, "settlement_failed", message)
}

func (s *ImageStudioJobService) handleImageStudioSettlementError(ctx context.Context, job ImageStudioJob, err error) {
	if isTerminalImageStudioSettlementError(err) {
		message := ""
		if err != nil {
			message = err.Error()
		}
		_, _ = s.repo.MarkSettlementFailed(ctx, job.ID, time.Now(), "settlement_unrecoverable", message)
		return
	}
	s.requeueSettlement(ctx, job, err)
}

func isTerminalImageStudioSettlementError(err error) bool {
	return errors.Is(err, errImageStudioSettlementPayloadInvalid) ||
		errors.Is(err, errImageStudioSettlementDependencyInvalid) ||
		errors.Is(err, ErrAPIKeyNotFound) ||
		errors.Is(err, ErrAccountNotFound) ||
		errors.Is(err, ErrSubscriptionNotFound)
}

func removeUncommittedImageStudioAssets(originalPath, thumbnailPath string) {
	originalDir := filepath.Dir(strings.TrimSpace(originalPath))
	thumbnailDir := filepath.Dir(strings.TrimSpace(thumbnailPath))
	if originalDir != "." && originalDir == thumbnailDir {
		_ = os.RemoveAll(originalDir)
		return
	}
	_ = removeImageStudioAsset(originalPath)
	_ = removeImageStudioAsset(thumbnailPath)
}

func (s *ImageStudioJobService) persistAssets(jobID int64, imageBytes []byte, mimeType string) (originalPath, thumbnailPath string, fileSize int64, width, height int, retErr error) {
	baseDir := filepath.Join(s.AssetBaseDir(), fmt.Sprintf("%d", jobID))
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return "", "", 0, 0, 0, err
	}
	defer func() {
		if retErr != nil {
			_ = os.RemoveAll(baseDir)
		}
	}()

	cfg, _, err := image.DecodeConfig(bytes.NewReader(imageBytes))
	if err != nil {
		return "", "", 0, 0, 0, err
	}

	originalPath = filepath.Join(baseDir, "original"+imageStudioFileExtFromMIME(mimeType))
	if err := s.writeDurableImageStudioAsset(baseDir, originalPath, imageBytes); err != nil {
		return "", "", 0, 0, 0, err
	}

	thumbnailBytes, err := buildImageStudioThumbnail(imageBytes)
	if err != nil {
		return "", "", 0, 0, 0, err
	}
	thumbnailPath = filepath.Join(baseDir, "thumbnail.jpg")
	if err := s.writeDurableImageStudioAsset(baseDir, thumbnailPath, thumbnailBytes); err != nil {
		return "", "", 0, 0, 0, err
	}
	if err := syncImageStudioAssetDirectory(baseDir); err != nil {
		return "", "", 0, 0, 0, err
	}
	if err := syncImageStudioAssetDirectory(filepath.Dir(baseDir)); err != nil {
		return "", "", 0, 0, 0, err
	}

	return originalPath, thumbnailPath, int64(len(imageBytes)), cfg.Width, cfg.Height, nil
}

func (s *ImageStudioJobService) writeDurableImageStudioAsset(dir, finalPath string, data []byte) (retErr error) {
	temp, err := os.CreateTemp(dir, ".asset-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		if retErr != nil {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o644); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	syncFile := s.syncAssetFile
	if syncFile == nil {
		syncFile = (*os.File).Sync
	}
	if err := syncFile(temp); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	rename := s.renameAssetFile
	if rename == nil {
		rename = os.Rename
	}
	return rename(tempPath, finalPath)
}

func syncImageStudioAssetDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func (s *ImageStudioJobService) failJob(ctx context.Context, job ImageStudioJob, code string, err error) {
	message := ""
	if err != nil {
		message = err.Error()
	}
	if s.expireRunningImageStudioInputs(ctx, job) {
		return
	}
	_ = s.repo.MarkFailed(ctx, job.ID, time.Now(), strings.TrimSpace(code), message)
}

func (s *ImageStudioJobService) handleJobError(ctx context.Context, job ImageStudioJob, code string, err error) {
	if s.expireRunningImageStudioInputs(ctx, job) {
		return
	}
	if s.shouldRetryImageStudioJob(job, err) {
		nextAttemptAt := time.Now().Add(imageStudioRetryDelay(job.AttemptCount + 1))
		_ = s.repo.MarkRetryable(ctx, job.ID, nextAttemptAt, strings.TrimSpace(code), err.Error())
		return
	}
	s.failJob(ctx, job, code, err)
}

func (s *ImageStudioJobService) expireRunningImageStudioInputs(ctx context.Context, job ImageStudioJob) bool {
	if job.InputExpiresAt == nil || job.InputExpiresAt.After(s.now()) {
		return false
	}
	changed, err := s.repo.FailExpiredRunningInputs(ctx, job.ID, s.now())
	if err != nil {
		slog.Warn("image_studio_input_expiry_transition_failed", "job_id", job.ID, "error_kind", imageStudioInputCleanupLogValue(err))
		return true
	}
	if changed {
		s.cleanupDurableImageStudioInputs(ctx, job)
	}
	return true
}

func (s *ImageStudioJobService) shouldRetryImageStudioJob(job ImageStudioJob, err error) bool {
	if job.AttemptCount >= job.MaxAttempts-1 {
		return false
	}
	return isImageStudioRetryableError(err)
}

func imageStudioRetryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return 10 * time.Second
	}
	if attempt == 2 {
		return 30 * time.Second
	}
	return 90 * time.Second
}

func isImageStudioRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var failoverErr *UpstreamFailoverError
	if errors.As(err, &failoverErr) {
		if failoverErr == nil {
			return false
		}
		switch failoverErr.StatusCode {
		case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 529:
			return true
		default:
			return false
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"timeout",
		"timed out",
		"connection reset by peer",
		"connection refused",
		"i/o timeout",
		"stream data interval timeout",
		"server closed idle connection",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func decodeImageStudioResponseImage(body []byte, fallbackOutputFormat string) ([]byte, string, error) {
	b64 := strings.TrimSpace(gjson.GetBytes(body, "data.0.b64_json").String())
	if b64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, "", err
		}
		mimeType := openAIImageOutputMIMEType(fallbackOutputFormat)
		if detected := strings.TrimSpace(httpDetectImageMIME(decoded)); detected != "" {
			mimeType = detected
		}
		return decoded, mimeType, nil
	}

	b64 = strings.TrimSpace(gjson.GetBytes(body, "output.0.result").String())
	if b64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, "", err
		}
		format := strings.TrimSpace(gjson.GetBytes(body, "output.0.output_format").String())
		if format == "" {
			format = fallbackOutputFormat
		}
		mimeType := openAIImageOutputMIMEType(format)
		if detected := strings.TrimSpace(httpDetectImageMIME(decoded)); detected != "" {
			mimeType = detected
		}
		return decoded, mimeType, nil
	}

	urlValue := strings.TrimSpace(gjson.GetBytes(body, "data.0.url").String())
	if strings.HasPrefix(strings.ToLower(urlValue), "data:") {
		return decodeImageStudioDataURL(urlValue)
	}

	urlValue = strings.TrimSpace(gjson.GetBytes(body, "output.0.url").String())
	if strings.HasPrefix(strings.ToLower(urlValue), "data:") {
		return decodeImageStudioDataURL(urlValue)
	}
	return nil, "", fmt.Errorf("missing image payload")
}

func decodeImageStudioDataURL(raw string) ([]byte, string, error) {
	parts := strings.SplitN(raw, ",", 2)
	if len(parts) != 2 {
		return nil, "", fmt.Errorf("invalid data url")
	}
	meta := strings.TrimPrefix(parts[0], "data:")
	meta = strings.TrimSpace(strings.TrimSuffix(meta, ";base64"))
	if meta == "" {
		meta = "image/png"
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, "", err
	}
	return decoded, meta, nil
}

func buildImageStudioThumbnail(srcBytes []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(srcBytes))
	if err != nil {
		return nil, err
	}
	srcBounds := src.Bounds()
	width := srcBounds.Dx()
	height := srcBounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid image size")
	}

	scale := 1.0
	if width >= height && width > imageStudioThumbnailMaxEdge {
		scale = float64(imageStudioThumbnailMaxEdge) / float64(width)
	}
	if height > width && height > imageStudioThumbnailMaxEdge {
		scale = float64(imageStudioThumbnailMaxEdge) / float64(height)
	}
	targetWidth := width
	targetHeight := height
	if scale < 1 {
		targetWidth = max(1, int(float64(width)*scale))
		targetHeight = max(1, int(float64(height)*scale))
	}

	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	stddraw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, stddraw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, srcBounds, stddraw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: imageStudioThumbnailQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func imageStudioFileExtFromMIME(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}

func httpDetectImageMIME(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	detected := http.DetectContentType(data)
	if strings.HasPrefix(strings.ToLower(detected), "image/") {
		return detected
	}
	return ""
}
