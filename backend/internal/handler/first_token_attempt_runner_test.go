package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type firstTokenRunnerPolicyStub struct {
	snapshot service.FirstTokenTimeoutSnapshot
}

func (p firstTokenRunnerPolicyStub) Snapshot() service.FirstTokenTimeoutSnapshot {
	return p.snapshot
}

func TestRunFirstTokenAttemptDisabledPreservesWriterAndContext(t *testing.T) {
	c, recorder := newFirstTokenRunnerContext()
	originalWriter := c.Writer
	originalContext := c.Request.Context()
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: false, Timeout: time.Millisecond}}

	result, err := runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{}, func(ctx context.Context) (*service.ForwardResult, error) {
		require.Same(t, originalWriter, c.Writer)
		require.Equal(t, originalContext, ctx)
		require.Equal(t, originalContext, c.Request.Context())
		c.Header("X-Upstream", "disabled")
		_, writeErr := c.Writer.WriteString("body")
		return &service.ForwardResult{Model: "test"}, writeErr
	})

	require.NoError(t, err)
	require.Equal(t, "test", result.Model)
	require.Same(t, originalWriter, c.Writer)
	require.Equal(t, originalContext, c.Request.Context())
	require.Equal(t, "disabled", recorder.Header().Get("X-Upstream"))
	require.Equal(t, "body", recorder.Body.String())
}

func TestRunFirstTokenAttemptEnabledCommitsAndRestoresWriterAndContext(t *testing.T) {
	c, recorder := newFirstTokenRunnerContext()
	originalWriter := c.Writer
	originalRequest := c.Request
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: time.Second}}

	_, err := runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{Protocol: service.ProtocolResponses}, func(ctx context.Context) (*service.ForwardResult, error) {
		require.NotSame(t, originalWriter, c.Writer)
		require.NotEqual(t, originalRequest.Context(), ctx)
		require.Equal(t, ctx, c.Request.Context())
		c.Header("X-Upstream", "enabled")
		_, writeErr := c.Writer.WriteString("metadata\n")
		require.NoError(t, writeErr)
		require.Empty(t, recorder.Body.String())
		require.NoError(t, service.CommitFirstTokenFromContext(ctx))
		_, writeErr = c.Writer.WriteString("token\n")
		return &service.ForwardResult{}, writeErr
	})

	require.NoError(t, err)
	require.Same(t, originalWriter, c.Writer)
	require.Same(t, originalRequest, c.Request)
	require.Equal(t, "enabled", recorder.Header().Get("X-Upstream"))
	require.Equal(t, "metadata\ntoken\n", recorder.Body.String())
}

func TestRunFirstTokenAttemptTimeoutRollsBackAndReturnsFailover(t *testing.T) {
	c, recorder := newFirstTokenRunnerContext()
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 10 * time.Millisecond}}
	upstreamCanceled := make(chan struct{})

	_, err := runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{}, func(ctx context.Context) (*service.ForwardResult, error) {
		c.Header("X-Leaked", "account-one")
		_, writeErr := c.Writer.WriteString("event: ping\ndata: {}\n\n")
		require.NoError(t, writeErr)
		<-ctx.Done()
		close(upstreamCanceled)
		return nil, context.Cause(ctx)
	})

	select {
	case <-upstreamCanceled:
	default:
		t.Fatal("timeout did not cancel upstream context")
	}
	var failoverErr *service.UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusGatewayTimeout, failoverErr.StatusCode)
	require.False(t, failoverErr.RetryableOnSameAccount)
	require.Empty(t, recorder.Header().Get("X-Leaked"))
	require.Empty(t, recorder.Body.String())
}

func TestRunFirstTokenAttemptTimeoutRestoresResponseCommittedBeforeFallback(t *testing.T) {
	c, recorder := newFirstTokenRunnerContext()
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 5 * time.Millisecond}}

	_, err := runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{Protocol: service.ProtocolResponses}, func(ctx context.Context) (*service.ForwardResult, error) {
		service.MarkResponseCommitted(c)
		_, writeErr := c.Writer.WriteString("gated bytes")
		require.NoError(t, writeErr)
		<-ctx.Done()
		return nil, context.Cause(ctx)
	})
	require.Error(t, err)
	_, markerExists := c.Get(service.ResponseCommittedKey)
	require.False(t, markerExists, "a rolled-back attempt must restore an originally absent marker")
	require.Empty(t, recorder.Body.String())

	ordinaryErr := errors.New("next upstream attempt failed before writing")
	_, err = runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{Protocol: service.ProtocolResponses}, func(context.Context) (*service.ForwardResult, error) {
		return nil, ordinaryErr
	})
	require.ErrorIs(t, err, ordinaryErr)
	require.True(t, (&OpenAIGatewayHandler{}).ensureForwardErrorResponse(c, false))
	require.NotEmpty(t, recorder.Body.String(), "the fallback must not be suppressed as a silent EOF")
}

