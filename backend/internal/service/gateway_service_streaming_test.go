package service

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type upstreamContextTestKey string

func newStreamingResponseTestGatewayService() *GatewayService {
	return &GatewayService{
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				StreamDataIntervalTimeout: 0,
				MaxLineSize:               defaultMaxLineSize,
			},
		},
		rateLimitService: &RateLimitService{},
	}
}

func TestGatewayService_StreamingReusesScannerBufferAndStillParsesUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStreamingResponseTestGatewayService()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		defer func() { _ = pw.Close() }()
		// Minimal SSE event to trigger parseSSEUsage
		_, _ = pw.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3}}}\n\n"))
		_, _ = pw.Write([]byte("data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n"))
		_, _ = pw.Write([]byte("data: [DONE]\n\n"))
	}()

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.usage)
	require.Equal(t, 3, result.usage.InputTokens)
	require.Equal(t, 7, result.usage.OutputTokens)
}

func TestGatewayService_StreamingKeepaliveUsesIdleTimer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStreamingResponseTestGatewayService()
	svc.cfg.Gateway.StreamKeepaliveInterval = 1

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n"))
		time.Sleep(1100 * time.Millisecond)
		_, _ = pw.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}()

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, rec.Body.String(), "event: ping")
}

func TestGatewayService_StreamingKeepaliveUsesNoopDeltaForAffectedClaudeCodeVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStreamingResponseTestGatewayService()
	svc.cfg.Gateway.StreamKeepaliveInterval = 1

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.198 (external, cli)")

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n"))
		_, _ = pw.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"))
		time.Sleep(1100 * time.Millisecond)
		_, _ = pw.Write([]byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
		_, _ = pw.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}()

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.NoError(t, err)
	require.NotNil(t, result)
	body := rec.Body.String()
	require.Contains(t, body, "event: content_block_delta")
	require.Contains(t, body, `"delta":{"type":"text_delta","text":""}`)
}

func TestGatewayService_StreamingKeepaliveUsesNoopDeltaDuringToolUseForAffectedClaudeCodeVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStreamingResponseTestGatewayService()
	svc.cfg.Gateway.StreamKeepaliveInterval = 1

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.198 (external, cli)")

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n"))
		_, _ = pw.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"Edit\",\"input\":{}}}\n\n"))
		time.Sleep(1100 * time.Millisecond)
		_, _ = pw.Write([]byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n"))
		_, _ = pw.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}()

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.NoError(t, err)
	require.NotNil(t, result)
	body := rec.Body.String()
	require.Contains(t, body, "event: content_block_delta")
	require.Contains(t, body, `"index":1`)
	require.Contains(t, body, `"delta":{"type":"input_json_delta","partial_json":""}`)
}

func TestGatewayService_StreamingKeepaliveKeepsPingForOlderClaudeCodeVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStreamingResponseTestGatewayService()
	svc.cfg.Gateway.StreamKeepaliveInterval = 1

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.187 (external, cli)")

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n"))
		_, _ = pw.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"))
		time.Sleep(1100 * time.Millisecond)
		_, _ = pw.Write([]byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
		_, _ = pw.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}()

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.NoError(t, err)
	require.NotNil(t, result)
	body := rec.Body.String()
	require.Contains(t, body, "event: ping")
	require.NotContains(t, body, `"delta":{"type":"text_delta","text":""}`)
}

func TestDetachUpstreamContextIgnoresClientCancel(t *testing.T) {
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), upstreamContextTestKey("test-key"), "test-value"))
	upstreamCtx, release := detachUpstreamContext(parent)
	defer release()

	cancel()

	require.NoError(t, upstreamCtx.Err())
	require.Equal(t, "test-value", upstreamCtx.Value(upstreamContextTestKey("test-key")))
}

func TestDetachUpstreamContextPreservesControlledAttemptTimeout(t *testing.T) {
	attempt := NewFirstTokenAttempt(context.Background(), time.Millisecond)
	t.Cleanup(attempt.Close)
	ctx := WithFirstTokenAttempt(attempt.Context(), attempt, func() error { return nil })

	upstreamCtx, release := detachUpstreamContext(ctx)
	defer release()

	select {
	case <-upstreamCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("controlled upstream context did not observe first-token timeout")
	}
	require.ErrorIs(t, context.Cause(upstreamCtx), ErrFirstTokenTimeout)
}

func TestDetachStreamUpstreamContextPreservesControlledClientCancel(t *testing.T) {
	parent, cancelParent := context.WithCancelCause(context.Background())
	attempt := NewFirstTokenAttempt(parent, time.Hour)
	t.Cleanup(attempt.Close)
	ctx := WithFirstTokenAttempt(attempt.Context(), attempt, func() error { return nil })

	upstreamCtx, release := detachStreamUpstreamContext(ctx, true)
	defer release()
	clientCause := errors.New("client disconnected")
	cancelParent(clientCause)

	select {
	case <-upstreamCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("controlled stream context did not observe client cancellation")
	}
	require.ErrorIs(t, context.Cause(upstreamCtx), clientCause)
}

func TestDetachStreamUpstreamContextKeepsCommittedRequestForUsageTrailer(t *testing.T) {
	releaseUsage := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n")
		w.(http.Flusher).Flush()
		<-releaseUsage
		_, _ = io.WriteString(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n")
		w.(http.Flusher).Flush()
	}))
	t.Cleanup(server.Close)

	parent, cancelParent := context.WithCancelCause(context.Background())
	attempt := NewFirstTokenAttempt(parent, time.Hour)
	t.Cleanup(attempt.Close)
	ctx := WithFirstTokenAttempt(attempt.Context(), attempt, func() error {
		attempt.MarkFirstToken()
		return nil
	})
	upstreamCtx, release := detachStreamUpstreamContext(ctx, true)
	defer release()
	req, err := http.NewRequestWithContext(upstreamCtx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	_, err = reader.ReadString('\n')
	require.NoError(t, err)
	_, err = reader.ReadString('\n')
	require.NoError(t, err)

	require.NoError(t, CommitFirstTokenFromContext(ctx))
	require.Equal(t, FirstTokenCommitted, attempt.State())
	clientCause := errors.New("client disconnected")
	cancelParent(clientCause)
	require.ErrorIs(t, context.Cause(parent), clientCause)
	close(releaseUsage)

	trailer, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Contains(t, string(trailer), `"output_tokens":7`)
}
