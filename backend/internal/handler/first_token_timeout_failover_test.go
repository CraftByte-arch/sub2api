package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type firstTokenTimeoutOpsRepo struct {
	service.OpsRepository
	inserted chan *service.OpsInsertErrorLogInput
}

func resetFirstTokenTimeoutOpsLogger(t *testing.T) {
	t.Helper()
	StopOpsErrorLogWorkers()
	resetOpsErrorLoggerStateForTest(t)
}

func configureFirstTokenTimeoutOpsQueue() {
	opsErrorLogOnce.Do(func() {})
	opsErrorLogMu.Lock()
	opsErrorLogQueue = make(chan opsErrorLogJob, 1)
	opsErrorLogMu.Unlock()
}

func flushFirstTokenTimeoutOpsJob(t *testing.T) {
	t.Helper()
	opsErrorLogMu.RLock()
	queue := opsErrorLogQueue
	opsErrorLogMu.RUnlock()
	require.NotNil(t, queue)
	job := <-queue
	opsErrorLogQueueLen.Add(-1)
	require.NoError(t, job.ops.RecordError(context.Background(), job.entry))
}

func (r *firstTokenTimeoutOpsRepo) InsertErrorLog(_ context.Context, input *service.OpsInsertErrorLogInput) (int64, error) {
	r.inserted <- input
	return 1, nil
}

func TestFirstTokenTimeoutLogsOnlySafeAttemptMetadata(t *testing.T) {
	core, observed := observer.New(zap.WarnLevel)
	c, _ := newFirstTokenRunnerContext()
	c.Request = c.Request.WithContext(logger.IntoContext(c.Request.Context(), zap.New(core)))
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 5 * time.Millisecond}}
	meta := FirstTokenAttemptMetadata{
		Protocol:     service.ProtocolResponses,
		Platform:     service.PlatformOpenAI,
		AccountID:    42,
		Model:        "gpt-5",
		AttemptIndex: 3,
		SwitchCount:  2,
	}

	_, err := runFirstTokenAttempt(c, policy, meta, func(ctx context.Context) (*service.ForwardResult, error) {
		<-ctx.Done()
		return nil, context.Cause(ctx)
	})

	var failoverErr *service.UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	entries := observed.All()
	require.Len(t, entries, 1)
	require.Equal(t, "gateway.first_token_timeout", entries[0].Message)
	keys := make([]string, 0, len(entries[0].Context))
	for _, field := range entries[0].Context {
		keys = append(keys, field.Key)
	}
	sort.Strings(keys)
	require.Equal(t, []string{"account", "attempt", "elapsed", "model", "platform", "protocol", "switch", "threshold"}, keys)
}

func (a *firstTokenResponseGateControlledAttempt) Elapsed() time.Duration {
	return 7 * time.Millisecond
}

func TestFirstTokenTimeoutCommitTerminalRaceLogsExactlyOnce(t *testing.T) {
	core, observed := observer.New(zap.WarnLevel)
	c, _ := newFirstTokenRunnerContext()
	c.Request = c.Request.WithContext(logger.IntoContext(c.Request.Context(), zap.New(core)))
	base, _ := newFirstTokenResponseGateTestWriter()
	realAttempt := service.NewFirstTokenAttempt(context.Background(), time.Hour)
	t.Cleanup(realAttempt.Close)
	gate := NewFirstTokenResponseGate(base, realAttempt)
	controlled := newFirstTokenResponseGateControlledAttempt(service.ErrFirstTokenTimeout)
	gate.attempt = controlled

	err := finishFirstTokenAttempt(
		c,
		service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: time.Second},
		FirstTokenAttemptMetadata{Protocol: service.ProtocolResponses, Platform: service.PlatformOpenAI, AccountID: 42, Model: "gpt-5", AttemptIndex: 2, SwitchCount: 1},
		controlled,
		c.Request.Context(),
		gate,
		errors.New("terminal upstream error"),
	)

	var failoverErr *service.UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, service.UpstreamErrorTypeFirstTokenTimeout, failoverErr.ErrorType)
	require.True(t, controlled.cancelCalled)
	require.Len(t, observed.All(), 1)
	require.Equal(t, "gateway.first_token_timeout", observed.All()[0].Message)
}

