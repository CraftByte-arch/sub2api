package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFirstTokenTimeoutOpenAIResponsesWriterCommitsConvertedText(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
	c, commits := newFirstTokenPipelineContext(t, "/v1/responses")
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1"}}`,
		``,
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1,"output_tokens":1}}}`,
		``,
	}, "\n")))}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1}, time.Now(), "model", "model")
	require.NoError(t, err)
	require.Equal(t, 1, *commits)
}

func TestFirstTokenTimeoutChatWriterSkipsRoleAndCommitsContent(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
	c, commits := newFirstTokenPipelineContext(t, "/v1/chat/completions")
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"model"}}`,
		``,
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_1","model":"model","usage":{"input_tokens":1,"output_tokens":1}}}`,
		``,
	}, "\n")))}

	_, err := svc.handleChatStreamingResponse(resp, c, &Account{ID: 1}, "model", "model", "model", time.Now(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, *commits)
}

func TestFirstTokenTimeoutAnthropicWriterSkipsLifecycleAndCommitsDelta(t *testing.T) {
	svc := newStreamingResponseTestGatewayService()
	c, commits := newFirstTokenPipelineContext(t, "/v1/messages")
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"usage":{"input_tokens":1}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
		``,
	}, "\n")))}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	require.NoError(t, err)
	require.Equal(t, 1, *commits)
}

func newFirstTokenPipelineContext(t *testing.T, path string) (*gin.Context, *int) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	attempt := NewFirstTokenAttempt(context.Background(), time.Hour)
	t.Cleanup(attempt.Close)
	commits := 0
	ctx := WithFirstTokenAttempt(attempt.Context(), attempt, func() error {
		commits++
		return nil
	})
	c.Request = httptest.NewRequest(http.MethodPost, path, nil).WithContext(ctx)
	return c, &commits
}
