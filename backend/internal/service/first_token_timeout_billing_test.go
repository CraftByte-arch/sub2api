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