func TestFirstTokenTimeoutOpsRecoveredAfterFailover(t *testing.T) {
	c, _ := newFirstTokenRunnerContext()
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 5 * time.Millisecond}}
	meta := FirstTokenAttemptMetadata{Protocol: service.ProtocolResponses, Platform: service.PlatformOpenAI, AccountID: 42, Model: "gpt-5", AttemptIndex: 1}

	_, err := runFirstTokenAttempt(c, policy, meta, func(ctx context.Context) (*service.ForwardResult, error) {
		<-ctx.Done()
		return nil, context.Cause(ctx)
	})
	var failoverErr *service.UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)

	meta.AccountID = 43
	meta.AttemptIndex = 2
	meta.SwitchCount = 1
	_, err = runFirstTokenAttempt(c, policy, meta, func(ctx context.Context) (*service.ForwardResult, error) {
		require.NoError(t, service.CommitFirstTokenFromContext(ctx))
		return &service.ForwardResult{RequestID: "recovered"}, nil
	})
	require.NoError(t, err)

	requireFirstTokenTimeoutOpsEvent(t, c, 42, 1, 0)
}

func TestFirstTokenTimeoutOpsExhaustedKeepsSyntheticFailure(t *testing.T) {
	c, recorder := newFirstTokenRunnerContext()
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 5 * time.Millisecond}}
	meta := FirstTokenAttemptMetadata{Protocol: service.ProtocolResponses, Platform: service.PlatformOpenAI, AccountID: 42, Model: "gpt-5", AttemptIndex: 2, SwitchCount: 1}

	_, err := runFirstTokenAttempt(c, policy, meta, func(ctx context.Context) (*service.ForwardResult, error) {
		<-ctx.Done()
		return nil, context.Cause(ctx)
	})
	var failoverErr *service.UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	(&GatewayHandler{}).handleResponsesFailoverExhausted(c, failoverErr, false)

	require.Equal(t, http.StatusGatewayTimeout, recorder.Code)
	requireFirstTokenTimeoutOpsEvent(t, c, 42, 2, 1)
}

