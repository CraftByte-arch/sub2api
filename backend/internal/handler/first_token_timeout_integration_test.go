package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestFirstTokenTimeoutFailoverAcrossInboundHTTPProtocols(t *testing.T) {
	tests := []struct {
		name          string
		protocol      service.FirstTokenProtocol
		metadataEvent string
		metadata      string
		semanticEvent string
		semantic      string
	}{
		{
			name:     "responses",
			protocol: service.ProtocolResponses,
			metadata: `{"type":"response.created","response":{"id":"resp_1"}}`,
			semantic: `{"type":"response.output_text.delta","delta":"hello"}`,
		},
		{
			name:     "chat_completions",
			protocol: service.ProtocolChatCompletions,
			metadata: `{"choices":[{"delta":{"role":"assistant"}}]}`,
			semantic: `{"choices":[{"delta":{"content":"hello"}}]}`,
		},
		{
			name:          "anthropic_messages",
			protocol:      service.ProtocolAnthropicMessages,
			metadataEvent: "ping",
			metadata:      `{"type":"ping"}`,
			semanticEvent: "content_block_delta",
			semantic:      `{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder := newFirstTokenRunnerContext()
			policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 15 * time.Millisecond}}
			attempts := 0
			firstCanceled := false

			for accountID := int64(1); accountID <= 2; accountID++ {
				attempts++
				_, err := runEligibleFirstTokenAttempt(
					c,
					policy,
					tt.protocol,
					true,
					"model",
					[]byte(`{"stream":true}`),
					FirstTokenAttemptMetadata{AccountID: accountID, AttemptIndex: attempts},
					func(ctx context.Context) (*service.ForwardResult, error) {
						c.Header("X-Upstream-Account", fmt.Sprintf("%d", accountID))
						_, writeErr := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", tt.metadataEvent, tt.metadata)
						if writeErr != nil {
							return nil, writeErr
						}
						if accountID == 1 {
							<-ctx.Done()
							firstCanceled = true
							return nil, context.Cause(ctx)
						}
						require.Empty(t, recorder.Body.String())
						if err := service.CommitFirstTokenEventFromContext(ctx, tt.protocol, tt.semanticEvent, []byte(tt.semantic)); err != nil {
							return nil, err
						}
						_, writeErr = fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", tt.semanticEvent, tt.semantic)
						return &service.ForwardResult{Stream: true}, writeErr
					},
				)
				if err == nil {
					break
				}
				var failoverErr *service.UpstreamFailoverError
				require.ErrorAs(t, err, &failoverErr)
				require.Equal(t, http.StatusGatewayTimeout, failoverErr.StatusCode)
			}

			require.True(t, firstCanceled)
			require.Equal(t, 2, attempts)
			require.Equal(t, "2", recorder.Header().Get("X-Upstream-Account"))
			require.Equal(t, 1, strings.Count(recorder.Body.String(), tt.metadata))
			require.Contains(t, recorder.Body.String(), tt.metadata)
			require.Contains(t, recorder.Body.String(), tt.semantic)
		})
	}
}

func TestFirstTokenTimeoutGatesCompactKeepalivePerAttempt(t *testing.T) {
	c, recorder := newFirstTokenRunnerContext()
	service.MarkOpenAICompactClientStream(c)
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 20 * time.Millisecond}}

	_, err := runEligibleFirstTokenAttempt(
		c, policy, service.ProtocolResponses, true, "model", []byte(`{"stream":true}`),
		FirstTokenAttemptMetadata{AccountID: 1},
		func(ctx context.Context) (*service.ForwardResult, error) {
			stopKeepalive := service.StartOpenAICompactSSEKeepalive(c, time.Millisecond)
			defer stopKeepalive()
			<-ctx.Done()
			return nil, context.Cause(ctx)
		},
	)

	var failoverErr *service.UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusGatewayTimeout, failoverErr.StatusCode)
	require.Empty(t, recorder.Header())
	require.Empty(t, recorder.Body.String())
}

func TestFirstTokenTimeoutClientCancelDoesNotSelectAnotherAccount(t *testing.T) {
	parent, cancelParent := context.WithCancelCause(context.Background())
	c, recorder := newFirstTokenRunnerContextWithParent(parent)
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: time.Second}}
	attempts := 0
	clientCause := errors.New("client disconnected")

	for accountID := int64(1); accountID <= 2; accountID++ {
		attempts++
		_, err := runEligibleFirstTokenAttempt(
			c, policy, service.ProtocolResponses, true, "model", []byte(`{"stream":true}`),
			FirstTokenAttemptMetadata{AccountID: accountID},
			func(ctx context.Context) (*service.ForwardResult, error) {
				_, writeErr := c.Writer.WriteString("data: metadata\n\n")
				require.NoError(t, writeErr)
				cancelParent(clientCause)
				<-ctx.Done()
				return nil, context.Cause(ctx)
			},
		)
		require.ErrorIs(t, err, clientCause)
		var failoverErr *service.UpstreamFailoverError
		require.False(t, errors.As(err, &failoverErr))
		if parent.Err() != nil {
			break
		}
	}

	require.Equal(t, 1, attempts)
	require.Empty(t, recorder.Body.String())
}

func TestFirstTokenTimeoutStopsAfterSemanticCommit(t *testing.T) {
	c, recorder := newFirstTokenRunnerContext()
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 10 * time.Millisecond}}

	_, err := runEligibleFirstTokenAttempt(
		c, policy, service.ProtocolAnthropicMessages, true, "model", []byte(`{"stream":true}`),
		FirstTokenAttemptMetadata{AccountID: 1},
		func(ctx context.Context) (*service.ForwardResult, error) {
			semantic := []byte(`{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"working"}}`)
			require.NoError(t, service.CommitFirstTokenEventFromContext(ctx, service.ProtocolAnthropicMessages, "content_block_delta", semantic))
			_, writeErr := fmt.Fprintf(c.Writer, "event: content_block_delta\ndata: %s\n\n", semantic)
			require.NoError(t, writeErr)
			time.Sleep(30 * time.Millisecond)
			_, writeErr = c.Writer.WriteString("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			return &service.ForwardResult{Stream: true}, writeErr
		},
	)

	require.NoError(t, err)
	require.Contains(t, recorder.Body.String(), "thinking_delta")
	require.Contains(t, recorder.Body.String(), "message_stop")
}
