package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type firstTokenTimeoutAccountRepo struct {
	AccountRepository
	updateCalls         int
	setSchedulableCalls int
	tempUnscheduleCalls int
}

func (r *firstTokenTimeoutAccountRepo) Update(context.Context, *Account) error {
	r.updateCalls++
	return nil
}

func (r *firstTokenTimeoutAccountRepo) SetSchedulable(context.Context, int64, bool) error {
	r.setSchedulableCalls++
	return nil
}

func (r *firstTokenTimeoutAccountRepo) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	r.tempUnscheduleCalls++
	return nil
}

func TestFirstTokenTimeoutFailoverError(t *testing.T) {
	err := NewFirstTokenTimeoutFailoverError()
	require.Equal(t, http.StatusGatewayTimeout, err.StatusCode)
	require.Equal(t, "first_token_timeout", err.ErrorType)
	require.False(t, err.RetryableOnSameAccount)
}

func TestFirstTokenPreludeOverflowFailoverError(t *testing.T) {
	err := NewFirstTokenPreludeOverflowFailoverError()
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Equal(t, "first_token_prelude_overflow", err.ErrorType)
	require.False(t, err.RetryableOnSameAccount)
}

func TestFirstTokenTimeoutSchedulerRuntimeFailureDoesNotChangeAccountState(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()

	repo := &firstTokenTimeoutAccountRepo{}
	svc := &OpenAIGatewayService{
		accountRepo:      repo,
		rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("true"),
	}

	svc.ReportOpenAIAccountFirstTokenTimeout(42)

	errorRate, _, _ := svc.openaiAccountStats.snapshot(42)
	require.Equal(t, 0.2, errorRate)
	require.Zero(t, repo.updateCalls)
	require.Zero(t, repo.setSchedulableCalls)
	require.Zero(t, repo.tempUnscheduleCalls)
}

func TestFirstTokenTimeoutBillingSkipsFailedAttemptAndBillsSuccessOnce(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, subRepo, nil)

	timeoutErr := NewFirstTokenTimeoutFailoverError()
	require.Equal(t, UpstreamErrorTypeFirstTokenTimeout, timeoutErr.ErrorType)
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result:  nil,
		APIKey:  &APIKey{ID: 101, Group: &Group{RateMultiplier: 1}},
		User:    &User{ID: 201},
		Account: &Account{ID: 301, Type: AccountTypeAPIKey},
	})
	require.Error(t, err)
	require.Zero(t, usageRepo.calls)
	require.Zero(t, userRepo.deductCalls)

	err = svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: "first-token-timeout-success",
			Model:     "gpt-5.1",
			Usage: OpenAIUsage{
				InputTokens:  100,
				OutputTokens: 20,
			},
			Duration: time.Second,
		},
		APIKey:  &APIKey{ID: 101, Group: &Group{RateMultiplier: 1}},
		User:    &User{ID: 201},
		Account: &Account{ID: 302, Type: AccountTypeAPIKey},
	})
	require.NoError(t, err)
	require.Equal(t, 1, usageRepo.calls)
	require.Equal(t, 1, userRepo.deductCalls)
	require.Positive(t, userRepo.lastAmount)
}