func requireFirstTokenTimeoutOpsEvent(t *testing.T, c *gin.Context, accountID int64, attemptIndex, switchCount int) {
	t.Helper()
	raw, ok := c.Get(service.OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := raw.([]*service.OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	payload, err := json.Marshal(events[0])
	require.NoError(t, err)
	require.Equal(t, service.UpstreamErrorTypeFirstTokenTimeout, gjson.GetBytes(payload, "kind").String())
	require.Equal(t, service.UpstreamErrorTypeFirstTokenTimeout, gjson.GetBytes(payload, "message").String())
	require.Equal(t, int64(http.StatusGatewayTimeout), gjson.GetBytes(payload, "upstream_status_code").Int())
	require.Equal(t, string(service.ProtocolResponses), gjson.GetBytes(payload, "protocol").String())
	require.Equal(t, service.PlatformOpenAI, gjson.GetBytes(payload, "platform").String())
	require.Equal(t, accountID, gjson.GetBytes(payload, "account_id").Int())
	require.Equal(t, "gpt-5", gjson.GetBytes(payload, "model").String())
	require.Equal(t, int64(5), gjson.GetBytes(payload, "threshold_ms").Int())
	require.Equal(t, int64(attemptIndex), gjson.GetBytes(payload, "attempt").Int())
	require.Equal(t, int64(switchCount), gjson.GetBytes(payload, "switch").Int())
	require.Positive(t, gjson.GetBytes(payload, "elapsed_ms").Int())
	require.False(t, gjson.GetBytes(payload, "upstream_url").Exists())
	require.False(t, gjson.GetBytes(payload, "upstream_response_body").Exists())
	require.False(t, gjson.GetBytes(payload, "detail").Exists())
	require.NotContains(t, string(payload), "credential")
}

func TestFirstTokenTimeoutOpsRecoveredMiddlewarePersistsStableType(t *testing.T) {
	resetFirstTokenTimeoutOpsLogger(t)
	t.Cleanup(func() { resetFirstTokenTimeoutOpsLogger(t) })
	repo := &firstTokenTimeoutOpsRepo{inserted: make(chan *service.OpsInsertErrorLogInput, 1)}
	ops := service.NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	configureFirstTokenTimeoutOpsQueue()
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 5 * time.Millisecond}}

	router := gin.New()
	router.Use(OpsErrorLoggerMiddleware(ops))
	router.POST("/v1/responses", func(c *gin.Context) {
		setOpsRequestContext(c, "gpt-5", true)
		_, err := runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{
			Protocol: service.ProtocolResponses, Platform: service.PlatformOpenAI, AccountID: 42, Model: "gpt-5", AttemptIndex: 1,
		}, func(ctx context.Context) (*service.ForwardResult, error) {
			<-ctx.Done()
			return nil, context.Cause(ctx)
		})
		var failoverErr *service.UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)

		_, err = runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{
			Protocol: service.ProtocolResponses, Platform: service.PlatformOpenAI, AccountID: 43, Model: "gpt-5", AttemptIndex: 2, SwitchCount: 1,
		}, func(ctx context.Context) (*service.ForwardResult, error) {
			require.NoError(t, service.CommitFirstTokenFromContext(ctx))
			return &service.ForwardResult{RequestID: "recovered"}, nil
		})
		require.NoError(t, err)
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	flushFirstTokenTimeoutOpsJob(t)

	select {
	case entry := <-repo.inserted:
		require.Equal(t, service.UpstreamErrorTypeFirstTokenTimeout, entry.ErrorType)
		require.Equal(t, "upstream", entry.ErrorPhase)
		require.NotNil(t, entry.UpstreamErrorsJSON)
		require.Contains(t, *entry.UpstreamErrorsJSON, `"kind":"first_token_timeout"`)
		require.NotContains(t, *entry.UpstreamErrorsJSON, "upstream_url")
		require.NotContains(t, *entry.UpstreamErrorsJSON, "upstream_response_body")
		require.NotContains(t, *entry.UpstreamErrorsJSON, "credential")
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for recovered TTFT ops entry")
	}
}

func TestFirstTokenTimeoutOpsExhaustedMiddlewarePersistsStableType(t *testing.T) {
	resetFirstTokenTimeoutOpsLogger(t)
	t.Cleanup(func() { resetFirstTokenTimeoutOpsLogger(t) })
	repo := &firstTokenTimeoutOpsRepo{inserted: make(chan *service.OpsInsertErrorLogInput, 1)}
	ops := service.NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	configureFirstTokenTimeoutOpsQueue()
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 5 * time.Millisecond}}

	router := gin.New()
	router.Use(OpsErrorLoggerMiddleware(ops))
	router.POST("/v1/responses", func(c *gin.Context) {
		setOpsRequestContext(c, "gpt-5", true)
		_, err := runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{
			Protocol: service.ProtocolResponses, Platform: service.PlatformOpenAI, AccountID: 42, Model: "gpt-5", AttemptIndex: 1,
		}, func(ctx context.Context) (*service.ForwardResult, error) {
			<-ctx.Done()
			return nil, context.Cause(ctx)
		})
		var failoverErr *service.UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)
		(&GatewayHandler{}).handleResponsesFailoverExhausted(c, failoverErr, false)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusGatewayTimeout, recorder.Code)
	flushFirstTokenTimeoutOpsJob(t)

	select {
	case entry := <-repo.inserted:
		require.Equal(t, service.UpstreamErrorTypeFirstTokenTimeout, entry.ErrorType)
		require.Equal(t, "upstream", entry.ErrorPhase)
		require.NotNil(t, entry.UpstreamErrorsJSON)
		require.Contains(t, *entry.UpstreamErrorsJSON, `"kind":"first_token_timeout"`)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for exhausted TTFT ops entry")
	}
}

