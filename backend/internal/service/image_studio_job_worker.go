package service

import (
	"bytes"
	"context"
	"encoding/base64"
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
	imageStudioThumbnailMaxEdge = 320
	imageStudioThumbnailQuality = 82
	imageStudioMaxAttempts      = 3
)

type imageStudioChargeResult struct {
	chargedAmountUSD float64
	usedBalance      bool
}

func (s *ImageStudioJobService) SetRuntimeDependencies(
	openAIGateway *OpenAIGatewayService,
	apiKeyService *APIKeyService,
	billingCacheService *BillingCacheService,
	userRepo UserRepository,
	usageCardService *UsageCardService,
) {
	if s == nil {
		return
	}
	s.openAIGateway = openAIGateway
	s.apiKeyService = apiKeyService
	s.billingCacheService = billingCacheService
	s.userRepo = userRepo
	s.usageCardService = usageCardService
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
	if s.billingCacheService != nil {
		if err := s.billingCacheService.CheckBillingEligibility(ctx, apiKey.User, apiKey, apiKey.Group, nil, QuotaPlatform(ctx, apiKey)); err != nil {
			s.failJob(ctx, job.ID, "billing_ineligible", err)
			return
		}
	}

	body := append([]byte(nil), job.RequestPayload...)
	result, rawBody, err := s.forwardJob(ctx, job, apiKey, body)
	if err != nil {
		s.handleJobError(ctx, job, "upstream_failed", err)
		return
	}

	imageBytes, mimeType, err := decodeImageStudioResponseImage(rawBody, job.OutputFormat)
	if err != nil {
		s.handleJobError(ctx, job, "invalid_image_response", err)
		return
	}

	chargeResult, err := s.chargeJob(ctx, apiKey, job.EstimatedCostUSD)
	if err != nil {
		s.handleJobError(ctx, job, "billing_failed", err)
		return
	}

	originalPath, thumbnailPath, fileSizeBytes, width, height, err := s.persistAssets(job.ID, imageBytes, mimeType)
	if err != nil {
		s.handleJobError(ctx, job, "asset_persist_failed", err)
		return
	}

	if s.apiKeyService != nil && chargeResult.chargedAmountUSD > 0 {
		_ = s.apiKeyService.UpdateQuotaUsed(ctx, apiKey.ID, chargeResult.chargedAmountUSD)
		_ = s.apiKeyService.UpdateRateLimitUsage(ctx, apiKey.ID, chargeResult.chargedAmountUSD)
	}
	if chargeResult.usedBalance && s.billingCacheService != nil && chargeResult.chargedAmountUSD > 0 {
		s.billingCacheService.QueueDeductBalance(apiKey.UserID, chargeResult.chargedAmountUSD)
	}

	completedAt := time.Now()
	_ = s.repo.MarkSucceeded(
		ctx,
		job.ID,
		completedAt,
		chargeResult.chargedAmountUSD,
		originalPath,
		thumbnailPath,
		mimeType,
		fileSizeBytes,
		width,
		height,
		s.ResolveRetention(completedAt),
	)
	_ = result
}

func (s *ImageStudioJobService) forwardJob(ctx context.Context, job ImageStudioJob, apiKey *APIKey, body []byte) (*OpenAIForwardResult, []byte, error) {
	if s == nil || s.openAIGateway == nil {
		return nil, nil, fmt.Errorf("openai gateway service is not configured")
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
		return nil, nil, err
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
		return nil, nil, err
	}
	if selection == nil || selection.Account == nil {
		return nil, nil, fmt.Errorf("no available image account")
	}
	if selection.ReleaseFunc != nil {
		defer selection.ReleaseFunc()
	}

	result, err := s.openAIGateway.ForwardImages(requestCtx, ginCtx, selection.Account, body, parsed, channelMapping.MappedModel)
	if err != nil {
		return result, recorder.Body.Bytes(), err
	}
	if recorder.Code >= 400 {
		return result, recorder.Body.Bytes(), fmt.Errorf("upstream request failed with status %d", recorder.Code)
	}
	return result, recorder.Body.Bytes(), nil
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

func (s *ImageStudioJobService) forwardResponsesJob(ctx context.Context, job ImageStudioJob, apiKey *APIKey, body []byte) (*OpenAIForwardResult, []byte, error) {
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest("POST", openAIResponsesEndpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ginCtx.Request = req
	ginCtx.Set("api_key", apiKey)

	requestModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if requestModel == "" {
		return nil, nil, fmt.Errorf("responses model is required")
	}
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
		return nil, nil, err
	}
	if selection == nil || selection.Account == nil {
		return nil, nil, fmt.Errorf("no available image account")
	}
	if selection.ReleaseFunc != nil {
		defer selection.ReleaseFunc()
	}

	result, err := s.openAIGateway.Forward(ctx, ginCtx, selection.Account, forwardBody)
	if err != nil {
		return result, recorder.Body.Bytes(), err
	}
	if recorder.Code >= 400 {
		return result, recorder.Body.Bytes(), fmt.Errorf("upstream request failed with status %d", recorder.Code)
	}
	return result, recorder.Body.Bytes(), nil
}

func (s *ImageStudioJobService) chargeJob(ctx context.Context, apiKey *APIKey, amount float64) (imageStudioChargeResult, error) {
	if amount <= 0 {
		return imageStudioChargeResult{}, nil
	}
	if s == nil || s.userRepo == nil {
		return imageStudioChargeResult{}, fmt.Errorf("user repository is not configured")
	}
	priority := BillingPriorityBalanceFirst
	if s.billingCacheService != nil {
		priority = s.billingCacheService.ResolveWalletBillingPriority(ctx, apiKey)
	}

	tryUsageCard := func() error {
		if s.usageCardService == nil {
			return ErrUsageCardUnavailable
		}
		_, err := s.usageCardService.DeductFirstAvailable(ctx, apiKey.UserID, amount, time.Now())
		return err
	}
	tryBalance := func() error {
		return s.userRepo.DeductBalance(ctx, apiKey.UserID, amount)
	}

	switch priority {
	case BillingPriorityUsageCardOnly:
		if err := tryUsageCard(); err != nil {
			return imageStudioChargeResult{}, err
		}
		return imageStudioChargeResult{chargedAmountUSD: amount}, nil
	case BillingPriorityUsageCardFirst:
		if err := tryUsageCard(); err == nil {
			return imageStudioChargeResult{chargedAmountUSD: amount}, nil
		}
		if err := tryBalance(); err != nil {
			return imageStudioChargeResult{}, err
		}
		return imageStudioChargeResult{chargedAmountUSD: amount, usedBalance: true}, nil
	case BillingPriorityBalanceOnly:
		if err := tryBalance(); err != nil {
			return imageStudioChargeResult{}, err
		}
		return imageStudioChargeResult{chargedAmountUSD: amount, usedBalance: true}, nil
	default:
		if err := tryBalance(); err == nil {
			return imageStudioChargeResult{chargedAmountUSD: amount, usedBalance: true}, nil
		}
		if err := tryUsageCard(); err != nil {
			return imageStudioChargeResult{}, err
		}
		return imageStudioChargeResult{chargedAmountUSD: amount}, nil
	}
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
