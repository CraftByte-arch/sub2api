package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDetectInterceptType_MaxTokensOneHaikuRequiresClaudeCodeClient(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	notClaudeCode := detectInterceptType(body, "claude-haiku-4-5", 1, false)
	require.Equal(t, InterceptTypeNone, notClaudeCode)

	isClaudeCode := detectInterceptType(body, "claude-haiku-4-5", 1, true)
	require.Equal(t, InterceptTypeMaxTokensOneHaiku, isClaudeCode)
}

func TestDetectInterceptType_SuggestionModeUnaffected(t *testing.T) {
	body := []byte(`{
		"messages":[{
			"role":"user",
			"content":[{"type":"text","text":"[SUGGESTION MODE:foo]"}]
		}],
		"system":[]
	}`)

	got := detectInterceptType(body, "claude-sonnet-4-5", 256, false)
	require.Equal(t, InterceptTypeSuggestionMode, got)
}

func TestSendMockInterceptResponse_MaxTokensOneHaiku(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)

	sendMockInterceptResponse(ctx, "claude-haiku-4-5", InterceptTypeMaxTokensOneHaiku)

	require.Equal(t, http.StatusOK, rec.Code)

	var response map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, "max_tokens", response["stop_reason"])

	id, ok := response["id"].(string)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(id, "msg_bdrk_"))

	content, ok := response["content"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, content)

	firstBlock, ok := content[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "#", firstBlock["text"])

	usage, ok := response["usage"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(1), usage["output_tokens"])
}

func TestSendMockInterceptResponseFirstTokenTracking(t *testing.T) {
	t.Run("without prior attempt records nothing", func(t *testing.T) {
		recorder := &firstTokenRunnerStatsRecorderSpy{}
		tracker := service.NewFirstTokenRequestTracker(recorder, context.Background(), service.ProtocolAnthropicMessages, "claude-sonnet-4-5", service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)

		tracker.ObserveLocalSuccess()
		sendMockInterceptResponse(ctx, "claude-sonnet-4-5", InterceptTypeWarmup)
		tracker.Finish()

		require.Equal(t, http.StatusOK, rec.Code)
		require.Empty(t, recorder.snapshot())
	})

	t.Run("after timeout records recovered request without local attempt", func(t *testing.T) {
		recorder := &firstTokenRunnerStatsRecorderSpy{}
		tracker := service.NewFirstTokenRequestTracker(recorder, context.Background(), service.ProtocolAnthropicMessages, "claude-sonnet-4-5", service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
		attempt := tracker.BeginAttempt(service.FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: service.PlatformAnthropic}, service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 20 * time.Second})
		attempt.Finish(service.NewFirstTokenTimeoutFailoverError(), service.FirstTokenTimedOut)
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)

		tracker.ObserveLocalSuccess()
		sendMockInterceptResponse(ctx, "claude-sonnet-4-5", InterceptTypeWarmup)
		tracker.Finish()

		require.Equal(t, http.StatusOK, rec.Code)
		deltas := recorder.snapshot()
		require.Len(t, deltas, 2)
		require.Equal(t, service.FirstTokenStatsAttemptTTFTTimeout, deltas[0].Outcome)
		require.Equal(t, service.FirstTokenStatsScopeRequest, deltas[1].Scope)
		require.Equal(t, service.FirstTokenStatsRequestRecoveredAfterTTFT, deltas[1].Outcome)
		require.Equal(t, int64(1), deltas[1].TTFTAffectedCount)
		require.Equal(t, 20, deltas[1].TimeoutSeconds)
	})
}
