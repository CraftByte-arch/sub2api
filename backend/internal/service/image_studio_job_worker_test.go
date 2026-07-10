package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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

type imageStudioWorkerRepoStub struct {
	ImageStudioJobRepository
	claimSettling                bool
	markSucceededErr             error
	markRunningCalls             int
	claimSettlingCalls           int
	markSucceededCalls           int
	markSettlementRetryableCalls int
	chargedAmountUSD             float64
	retryErrorCode               string
	markStaleRunningChanged      bool
	markStaleRunningCalls        int
	heartbeatCalls               int
	heartbeatCh                  chan struct{}
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

func (r *imageStudioWorkerRepoStub) MarkStaleRunningFailed(context.Context, int64, time.Time, time.Time) (bool, error) {
	r.markStaleRunningCalls++
	return r.markStaleRunningChanged, nil
}

func (r *imageStudioWorkerRepoStub) MarkRunning(context.Context, int64, time.Time) (bool, error) {
	r.markRunningCalls++
	return true, nil
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
}

func (r *imageStudioAPIKeyRepoStub) GetByID(context.Context, int64) (*APIKey, error) {
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

	t.Run("non-retryable validation", func(t *testing.T) {
		require.False(t, isImageStudioRetryableError(errors.New("model is required")))
	})
}

func TestImageStudioRetryDelay(t *testing.T) {
	require.Equal(t, 10*time.Second, imageStudioRetryDelay(1))
	require.Equal(t, 30*time.Second, imageStudioRetryDelay(2))
	require.Equal(t, 90*time.Second, imageStudioRetryDelay(3))
}
