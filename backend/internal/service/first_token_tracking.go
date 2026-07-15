package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var ErrStreamDataIntervalTimeout = errors.New("stream data interval timeout")

type FirstTokenStatsRecorder interface {
	Record(delta FirstTokenStatsDelta)
}

type FirstTokenStatsAttemptMetadata struct {
	AccountID int64
	Platform  string
}

type firstTokenTrackedAttemptResult struct {
	outcome        string
	failureKind    string
	timeoutSeconds int
}

type FirstTokenRequestTracker struct {
	recorder FirstTokenStatsRecorder
	parent   context.Context
	protocol FirstTokenProtocol
	model    string
	now      func() time.Time

	requestTimeoutSeconds int
	finishOnce            sync.Once
	mu                    sync.Mutex
	finishing             bool
	attempts              []*FirstTokenAttemptTracker
}

type FirstTokenAttemptTracker struct {
	request        *FirstTokenRequestTracker
	metadata       FirstTokenStatsAttemptMetadata
	timeoutSeconds int
	finishOnce     sync.Once
	ttftMS         atomic.Int64
	done           chan struct{}
	result         firstTokenTrackedAttemptResult
}

func NewFirstTokenRequestTracker(
	recorder FirstTokenStatsRecorder,
	parent context.Context,
	protocol FirstTokenProtocol,
	model string,
	snapshot FirstTokenTimeoutSnapshot,
) *FirstTokenRequestTracker {
	return newFirstTokenRequestTrackerWithClock(recorder, parent, protocol, model, snapshot, time.Now)
}

func newFirstTokenRequestTrackerWithClock(
	recorder FirstTokenStatsRecorder,
	parent context.Context,
	protocol FirstTokenProtocol,
	model string,
	snapshot FirstTokenTimeoutSnapshot,
	now func() time.Time,
) *FirstTokenRequestTracker {
	if parent == nil {
		parent = context.Background()
	}
	if now == nil {
		now = time.Now
	}
	return &FirstTokenRequestTracker{
		recorder:              recorder,
		parent:                parent,
		protocol:              protocol,
		model:                 model,
		now:                   now,
		requestTimeoutSeconds: firstTokenTimeoutSeconds(snapshot.Timeout),
	}
}

func (t *FirstTokenRequestTracker) BeginAttempt(meta FirstTokenStatsAttemptMetadata, snapshot FirstTokenTimeoutSnapshot) *FirstTokenAttemptTracker {
	if t == nil {
		return nil
	}
	attempt := &FirstTokenAttemptTracker{
		request:        t,
		metadata:       meta,
		timeoutSeconds: firstTokenTimeoutSeconds(snapshot.Timeout),
		done:           make(chan struct{}),
	}
	attempt.ttftMS.Store(-1)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finishing {
		return nil
	}
	t.attempts = append(t.attempts, attempt)
	return attempt
}

