package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

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
