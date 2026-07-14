package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type FirstTokenAttemptMetadata struct {
	Protocol     service.FirstTokenProtocol
	AccountID    int64
	Platform     string
	Model        string
	AttemptIndex int
	SwitchCount  int
}

type firstTokenTimeoutPolicySnapshotter interface {
	Snapshot() service.FirstTokenTimeoutSnapshot
}

func firstTokenAttemptEligible(c *gin.Context, protocol service.FirstTokenProtocol, stream bool, model string, body []byte) bool {
	if !stream || !firstTokenResponseGateEligible(c) {
		return false
	}
	switch protocol {
	case service.ProtocolResponses, service.ProtocolChatCompletions, service.ProtocolAnthropicMessages:
	default:
		return false
	}
	if service.IsImageGenerationIntent("", model, body) {
		return false
	}
	return !gjson.GetBytes(body, "background").Bool()
}

func runEligibleFirstTokenAttempt[T any](
	c *gin.Context,
	policy firstTokenTimeoutPolicySnapshotter,
	protocol service.FirstTokenProtocol,
	stream bool,
	model string,
	body []byte,
	meta FirstTokenAttemptMetadata,
	forward func(context.Context) (T, error),
) (T, error) {
	return runEligibleFirstTokenAttemptFromContext(c, c.Request.Context(), policy, protocol, stream, model, body, meta, forward)
}

func runEligibleFirstTokenAttemptFromContext[T any](
	c *gin.Context,
	requestCtx context.Context,
	policy firstTokenTimeoutPolicySnapshotter,
	protocol service.FirstTokenProtocol,
	stream bool,
	model string,
	body []byte,
	meta FirstTokenAttemptMetadata,
	forward func(context.Context) (T, error),
) (T, error) {
	if !firstTokenAttemptEligible(c, protocol, stream, model, body) {
		return forward(requestCtx)
	}
	meta.Protocol = protocol
	return runFirstTokenAttempt(c, policy, meta, forward)
}

func runFirstTokenAttempt[T any](
	c *gin.Context,
	policy firstTokenTimeoutPolicySnapshotter,
	meta FirstTokenAttemptMetadata,
	forward func(context.Context) (T, error),
) (T, error) {
	_ = meta
	if c == nil || c.Request == nil || c.Writer == nil || firstTokenPolicyDisabled(policy) {
		var ctx context.Context
		if c != nil && c.Request != nil {
			ctx = c.Request.Context()
		}
		return forward(ctx)
	}

	snapshot := policy.Snapshot()
	if !snapshot.Enabled {
		return forward(c.Request.Context())
	}

	originalWriter := c.Writer
	originalRequest := c.Request
	attempt := service.NewFirstTokenAttempt(originalRequest.Context(), snapshot.Timeout)
	gate := NewFirstTokenResponseGate(originalWriter, attempt)
	attemptCtx := service.WithFirstTokenAttempt(attempt.Context(), attempt, gate.Commit)
	c.Writer = gate
	c.Request = originalRequest.WithContext(attemptCtx)

	defer func() {
		c.Writer = originalWriter
		c.Request = originalRequest
	}()
	defer attempt.Close()
	defer gate.Rollback()

	result, forwardErr := forward(attemptCtx)
	if failoverErr := firstTokenAttemptTerminalFailover(attempt, originalRequest.Context()); failoverErr != nil {
		return result, failoverErr
	}

	var existingFailover *service.UpstreamFailoverError
	if errors.As(forwardErr, &existingFailover) {
		return result, forwardErr
	}

	if attempt.State() == service.FirstTokenPending {
		if forwardErr == nil {
			return result, &service.UpstreamFailoverError{
				StatusCode:             http.StatusBadGateway,
				RetryableOnSameAccount: false,
			}
		}
		if commitErr := gate.CommitTerminal(); commitErr != nil {
			if failoverErr := firstTokenAttemptTerminalFailover(attempt, originalRequest.Context()); failoverErr != nil {
				return result, failoverErr
			}
			return result, commitErr
		}
	}
	return result, forwardErr
}

func firstTokenPolicyDisabled(policy firstTokenTimeoutPolicySnapshotter) bool {
	if policy == nil {
		return true
	}
	if concrete, ok := policy.(*service.FirstTokenTimeoutPolicy); ok && concrete == nil {
		return true
	}
	return false
}

func firstTokenAttemptTerminalFailover(attempt *service.FirstTokenAttempt, parent context.Context) error {
	if attempt == nil {
		return nil
	}
	if parent != nil && parent.Err() != nil {
		if cause := context.Cause(parent); cause != nil {
			return cause
		}
		return parent.Err()
	}

	switch {
	case errors.Is(attempt.Cause(), service.ErrFirstTokenTimeout):
		return &service.UpstreamFailoverError{
			StatusCode:             http.StatusGatewayTimeout,
			RetryableOnSameAccount: false,
		}
	case errors.Is(attempt.Cause(), service.ErrFirstTokenPreludeTooLarge):
		return &service.UpstreamFailoverError{
			StatusCode:             http.StatusBadGateway,
			RetryableOnSameAccount: false,
		}
	default:
		return nil
	}
}
