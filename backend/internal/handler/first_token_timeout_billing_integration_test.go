package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type firstTokenBillingAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

func (r *firstTokenBillingAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			account := r.accounts[i]
			return &account, nil
		}
	}
	return nil, service.ErrNoAvailableAccounts
}

func (r *firstTokenBillingAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.schedulable(platform), nil
}

func (r *firstTokenBillingAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.schedulable(platform), nil
}

func (r *firstTokenBillingAccountRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.schedulable(platform), nil
}

func (r *firstTokenBillingAccountRepo) schedulable(platform string) []service.Account {
	out := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform && account.IsSchedulable() {
			out = append(out, account)
		}
	}
	return out
}

type firstTokenBillingHTTPUpstream struct {
	service.HTTPUpstream
	mu    sync.Mutex
	calls []int64
}

func (u *firstTokenBillingHTTPUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.calls = append(u.calls, accountID)
	u.mu.Unlock()

	var body io.ReadCloser
	if accountID == 1 {
		body = &firstTokenBlockingBody{ctx: req.Context()}
	} else {
		body = io.NopCloser(bytes.NewBufferString(
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ttft_success\",\"model\":\"gpt-5.1\",\"usage\":{\"input_tokens\":100,\"output_tokens\":20,\"total_tokens\":120}}}\n\n",
		))
	}
	header := http.Header{"Content-Type": []string{"text/event-stream"}}
	if accountID == 2 {
		header.Set("x-request-id", "resp_ttft_success")
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       body,
		Request:    req,
	}, nil
}

func (u *firstTokenBillingHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func (u *firstTokenBillingHTTPUpstream) accountCalls() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.calls...)
}

type firstTokenBlockingBody struct {
	ctx context.Context
}

func (b *firstTokenBlockingBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, context.Cause(b.ctx)
}

func (b *firstTokenBlockingBody) Close() error { return nil }

type firstTokenBillingUsageRepo struct {
	service.UsageLogRepository
	mu      sync.Mutex
	calls   int
	lastLog *service.UsageLog
}

func (r *firstTokenBillingUsageRepo) Create(_ context.Context, log *service.UsageLog) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	copy := *log
	r.lastLog = &copy
	return true, nil
}

func (r *firstTokenBillingUsageRepo) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *firstTokenBillingUsageRepo) recordedLog() *service.UsageLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastLog == nil {
		return nil
	}
	copy := *r.lastLog
	return &copy
}

type firstTokenBillingUserRepo struct {
	service.UserRepository
	mu           sync.Mutex
	deductCalls  int
	deductUserID int64
	deductAmount float64
}

func (r *firstTokenBillingUserRepo) GetByID(_ context.Context, id int64) (*service.User, error) {
	return &service.User{ID: id, Status: service.StatusActive, Balance: 100}, nil
}

func (r *firstTokenBillingUserRepo) DeductBalance(_ context.Context, userID int64, amount float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deductCalls++
	r.deductUserID = userID
	r.deductAmount = amount
	return nil
}

func (r *firstTokenBillingUserRepo) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deductCalls
}

func (r *firstTokenBillingUserRepo) deduction() (int64, float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deductUserID, r.deductAmount
}

type firstTokenBillingSettingRepo struct {
	service.SettingRepository
}

func (firstTokenBillingSettingRepo) Set(context.Context, string, string) error { return nil }

type firstTokenBillingGatewayCache struct{}

func (firstTokenBillingGatewayCache) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	return 0, errors.New("not found")
}

func (firstTokenBillingGatewayCache) SetSessionAccountID(context.Context, int64, string, int64, time.Duration) error {
	return nil
}

func (firstTokenBillingGatewayCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (firstTokenBillingGatewayCache) DeleteSessionAccountID(context.Context, int64, string) error {
	return nil
}

func TestFirstTokenTimeoutBillingFailoverThenSuccessRecordsOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(9001)
	accountRepo := &firstTokenBillingAccountRepo{accounts: []service.Account{
		{ID: 1, Name: "slow", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Priority: 0, Credentials: map[string]any{"access_token": "token-1"}},
		{ID: 2, Name: "fast", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Priority: 1, Credentials: map[string]any{"access_token": "token-2"}},
	}}
	upstream := &firstTokenBillingHTTPUpstream{}
	usageRepo := &firstTokenBillingUsageRepo{}
	userRepo := &firstTokenBillingUserRepo{}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 3
	billingCache := service.NewBillingCacheService(nil, userRepo, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	gateway := service.NewOpenAIGatewayService(
		accountRepo, usageRepo, nil, userRepo, nil, nil, firstTokenBillingGatewayCache{}, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, billingCache, upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	concurrencyCache := &concurrencyCacheMock{
		acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
	}
	policy := service.NewFirstTokenTimeoutPolicy(firstTokenBillingSettingRepo{}, nil)
	require.NoError(t, policy.Update(context.Background(), service.FirstTokenTimeoutSettings{Enabled: true, TimeoutSeconds: 1}))
	statsRecorder := &firstTokenRunnerStatsRecorderSpy{}
	h := NewOpenAIGatewayHandler(
		gateway,
		service.NewConcurrencyService(concurrencyCache),
		billingCache,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
		nil, nil, nil, nil, cfg, policy,
		statsRecorder,
	)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.1","stream":true,"input":"hello"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	apiKey := &service.APIKey{
		ID: 7001, GroupID: &groupID,
		User:  &service.User{ID: 8001, Status: service.StatusActive, Balance: 100},
		Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 1},
	}
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})

	h.Responses(c)

	require.Equal(t, []int64{1, 2}, upstream.accountCalls())
	require.Equal(t, 1, usageRepo.callCount())
	require.Equal(t, 1, userRepo.callCount())
	require.Contains(t, recorder.Body.String(), "resp_ttft_success")
	deltas := statsRecorder.snapshot()
	require.Len(t, deltas, 3)
	require.Equal(t, service.FirstTokenStatsScopeAttempt, deltas[0].Scope)
	require.Equal(t, int64(1), deltas[0].AccountID)
	require.Equal(t, service.FirstTokenStatsAttemptTTFTTimeout, deltas[0].Outcome)
	require.Equal(t, service.FirstTokenStatsScopeAttempt, deltas[1].Scope)
	require.Equal(t, int64(2), deltas[1].AccountID)
	require.Equal(t, service.FirstTokenStatsAttemptSuccess, deltas[1].Outcome)
	require.Equal(t, int64(1), deltas[1].TTFTSampleCount)
	require.Equal(t, service.FirstTokenStatsScopeRequest, deltas[2].Scope)
	require.Equal(t, service.FirstTokenStatsRequestRecoveredAfterTTFT, deltas[2].Outcome)
	require.Equal(t, int64(1), deltas[2].TTFTAffectedCount)

	rawEvents, ok := c.Get(service.OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := rawEvents.([]*service.OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1, "the real Responses reader and runner must not both record the TTFT attempt")
	require.Equal(t, service.UpstreamErrorTypeFirstTokenTimeout, events[0].Kind)

	usageLog := usageRepo.recordedLog()
	require.NotNil(t, usageLog)
	require.Equal(t, int64(2), usageLog.AccountID)
	require.Equal(t, "resp_ttft_success", usageLog.RequestID)
	require.Equal(t, 100, usageLog.InputTokens)
	require.Equal(t, 20, usageLog.OutputTokens)
	require.Equal(t, 120, usageLog.TotalTokens())
	deductUserID, deductAmount := userRepo.deduction()
	require.Equal(t, int64(8001), deductUserID)
	require.Positive(t, deductAmount)
}

