package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

type FirstTokenAttemptMetadata struct {
	Protocol     service.FirstTokenProtocol
	AccountID    int64
	Platform     string
	Model        string
	AttemptIndex int
	SwitchCount  int
}

const firstTokenTimeoutClientMessage = "Upstream timed out before first token"

type firstTokenTimeoutPolicySnapshotter interface {
	Snapshot() service.FirstTokenTimeoutSnapshot
}

type firstTokenAttemptTerminalController interface {
	State() service.FirstTokenAttemptState
	Cause() error
	Elapsed() time.Duration
}

type firstTokenTerminalCommitter interface {
	CommitTerminal() error
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

func openAIResponsesFirstTokenStream(c *gin.Context, stream bool) bool {
	return stream || service.OpenAICompactClientWantsStream(c)
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
	return result, finishFirstTokenAttempt(c, snapshot, meta, attempt, originalRequest.Context(), gate, forwardErr)
}

func finishFirstTokenAttempt(
	c *gin.Context,
	snapshot service.FirstTokenTimeoutSnapshot,
	meta FirstTokenAttemptMetadata,
	attempt firstTokenAttemptTerminalController,
	parent context.Context,
	gate firstTokenTerminalCommitter,
	forwardErr error,
) error {
	finalErr := firstTokenAttemptTerminalFailover(attempt, parent)
	if finalErr == nil {
		var existingFailover *service.UpstreamFailoverError
		if errors.As(forwardErr, &existingFailover) {
			finalErr = forwardErr
		} else if attempt.State() == service.FirstTokenPending {
			if forwardErr == nil {
				finalErr = &service.UpstreamFailoverError{
					StatusCode:             http.StatusBadGateway,
					RetryableOnSameAccount: false,
				}
			} else if commitErr := gate.CommitTerminal(); commitErr != nil {
				if terminalErr := firstTokenAttemptTerminalFailover(attempt, parent); terminalErr != nil {
					finalErr = terminalErr
				} else {
					finalErr = commitErr
				}
			} else {
				finalErr = forwardErr
			}
		} else {
			finalErr = forwardErr
		}
	}

	var typedFailover *service.UpstreamFailoverError
	if errors.As(finalErr, &typedFailover) && typedFailover.ErrorType == service.UpstreamErrorTypeFirstTokenTimeout {
		elapsed := attempt.Elapsed()
		logger.FromContext(parent).Warn("gateway.first_token_timeout",
			zap.String("protocol", string(meta.Protocol)),
			zap.String("platform", meta.Platform),
			zap.Int64("account", meta.AccountID),
			zap.String("model", meta.Model),
			zap.Duration("threshold", snapshot.Timeout),
			zap.Int("attempt", meta.AttemptIndex),
			zap.Int("switch", meta.SwitchCount),
			zap.Duration("elapsed", elapsed),
		)
		service.RecordFirstTokenTimeoutOpsEvent(c, service.FirstTokenTimeoutOpsEvent{
			Protocol:     meta.Protocol,
			Platform:     meta.Platform,
			AccountID:    meta.AccountID,
			Model:        meta.Model,
			Threshold:    snapshot.Timeout,
			AttemptIndex: meta.AttemptIndex,
			SwitchCount:  meta.SwitchCount,
			Elapsed:      elapsed,
		})
	}
	return finalErr
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

func firstTokenAttemptTerminalFailover(attempt firstTokenAttemptTerminalController, parent context.Context) error {
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
		return service.NewFirstTokenTimeoutFailoverError()
	case errors.Is(attempt.Cause(), service.ErrFirstTokenPreludeTooLarge):
		return service.NewFirstTokenPreludeOverflowFailoverError()
	default:
		return nil
	}
}