func TestRunFirstTokenAttemptTimeoutRestoresExistingResponseCommittedValue(t *testing.T) {
	c, _ := newFirstTokenRunnerContext()
	c.Set(service.ResponseCommittedKey, "original")
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 5 * time.Millisecond}}

	_, err := runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{Protocol: service.ProtocolResponses}, func(ctx context.Context) (*service.ForwardResult, error) {
		service.MarkResponseCommitted(c)
		<-ctx.Done()
		return nil, context.Cause(ctx)
	})
	require.Error(t, err)
	marker, markerExists := c.Get(service.ResponseCommittedKey)
	require.True(t, markerExists)
	require.Equal(t, "original", marker)
}

func TestRunFirstTokenAttemptPreludeOverflowRollsBackAndReturnsFailover(t *testing.T) {
	c, recorder := newFirstTokenRunnerContext()
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: time.Second}}

	_, err := runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{}, func(context.Context) (*service.ForwardResult, error) {
		c.Header("X-Leaked", "overflow")
		_, writeErr := c.Writer.Write(make([]byte, firstTokenResponseGatePreludeLimit+1))
		return nil, writeErr
	})

	var failoverErr *service.UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.False(t, failoverErr.RetryableOnSameAccount)
	require.Empty(t, recorder.Header().Get("X-Leaked"))
	require.Empty(t, recorder.Body.String())
}

func TestRunFirstTokenAttemptClientCancelDoesNotBecomeFailover(t *testing.T) {
	parent, cancelParent := context.WithCancelCause(context.Background())
	c, recorder := newFirstTokenRunnerContextWithParent(parent)
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: time.Second}}
	clientCause := errors.New("client disconnected")

	_, err := runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{}, func(ctx context.Context) (*service.ForwardResult, error) {
		_, writeErr := c.Writer.WriteString("event: ping\ndata: {}\n\n")
		require.NoError(t, writeErr)
		cancelParent(clientCause)
		<-ctx.Done()
		return nil, context.Cause(ctx)
	})

	var failoverErr *service.UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.ErrorIs(t, err, clientCause)
	require.Empty(t, recorder.Body.String())
}

func TestRunFirstTokenAttemptPreservesExistingFailoverAndDiscardsPrelude(t *testing.T) {
	c, recorder := newFirstTokenRunnerContext()
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: time.Second}}
	winner := &service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable, RetryableOnSameAccount: true}

	_, err := runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{}, func(context.Context) (*service.ForwardResult, error) {
		c.Header("X-Leaked", "failover")
		_, writeErr := c.Writer.WriteString("metadata")
		require.NoError(t, writeErr)
		return nil, winner
	})

	require.ErrorIs(t, err, winner)
	require.Empty(t, recorder.Header().Get("X-Leaked"))
	require.Empty(t, recorder.Body.String())
}

func TestRunFirstTokenAttemptCommitsTerminalNonFailoverResponse(t *testing.T) {
	c, recorder := newFirstTokenRunnerContext()
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: time.Second}}
	winner := errors.New("terminal upstream error")

	_, err := runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{}, func(context.Context) (*service.ForwardResult, error) {
		c.Writer.WriteHeader(http.StatusBadRequest)
		_, writeErr := c.Writer.WriteString(`{"error":"rejected"}`)
		require.NoError(t, writeErr)
		return nil, winner
	})

	require.ErrorIs(t, err, winner)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.JSONEq(t, `{"error":"rejected"}`, recorder.Body.String())
}

func TestRunFirstTokenAttemptSuccessfulNonSemanticStreamFailsOverWithoutLeaking(t *testing.T) {
	c, recorder := newFirstTokenRunnerContext()
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: time.Second}}

	_, err := runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{Protocol: service.ProtocolAnthropicMessages}, func(context.Context) (*service.ForwardResult, error) {
		c.Header("X-Incompatible-Upstream", "gemini-native")
		_, writeErr := c.Writer.WriteString("data: {\"candidates\":[]}\n\n")
		return &service.ForwardResult{}, writeErr
	})

	var failoverErr *service.UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.False(t, failoverErr.RetryableOnSameAccount)
	require.Empty(t, recorder.Header().Get("X-Incompatible-Upstream"))
	require.Empty(t, recorder.Body.String())
}