func (t *FirstTokenRequestTracker) Finish() {
	if t == nil {
		return
	}
	t.finishOnce.Do(func() {
		t.mu.Lock()
		t.finishing = true
		trackedAttempts := append([]*FirstTokenAttemptTracker(nil), t.attempts...)
		t.mu.Unlock()
		attempts := make([]firstTokenTrackedAttemptResult, 0, len(trackedAttempts))
		for _, attempt := range trackedAttempts {
			<-attempt.done
			attempts = append(attempts, attempt.result)
		}

		outcome := FirstTokenStatsRequestOtherFailure
		failureKind := FirstTokenStatsFailureOther
		timeoutSeconds := t.requestTimeoutSeconds
		affected := int64(0)
		hadTimeout := false
		if len(attempts) > 0 {
			timeoutSeconds = attempts[len(attempts)-1].timeoutSeconds
			for _, attempt := range attempts {
				if attempt.outcome == FirstTokenStatsAttemptTTFTTimeout {
					hadTimeout = true
				}
			}
		}

		switch {
		case t.parent.Err() != nil:
			outcome = FirstTokenStatsRequestClientCanceled
			failureKind = ""
		case hasFirstTokenAttemptOutcome(attempts, FirstTokenStatsAttemptSuccess):
			failureKind = ""
			if hadTimeout {
				outcome = FirstTokenStatsRequestRecoveredAfterTTFT
				affected = 1
			} else {
				outcome = FirstTokenStatsRequestSuccess
			}
		case len(attempts) > 0 && attempts[len(attempts)-1].outcome == FirstTokenStatsAttemptTTFTTimeout:
			outcome = FirstTokenStatsRequestTTFTExhausted
			failureKind = ""
			affected = 1
		case len(attempts) > 0:
			outcome = FirstTokenStatsRequestOtherFailure
			failureKind = attempts[len(attempts)-1].failureKind
			if failureKind == "" {
				failureKind = FirstTokenStatsFailureOther
			}
			if hadTimeout {
				affected = 1
			}
		}

		t.record(FirstTokenStatsDelta{
			BucketStart:       t.now().UTC().Truncate(time.Hour),
			Scope:             FirstTokenStatsScopeRequest,
			AccountID:         0,
			Protocol:          string(t.protocol),
			Platform:          "",
			Model:             t.model,
			TimeoutSeconds:    timeoutSeconds,
			Outcome:           outcome,
			FailureKind:       failureKind,
			SampleCount:       1,
			TTFTAffectedCount: affected,
		})
	})
}

func (t *FirstTokenAttemptTracker) MarkFirstToken(elapsed time.Duration) {
	if t == nil {
		return
	}
	ms := elapsed.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	if ms > math.MaxInt32 {
		ms = math.MaxInt32
	}
	t.ttftMS.CompareAndSwap(-1, ms)
}

func (t *FirstTokenAttemptTracker) Finish(err error, state FirstTokenAttemptState, parent context.Context) {
	if t == nil || t.request == nil {
		return
	}
	t.finishOnce.Do(func() {
		if parent == nil {
			parent = t.request.parent
		}
		outcome, failureKind := firstTokenAttemptStatsOutcome(err, state, parent)
		delta := FirstTokenStatsDelta{
			BucketStart:    t.request.now().UTC().Truncate(time.Hour),
			Scope:          FirstTokenStatsScopeAttempt,
			AccountID:      t.metadata.AccountID,
			Protocol:       string(t.request.protocol),
			Platform:       t.metadata.Platform,
			Model:          t.request.model,
			TimeoutSeconds: t.timeoutSeconds,
			Outcome:        outcome,
			FailureKind:    failureKind,
			SampleCount:    1,
		}
		if ttftMS := t.ttftMS.Load(); outcome != FirstTokenStatsAttemptTTFTTimeout && ttftMS >= 0 {
			delta.TTFTSampleCount = 1
			delta.TTFTSumMS = ttftMS
			delta.TTFTMaxMS = ttftMS
		}
		t.result = firstTokenTrackedAttemptResult{
			outcome:        outcome,
			failureKind:    failureKind,
			timeoutSeconds: t.timeoutSeconds,
		}
		defer close(t.done)
		t.request.record(delta)
	})
}

func (t *FirstTokenRequestTracker) record(delta FirstTokenStatsDelta) {
	if t != nil && t.recorder != nil {
		t.recorder.Record(delta)
	}
}

func firstTokenAttemptStatsOutcome(err error, state FirstTokenAttemptState, parent context.Context) (string, string) {
	if parent != nil && parent.Err() != nil {
		return FirstTokenStatsAttemptClientCanceled, ""
	}
	var failoverErr *UpstreamFailoverError
	if state == FirstTokenTimedOut || errors.Is(err, ErrFirstTokenTimeout) ||
		(errors.As(err, &failoverErr) && failoverErr.ErrorType == UpstreamErrorTypeFirstTokenTimeout) {
		return FirstTokenStatsAttemptTTFTTimeout, ""
	}
	if err == nil && state == FirstTokenCommitted {
		return FirstTokenStatsAttemptSuccess, ""
	}
	return FirstTokenStatsAttemptOtherFailure, classifyFirstTokenFailureKind(err)
}