func TestFirstTokenTimeoutRetrySkipsSameAccountInPoolMode(t *testing.T) {
	fs := NewFailoverState(2, false)
	unscheduler := &mockTempUnscheduler{}
	failoverErr := service.NewFirstTokenTimeoutFailoverError()
	// The stable type is authoritative even if a caller accidentally carries a
	// pool-mode retry flag from a previous upstream classification.
	failoverErr.RetryableOnSameAccount = true

	action := fs.HandleFailoverError(context.Background(), unscheduler, 42, service.PlatformOpenAI, 3, failoverErr)

	require.Equal(t, FailoverContinue, action)
	require.Zero(t, fs.SameAccountRetryCount[42])
	require.Contains(t, fs.FailedAccountIDs, int64(42))
	require.Equal(t, 1, fs.SwitchCount)
	require.Empty(t, unscheduler.calls)
}

func TestFirstTokenTimeoutOpenAILoopsSkipSameAccountRetry(t *testing.T) {
	for _, loop := range []string{"responses", "messages", "chat_completions"} {
		t.Run(loop, func(t *testing.T) {
			failoverErr := service.NewFirstTokenTimeoutFailoverError()
			failoverErr.RetryableOnSameAccount = true
			retryCount := 0
			switchCount := 0
			failedAccountIDs := map[int64]struct{}{}

			if shouldRetryFailoverOnSameAccount(failoverErr) {
				retryCount++
			} else {
				failedAccountIDs[42] = struct{}{}
				switchCount++
			}

			require.Zero(t, retryCount)
			require.Contains(t, failedAccountIDs, int64(42))
			require.Equal(t, 1, switchCount)
		})
	}
}

func TestFirstTokenTimeoutExhaustedEnvelopes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	timeoutErr := service.NewFirstTokenTimeoutFailoverError()

	tests := []struct {
		name string
		path string
		call func(*gin.Context)
	}{
		{name: "gateway responses", path: "/v1/responses", call: func(c *gin.Context) {
			(&GatewayHandler{}).handleResponsesFailoverExhausted(c, timeoutErr, false)
		}},
		{name: "gateway chat completions", path: "/v1/chat/completions", call: func(c *gin.Context) {
			(&GatewayHandler{}).handleCCFailoverExhausted(c, timeoutErr, false)
		}},
		{name: "gateway anthropic messages", path: "/v1/messages", call: func(c *gin.Context) {
			(&GatewayHandler{}).handleFailoverExhausted(c, timeoutErr, service.PlatformAnthropic, false)
		}},
		{name: "openai responses", path: "/openai/v1/responses", call: func(c *gin.Context) {
			(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, timeoutErr, false)
		}},
		{name: "openai chat completions", path: "/openai/v1/chat/completions", call: func(c *gin.Context) {
			(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, timeoutErr, false)
		}},
		{name: "openai anthropic messages", path: "/openai/v1/messages", call: func(c *gin.Context) {
			(&OpenAIGatewayHandler{}).handleAnthropicFailoverExhausted(c, timeoutErr, false)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, nil)

			tt.call(c)

			require.Equal(t, http.StatusGatewayTimeout, recorder.Code)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
			errorPayload, ok := payload["error"].(map[string]any)
			require.True(t, ok)
			gotType, _ := errorPayload["type"].(string)
			if gotType == "" {
				gotType, _ = errorPayload["code"].(string)
			}
			require.Equal(t, service.UpstreamErrorTypeFirstTokenTimeout, gotType)
		})
	}
}
