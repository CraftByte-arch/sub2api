package main

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type cleanupFirstTokenStatsRepository struct {
	flushed chan []service.FirstTokenStatsDelta
}

func (r *cleanupFirstTokenStatsRepository) UpsertBatch(_ context.Context, deltas []service.FirstTokenStatsDelta) error {
	copyDeltas := append([]service.FirstTokenStatsDelta(nil), deltas...)
	select {
	case r.flushed <- copyDeltas:
	default:
	}
	return nil
}

func (*cleanupFirstTokenStatsRepository) QueryOverview(context.Context, service.FirstTokenStatsOverviewFilter) (*service.FirstTokenStatsOverview, error) {
	return nil, nil
}

func (*cleanupFirstTokenStatsRepository) QueryAccounts(context.Context, service.FirstTokenStatsAccountFilter) (*service.FirstTokenStatsAccountPage, error) {
	return nil, nil
}

func (*cleanupFirstTokenStatsRepository) DeleteBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func TestProvideServiceBuildInfo(t *testing.T) {
	in := handler.BuildInfo{
		Version:   "v-test",
		BuildType: "release",
	}
	out := provideServiceBuildInfo(in)
	require.Equal(t, in.Version, out.Version)
	require.Equal(t, in.BuildType, out.BuildType)
}

func TestProvideCleanup_WithMinimalDependencies_NoPanic(t *testing.T) {
	cfg := &config.Config{}

	oauthSvc := service.NewOAuthService(nil, nil)
	openAIOAuthSvc := service.NewOpenAIOAuthService(nil, nil)
	geminiOAuthSvc := service.NewGeminiOAuthService(nil, nil, nil, nil, cfg)
	antigravityOAuthSvc := service.NewAntigravityOAuthService(nil)

	tokenRefreshSvc := service.NewTokenRefreshService(
		nil,
		oauthSvc,
		openAIOAuthSvc,
		geminiOAuthSvc,
		antigravityOAuthSvc,
		nil,
		nil,
		cfg,
		nil,
	)
	accountExpirySvc := service.NewAccountExpiryService(nil, time.Second)
	proxyExpirySvc := service.NewProxyExpiryService(nil, time.Second)
	subscriptionExpirySvc := service.NewSubscriptionExpiryService(nil, time.Second)
	pricingSvc := service.NewPricingService(cfg, nil)
	emailQueueSvc := service.NewEmailQueueService(nil, 1)
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	idempotencyCleanupSvc := service.NewIdempotencyCleanupService(nil, cfg)
	schedulerSnapshotSvc := service.NewSchedulerSnapshotService(nil, nil, nil, nil, cfg)
	opsSystemLogSinkSvc := service.NewOpsSystemLogSink(nil)

	cleanup := provideCleanup(
		nil, // entClient
		nil, // redis
		&service.OpsMetricsCollector{},
		&service.OpsAggregationService{},
		&service.OpsAlertEvaluatorService{},
		&service.OpsCleanupService{},
		&service.OpsScheduledReportService{},
		opsSystemLogSinkSvc,
		schedulerSnapshotSvc,
		tokenRefreshSvc,
		accountExpirySvc,
		proxyExpirySvc,
		subscriptionExpirySvc,
		&service.UsageCleanupService{},
		idempotencyCleanupSvc,
		&service.BatchImageCleanupService{},
		nil, // batchImageWorker
		pricingSvc,
		emailQueueSvc,
		billingCacheSvc,
		&service.UsageRecordWorkerPool{},
		&service.SubscriptionService{},
		oauthSvc,
		openAIOAuthSvc,
		geminiOAuthSvc,
		antigravityOAuthSvc,
		nil, // grokOAuth
		nil, // openAIGateway
		nil, // scheduledTestRunner
		nil, // backupSvc
		nil, // paymentOrderExpiry
		nil, // channelMonitorRunner
		nil, // imageStudioJobService
		nil, // quotaFlusher
		nil, // firstTokenTimeoutPolicy
		nil, // firstTokenStatsRecorder
	)

	require.NotPanics(t, func() {
		cleanup()
	})
}

func TestProvideCleanup_StartsAndStopsFirstTokenStatsRecorder(t *testing.T) {
	cfg := &config.Config{}
	oauthSvc := service.NewOAuthService(nil, nil)
	openAIOAuthSvc := service.NewOpenAIOAuthService(nil, nil)
	geminiOAuthSvc := service.NewGeminiOAuthService(nil, nil, nil, nil, cfg)
	antigravityOAuthSvc := service.NewAntigravityOAuthService(nil)
	tokenRefreshSvc := service.NewTokenRefreshService(nil, oauthSvc, openAIOAuthSvc, geminiOAuthSvc, antigravityOAuthSvc, nil, nil, cfg, nil)
	accountExpirySvc := service.NewAccountExpiryService(nil, time.Second)
	proxyExpirySvc := service.NewProxyExpiryService(nil, time.Second)
	subscriptionExpirySvc := service.NewSubscriptionExpiryService(nil, time.Second)
	pricingSvc := service.NewPricingService(cfg, nil)
	emailQueueSvc := service.NewEmailQueueService(nil, 1)
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	idempotencyCleanupSvc := service.NewIdempotencyCleanupService(nil, cfg)
	schedulerSnapshotSvc := service.NewSchedulerSnapshotService(nil, nil, nil, nil, cfg)
	opsSystemLogSinkSvc := service.NewOpsSystemLogSink(nil)
	repo := &cleanupFirstTokenStatsRepository{flushed: make(chan []service.FirstTokenStatsDelta, 1)}
	statsRecorder := service.NewFirstTokenTimeoutStatsRecorder(repo)

	cleanup := provideCleanup(
		nil, nil, &service.OpsMetricsCollector{}, &service.OpsAggregationService{}, &service.OpsAlertEvaluatorService{},
		&service.OpsCleanupService{}, &service.OpsScheduledReportService{}, opsSystemLogSinkSvc, schedulerSnapshotSvc,
		tokenRefreshSvc, accountExpirySvc, proxyExpirySvc, subscriptionExpirySvc, &service.UsageCleanupService{},
		idempotencyCleanupSvc, &service.BatchImageCleanupService{}, nil, pricingSvc, emailQueueSvc, billingCacheSvc,
		&service.UsageRecordWorkerPool{}, &service.SubscriptionService{}, oauthSvc, openAIOAuthSvc, geminiOAuthSvc,
		antigravityOAuthSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		statsRecorder,
	)
	statsRecorder.Record(service.FirstTokenStatsDelta{
		BucketStart: time.Now().UTC().Truncate(time.Hour), Scope: service.FirstTokenStatsScopeRequest,
		Protocol: string(service.ProtocolResponses), Model: "gpt-5", TimeoutSeconds: 30,
		Outcome: service.FirstTokenStatsRequestSuccess, SampleCount: 1,
	})
	require.Eventually(t, func() bool { return statsRecorder.Health().PendingSamples == 1 }, time.Second, 10*time.Millisecond)

	cleanup()

	select {
	case deltas := <-repo.flushed:
		require.Len(t, deltas, 1)
		require.Equal(t, service.FirstTokenStatsRequestSuccess, deltas[0].Outcome)
	case <-time.After(time.Second):
		t.Fatal("cleanup did not flush first token stats recorder")
	}
	require.Zero(t, statsRecorder.Health().PendingSamples)
}