func classifyFirstTokenFailureKind(err error) string {
	if err == nil {
		return FirstTokenStatsFailureOther
	}

	var failoverErr *UpstreamFailoverError
	if errors.As(err, &failoverErr) {
		errorType := strings.ToLower(strings.TrimSpace(failoverErr.ErrorType))
		if failoverErr.StatusCode == http.StatusTooManyRequests || isFirstTokenRateLimitErrorType(errorType) {
			return FirstTokenStatsFailureRateLimit
		}
		if failoverErr.StatusCode == http.StatusUnauthorized || failoverErr.StatusCode == http.StatusForbidden || isFirstTokenAuthErrorType(errorType) {
			return FirstTokenStatsFailureAuth
		}
		if errorType == UpstreamErrorTypeFirstTokenPreludeOverflow {
			// Prelude overflow is a typed protocol failure despite its generic 502 status.
			return FirstTokenStatsFailureProtocol
		}
		if failoverErr.StatusCode >= 400 && failoverErr.StatusCode < 500 {
			return FirstTokenStatsFailureUpstream4xx
		}
		if failoverErr.StatusCode >= 500 && failoverErr.StatusCode < 600 {
			return FirstTokenStatsFailureUpstream5xx
		}
	}

	if isFirstTokenTransportError(err) {
		return FirstTokenStatsFailureTransport
	}
	if errors.Is(err, ErrStreamDataIntervalTimeout) {
		return FirstTokenStatsFailureStreamIdleTimeout
	}
	if isFirstTokenProtocolError(err, failoverErr) {
		return FirstTokenStatsFailureProtocol
	}
	return FirstTokenStatsFailureOther
}

func isFirstTokenRateLimitErrorType(errorType string) bool {
	switch errorType {
	case "rate_limit", "rate_limit_error", "rate_limit_exceeded", "too_many_requests":
		return true
	default:
		return false
	}
}

func isFirstTokenAuthErrorType(errorType string) bool {
	switch errorType {
	case "auth", "authentication_error", "authorization_error", "permission_error", "invalid_api_key":
		return true
	default:
		return false
	}
}

func isFirstTokenTransportError(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	var tlsErr tls.RecordHeaderError
	if errors.As(err, &tlsErr) {
		return true
	}
	var unknownAuthority x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthority) {
		return true
	}
	var certificateInvalid x509.CertificateInvalidError
	return errors.As(err, &certificateInvalid)
}

func isFirstTokenProtocolError(err error, failoverErr *UpstreamFailoverError) bool {
	if errors.Is(err, ErrFirstTokenPreludeTooLarge) ||
		(failoverErr != nil && failoverErr.ErrorType == UpstreamErrorTypeFirstTokenPreludeOverflow) {
		return true
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return true
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return true
	}
	var sseErr *sseStreamErrorEventError
	if errors.As(err, &sseErr) {
		return true
	}
	switch err.Error() {
	case "invalid json", "stream usage incomplete: missing terminal event", "upstream http bridge stream ended before terminal event":
		return true
	default:
		return false
	}
}

func firstTokenTimeoutSeconds(timeout time.Duration) int {
	seconds := int((timeout + time.Second - 1) / time.Second)
	if seconds < firstTokenTimeoutMinSeconds {
		return firstTokenTimeoutMinSeconds
	}
	if seconds > firstTokenTimeoutMaxSeconds {
		return firstTokenTimeoutMaxSeconds
	}
	return seconds
}

func hasFirstTokenAttemptOutcome(attempts []firstTokenTrackedAttemptResult, outcome string) bool {
	for _, attempt := range attempts {
		if attempt.outcome == outcome {
			return true
		}
	}
	return false
}