func TestFirstTokenAttemptEligibleOnlyForTargetHTTPTextStreams(t *testing.T) {
	tests := []struct {
		name      string
		protocol  service.FirstTokenProtocol
		stream    bool
		model     string
		body      string
		websocket bool
		want      bool
	}{
		{name: "responses text stream", protocol: service.ProtocolResponses, stream: true, model: "gpt-5", body: `{"stream":true}`, want: true},
		{name: "chat text stream", protocol: service.ProtocolChatCompletions, stream: true, model: "gpt-5", body: `{"stream":true}`, want: true},
		{name: "messages text stream", protocol: service.ProtocolAnthropicMessages, stream: true, model: "claude", body: `{"stream":true}`, want: true},
		{name: "non stream", protocol: service.ProtocolResponses, model: "gpt-5", body: `{"stream":false}`},
		{name: "websocket", protocol: service.ProtocolResponses, stream: true, model: "gpt-5", body: `{"stream":true}`, websocket: true},
		{name: "image model", protocol: service.ProtocolResponses, stream: true, model: "gpt-image-1", body: `{"stream":true}`},
		{name: "image tool", protocol: service.ProtocolResponses, stream: true, model: "gpt-5", body: `{"stream":true,"tools":[{"type":"image_generation"}]}`},
		{name: "background", protocol: service.ProtocolResponses, stream: true, model: "gpt-5", body: `{"stream":true,"background":true}`},
		{name: "unknown protocol", protocol: service.FirstTokenProtocol("unknown"), stream: true, model: "gpt-5", body: `{"stream":true}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newFirstTokenRunnerContext()
			if tt.websocket {
				c.Request.Header.Set("Connection", "Upgrade")
				c.Request.Header.Set("Upgrade", "websocket")
			}
			require.Equal(t, tt.want, firstTokenAttemptEligible(c, tt.protocol, tt.stream, tt.model, []byte(tt.body)))
		})
	}
}

func TestRunEligibleFirstTokenAttemptInstallsGateOnlyForEligibleRequest(t *testing.T) {
	t.Run("eligible", func(t *testing.T) {
		c, _ := newFirstTokenRunnerContext()
		originalWriter := c.Writer
		policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: time.Second}}

		_, err := runEligibleFirstTokenAttempt(c, policy, service.ProtocolResponses, true, "gpt-5", []byte(`{"stream":true}`), FirstTokenAttemptMetadata{}, func(ctx context.Context) (*service.ForwardResult, error) {
			require.NotSame(t, originalWriter, c.Writer)
			_, writeErr := c.Writer.WriteString("data: metadata\n\n")
			require.NoError(t, writeErr)
			require.NoError(t, service.CommitFirstTokenFromContext(ctx))
			return &service.ForwardResult{}, nil
		})

		require.NoError(t, err)
	})

	t.Run("image intent", func(t *testing.T) {
		c, _ := newFirstTokenRunnerContext()
		originalWriter := c.Writer
		originalContext := c.Request.Context()
		policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: time.Second}}

		_, err := runEligibleFirstTokenAttempt(c, policy, service.ProtocolResponses, true, "gpt-image-1", []byte(`{"stream":true}`), FirstTokenAttemptMetadata{}, func(ctx context.Context) (*service.ForwardResult, error) {
			require.Same(t, originalWriter, c.Writer)
			require.Equal(t, originalContext, ctx)
			return &service.ForwardResult{}, nil
		})

		require.NoError(t, err)
	})
}

func TestRunEligibleFirstTokenAttemptFromContextPreservesExcludedImageIntent(t *testing.T) {
	c, _ := newFirstTokenRunnerContext()
	requestCtx := service.WithOpenAIImageGenerationIntent(c.Request.Context())
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: time.Second}}

	_, err := runEligibleFirstTokenAttemptFromContext(
		c,
		requestCtx,
		policy,
		service.ProtocolResponses,
		true,
		"gpt-image-1",
		[]byte(`{"stream":true}`),
		FirstTokenAttemptMetadata{},
		func(ctx context.Context) (*service.ForwardResult, error) {
			require.True(t, service.OpenAIImageGenerationIntentFromContext(ctx))
			return &service.ForwardResult{}, nil
		},
	)

	require.NoError(t, err)
}

func TestGatewayHandlersReceiveFirstTokenTimeoutPolicy(t *testing.T) {
	policy := service.NewFirstTokenTimeoutPolicy(nil, nil)

	gateway := NewGatewayHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, policy)
	openAI := NewOpenAIGatewayHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, policy)

	require.Same(t, policy, gateway.firstTokenTimeoutPolicy)
	require.Same(t, policy, openAI.firstTokenTimeoutPolicy)
}

func newFirstTokenRunnerContext() (*gin.Context, *httptest.ResponseRecorder) {
	return newFirstTokenRunnerContextWithParent(context.Background())
}

func newFirstTokenRunnerContextWithParent(parent context.Context) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(parent)
	return c, recorder
}