func TestFirstTokenTimeoutBillingExhaustedCandidatesReturns504(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(9003)
	accountRepo := &firstTokenBillingAccountRepo{accounts: []service.Account{
		{ID: 1, Name: "slow", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"access_token": "token-1"}},
	}}
	upstream := &firstTokenBillingHTTPUpstream{}
	usageRepo := &firstTokenBillingUsageRepo{}
	userRepo := &firstTokenBillingUserRepo{}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 3
	billingCache := service.NewBillingCacheService(nil, userRepo, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	gateway := service.NewOpenAIGatewayService(
		accountRepo, usageRepo, nil, userRepo, nil, nil, firstTokenBillingGatewayCache{}, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, billingCache, upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	concurrencyCache := &concurrencyCacheMock{
		acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
	}
	policy := service.NewFirstTokenTimeoutPolicy(firstTokenBillingSettingRepo{}, nil)
	require.NoError(t, policy.Update(context.Background(), service.FirstTokenTimeoutSettings{Enabled: true, TimeoutSeconds: 1}))
	statsRecorder := &firstTokenRunnerStatsRecorderSpy{}
	h := NewOpenAIGatewayHandler(
		gateway,
		service.NewConcurrencyService(concurrencyCache),
		billingCache,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
		nil, nil, nil, nil, cfg, policy, statsRecorder,
	)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.1","stream":true,"input":"hello"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	apiKey := &service.APIKey{
		ID: 7003, GroupID: &groupID,
		User:  &service.User{ID: 8003, Status: service.StatusActive, Balance: 100},
		Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 1},
	}
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})

	h.Responses(c)

	require.Equal(t, []int64{1}, upstream.accountCalls())
	require.Equal(t, http.StatusGatewayTimeout, recorder.Code)
	require.Contains(t, recorder.Body.String(), service.UpstreamErrorTypeFirstTokenTimeout)
	require.Zero(t, usageRepo.callCount())
	require.Zero(t, userRepo.callCount())
	deltas := statsRecorder.snapshot()
	require.Len(t, deltas, 2)
	require.Equal(t, service.FirstTokenStatsAttemptTTFTTimeout, deltas[0].Outcome)
	require.Equal(t, service.FirstTokenStatsRequestTTFTExhausted, deltas[1].Outcome)
	require.Equal(t, int64(1), deltas[1].TTFTAffectedCount)
}

func TestFirstTokenTrackingNoAccountSelectionRecordsRequestOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(9002)
	accountRepo := &firstTokenBillingAccountRepo{}
	usageRepo := &firstTokenBillingUsageRepo{}
	userRepo := &firstTokenBillingUserRepo{}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	billingCache := service.NewBillingCacheService(nil, userRepo, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	gateway := service.NewOpenAIGatewayService(
		accountRepo, usageRepo, nil, userRepo, nil, nil, firstTokenBillingGatewayCache{}, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, billingCache, &firstTokenBillingHTTPUpstream{},
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	concurrencyCache := &concurrencyCacheMock{
		acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
	}
	policy := service.NewFirstTokenTimeoutPolicy(firstTokenBillingSettingRepo{}, nil)
	require.NoError(t, policy.Update(context.Background(), service.FirstTokenTimeoutSettings{Enabled: true, TimeoutSeconds: 23}))
	statsRecorder := &firstTokenRunnerStatsRecorderSpy{}
	h := NewOpenAIGatewayHandler(
		gateway,
		service.NewConcurrencyService(concurrencyCache),
		billingCache,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
		nil, nil, nil, nil, cfg, policy, statsRecorder,
	)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.1","stream":true,"input":"hello"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	apiKey := &service.APIKey{
		ID: 7002, GroupID: &groupID,
		User:  &service.User{ID: 8002, Status: service.StatusActive, Balance: 100},
		Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 1},
	}
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})

	h.Responses(c)

	deltas := statsRecorder.snapshot()
	require.Len(t, deltas, 1)
	require.Equal(t, service.FirstTokenStatsScopeRequest, deltas[0].Scope)
	require.Zero(t, deltas[0].AccountID)
	require.Equal(t, service.FirstTokenStatsRequestOtherFailure, deltas[0].Outcome)
	require.Equal(t, service.FirstTokenStatsFailureOther, deltas[0].FailureKind)
	require.Equal(t, 23, deltas[0].TimeoutSeconds)
}
