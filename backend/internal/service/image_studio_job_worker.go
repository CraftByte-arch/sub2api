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

	startedAt := time.Now()
	acquired, err := s.repo.MarkRunning(ctx, job.ID, startedAt)
	if err != nil {
		return
	}
	if !acquired {
		return
	}
	if err := s.repo.UpdateHeartbeat(ctx, job.ID, startedAt); err != nil {
		return
	}
	stopHeartbeat := s.startImageStudioJobHeartbeat(ctx, job.ID, imageStudioHeartbeatInterval)
	defer stopHeartbeat()

	apiKey, err := s.apiKeyService.GetByID(ctx, job.APIKeyID)
	if err != nil {
		s.failJob(ctx, job.ID, "api_key_not_found", err)
		return
	}
	if apiKey.UserID != job.UserID {
		s.failJob(ctx, job.ID, "api_key_owner_mismatch", ErrImageStudioJobInvalid)
		return
	}
	if !apiKey.IsActive() {
		s.failJob(ctx, job.ID, "api_key_inactive", fmt.Errorf("api key is inactive"))
		return
	}
	if !GroupAllowsImageGeneration(apiKey.Group) {
		s.failJob(ctx, job.ID, "image_generation_disabled", fmt.Errorf("%s", ImageGenerationPermissionMessage()))
		return
	}
	if err := s.ValidateAPIKeyAvailableForImageStudio(ctx, apiKey); err != nil {
		s.failJob(ctx, job.ID, "image_studio_group_not_available", err)
		return
	}
	subscription, err := s.resolveImageStudioSubscription(ctx, apiKey)
	if err != nil {
		s.failJob(ctx, job.ID, "subscription_unavailable", err)
		return
	}
	if s.billingCacheService != nil {
		if err := s.billingCacheService.CheckBillingEligibility(ctx, apiKey.User, apiKey, apiKey.Group, subscription, QuotaPlatform(ctx, apiKey)); err != nil {
			s.failJob(ctx, job.ID, "billing_ineligible", err)
			return
		}
	}

	body := append([]byte(nil), job.RequestPayload...)
	forwarded, err := s.forwardJob(ctx, job, apiKey, body)
	if err != nil {
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

func (s *ImageStudioJobService) markImageStudioSettling(ctx context.Context, jobID int64, settlementPayload json.RawMessage, originalPath, thumbnailPath, mimeType string, fileSizeBytes int64, width, height int, leaseAt time.Time) error {
	if err := s.repo.MarkSettling(ctx, jobID, settlementPayload, originalPath, thumbnailPath, mimeType, fileSizeBytes, width, height, leaseAt); err != nil {
		removeUncommittedImageStudioAssets(originalPath, thumbnailPath)
		return err
	}
	return nil
}

func (s *ImageStudioJobService) recoverStaleRunningJob(ctx context.Context, job ImageStudioJob) {
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
	if s == nil || s.openAIGateway == nil {
		return nil, fmt.Errorf("openai gateway service is not configured")
	}

	endpoint := openAIImagesGenerationsEndpoint
	if job.Mode == ImageStudioJobModeEdit {
		endpoint = openAIImagesEditsEndpoint
	}
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest("POST", endpoint, bytes.NewReader(body))
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

	result, err := s.openAIGateway.ForwardImages(requestCtx, ginCtx, selection.Account, body, parsed, channelMapping.MappedModel)
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
		channelUsageFields: channelMapping.ToUsageFields(requestModel, result.UpstreamModel),
		inboundEndpoint:    endpoint,
		upstreamEndpoint:   endpoint,
	}, nil
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
	leaseAt := time.Now()
	claimed, err := s.repo.ClaimSettling(ctx, job.ID, leaseAt, leaseAt.Add(-5*time.Minute))
	if err != nil || !claimed {
		return
	}
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

func (s *ImageStudioJobService) persistAssets(jobID int64, imageBytes []byte, mimeType string) (string, string, int64, int, int, error) {
	baseDir := filepath.Join(s.AssetBaseDir(), fmt.Sprintf("%d", jobID))
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return "", "", 0, 0, 0, err
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(imageBytes))
	if err != nil {
		return "", "", 0, 0, 0, err
	}

	originalPath := filepath.Join(baseDir, "original"+imageStudioFileExtFromMIME(mimeType))
	if err := os.WriteFile(originalPath, imageBytes, 0o644); err != nil {
		return "", "", 0, 0, 0, err
	}

	thumbnailBytes, err := buildImageStudioThumbnail(imageBytes)
	if err != nil {
		return "", "", 0, 0, 0, err
	}
	thumbnailPath := filepath.Join(baseDir, "thumbnail.jpg")
	if err := os.WriteFile(thumbnailPath, thumbnailBytes, 0o644); err != nil {
		return "", "", 0, 0, 0, err
	}

	return originalPath, thumbnailPath, int64(len(imageBytes)), cfg.Width, cfg.Height, nil
}

func (s *ImageStudioJobService) failJob(ctx context.Context, jobID int64, code string, err error) {
	message := ""
	if err != nil {
		message = err.Error()
	}
	_ = s.repo.MarkFailed(ctx, jobID, time.Now(), strings.TrimSpace(code), message)
}

func (s *ImageStudioJobService) handleJobError(ctx context.Context, job ImageStudioJob, code string, err error) {
	if s.shouldRetryImageStudioJob(job, err) {
		nextAttemptAt := time.Now().Add(imageStudioRetryDelay(job.AttemptCount + 1))
		_ = s.repo.MarkRetryable(ctx, job.ID, nextAttemptAt, strings.TrimSpace(code), err.Error())
		return
	}
	s.failJob(ctx, job.ID, code, err)
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
