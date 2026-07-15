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
	"unicode/utf8"
)

var ErrStreamDataIntervalTimeout = errors.New("stream data interval timeout")

const (
	firstTokenStatsModelMaxRunes    = 255
	firstTokenStatsPlatformMaxRunes = 32
)

type FirstTokenStatsRecorder interface {
	// Record must be non-blocking; tracker finalization runs on request goroutines.
	Record(delta FirstTokenStatsDelta)
}

type FirstTokenStatsAttemptMetadata struct {
	AccountID int64
	Platform  string
}

type firstTokenRequestTrackerContextKey struct{}

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
	finalizeOnce          sync.Once
	mu                    sync.Mutex
	finishing             bool
	abandoned             bool
	succeeded             bool
	active                int
	attempts              []*FirstTokenAttemptTracker
	uncontrolled          *firstTokenTrackedAttemptResult
}

type FirstTokenAttemptTracker struct {
	request        *FirstTokenRequestTracker
	metadata       FirstTokenStatsAttemptMetadata
	timeoutSeconds int
	finishOnce     sync.Once
	ttftMS         atomic.Int64
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

func WithFirstTokenRequestTracker(ctx context.Context, tracker *FirstTokenRequestTracker) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if tracker == nil {
		return ctx
	}
	return context.WithValue(ctx, firstTokenRequestTrackerContextKey{}, tracker)
}

func FirstTokenRequestTrackerFromContext(ctx context.Context) *FirstTokenRequestTracker {
	if ctx == nil {
		return nil
	}
	tracker, _ := ctx.Value(firstTokenRequestTrackerContextKey{}).(*FirstTokenRequestTracker)
	return tracker
}

func newFirstTokenRequestTrackerWithClock(
	recorder FirstTokenStatsRecorder,
	parent context.Context,
	protocol FirstTokenProtocol,
	model string,
	snapshot FirstTokenTimeoutSnapshot,
	now func() time.Time,
) *FirstTokenRequestTracker {
	if !isFirstTokenStatsProtocol(protocol) {
		return nil
	}
	if !utf8.ValidString(model) || strings.ContainsRune(model, '\x00') {
		return nil
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	modelRunes := []rune(model)
	if len(modelRunes) > firstTokenStatsModelMaxRunes {
		model = string(modelRunes[:firstTokenStatsModelMaxRunes])
	}
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
	if t == nil || meta.AccountID <= 0 {
		return nil
	}
	if !utf8.ValidString(meta.Platform) || strings.ContainsRune(meta.Platform, '\x00') {
		return nil
	}
	meta.Platform = strings.TrimSpace(meta.Platform)
	if meta.Platform == "" || len([]rune(meta.Platform)) > firstTokenStatsPlatformMaxRunes {
		return nil
	}
	attempt := &FirstTokenAttemptTracker{
		request:        t,
		metadata:       meta,
		timeoutSeconds: firstTokenTimeoutSeconds(snapshot.Timeout),
	}
	attempt.ttftMS.Store(-1)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finishing || t.abandoned || t.succeeded {
		return nil
	}
	t.uncontrolled = nil
	t.attempts = append(t.attempts, attempt)
	t.active++
	return attempt
}

func (t *FirstTokenRequestTracker) Abandon() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finishing || t.active != 0 || len(t.attempts) != 0 || t.uncontrolled != nil {
		return
	}
	t.abandoned = true
}

func (t *FirstTokenRequestTracker) ObserveUncontrolledAttempt(err error, snapshot FirstTokenTimeoutSnapshot) {
	if t == nil {
		return
	}
	result := &firstTokenTrackedAttemptResult{
		outcome:        FirstTokenStatsAttemptSuccess,
		timeoutSeconds: firstTokenTimeoutSeconds(snapshot.Timeout),
	}
	if err != nil {
		result.outcome = FirstTokenStatsAttemptOtherFailure
		result.failureKind = classifyFirstTokenFailureKind(err)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finishing || t.abandoned || t.succeeded {
		return
	}
	t.uncontrolled = result
	if err == nil {
		t.succeeded = true
	}
}

func (t *FirstTokenRequestTracker) Finish() {
	if t == nil {
		return
	}
	t.finishOnce.Do(func() {
		t.mu.Lock()
		t.finishing = true
		shouldFinalize := t.active == 0
		t.mu.Unlock()
		if shouldFinalize {
			t.finalize()
		}
	})
}

func (t *FirstTokenRequestTracker) finalize() {
	if t == nil {
		return
	}
	t.finalizeOnce.Do(func() {
		t.mu.Lock()
		abandoned := t.abandoned
		trackedAttempts := append([]*FirstTokenAttemptTracker(nil), t.attempts...)
		uncontrolled := t.uncontrolled
		attempts := make([]firstTokenTrackedAttemptResult, 0, len(trackedAttempts))
		for _, attempt := range trackedAttempts {
			attempts = append(attempts, attempt.result)
		}
		t.mu.Unlock()
		if abandoned {
			return
		}
		if len(attempts) == 0 && uncontrolled != nil {
			return
		}

		outcome := FirstTokenStatsRequestOtherFailure
		failureKind := FirstTokenStatsFailureOther
		timeoutSeconds := t.requestTimeoutSeconds
		affected := int64(0)
		hadTimeout := false
		if uncontrolled != nil {
			timeoutSeconds = uncontrolled.timeoutSeconds
		} else if len(attempts) > 0 {
			timeoutSeconds = attempts[len(attempts)-1].timeoutSeconds
		}
		if len(attempts) > 0 {
			for _, attempt := range attempts {
				if attempt.outcome == FirstTokenStatsAttemptTTFTTimeout {
					hadTimeout = true
				}
			}
		}

		finalResult := firstTokenTrackedAttemptResult{}
		hasFinalResult := false
		if uncontrolled != nil {
			finalResult = *uncontrolled
			hasFinalResult = true
		} else if len(attempts) > 0 {
			finalResult = attempts[len(attempts)-1]
			hasFinalResult = true
		}

		switch {
		case t.parent.Err() != nil:
			outcome = FirstTokenStatsRequestClientCanceled
			failureKind = ""
		case hasFinalResult && finalResult.outcome == FirstTokenStatsAttemptSuccess:
			failureKind = ""
			if hadTimeout {
				outcome = FirstTokenStatsRequestRecoveredAfterTTFT
				affected = 1
			} else {
				outcome = FirstTokenStatsRequestSuccess
			}
		case hasFinalResult && finalResult.outcome == FirstTokenStatsAttemptTTFTTimeout:
			outcome = FirstTokenStatsRequestTTFTExhausted
			failureKind = ""
			affected = 1
		case hasFinalResult:
			outcome = FirstTokenStatsRequestOtherFailure
			failureKind = finalResult.failureKind
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

func (t *FirstTokenAttemptTracker) Finish(err error, state FirstTokenAttemptState) {
	if t == nil || t.request == nil {
		return
	}
	t.finishOnce.Do(func() {
		outcome, failureKind := firstTokenAttemptStatsOutcome(err, state, t.request.parent)
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
		result := firstTokenTrackedAttemptResult{
			outcome:        outcome,
			failureKind:    failureKind,
			timeoutSeconds: t.timeoutSeconds,
		}
		t.request.record(delta)
		t.request.mu.Lock()
		t.result = result
		if outcome == FirstTokenStatsAttemptSuccess {
			t.request.succeeded = true
		}
		t.request.active--
		shouldFinalize := t.request.finishing && t.request.active == 0
		t.request.mu.Unlock()
		if shouldFinalize {
			t.request.finalize()
		}
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
	var tlsErrPtr *tls.RecordHeaderError
	if errors.As(err, &tlsErrPtr) {
		return true
	}
	var unknownAuthority x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthority) {
		return true
	}
	var unknownAuthorityPtr *x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthorityPtr) {
		return true
	}
	var certificateInvalid x509.CertificateInvalidError
	if errors.As(err, &certificateInvalid) {
		return true
	}
	var certificateInvalidPtr *x509.CertificateInvalidError
	if errors.As(err, &certificateInvalidPtr) {
		return true
	}
	var hostnameError x509.HostnameError
	if errors.As(err, &hostnameError) {
		return true
	}
	var hostnameErrorPtr *x509.HostnameError
	return errors.As(err, &hostnameErrorPtr)
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
	seconds := int64(timeout / time.Second)
	if timeout > 0 && timeout%time.Second != 0 {
		seconds++
	}
	if seconds < int64(firstTokenTimeoutMinSeconds) {
		return firstTokenTimeoutMinSeconds
	}
	if seconds > int64(firstTokenTimeoutMaxSeconds) {
		return firstTokenTimeoutMaxSeconds
	}
	return int(seconds)
}

func isFirstTokenStatsProtocol(protocol FirstTokenProtocol) bool {
	switch protocol {
	case ProtocolResponses, ProtocolChatCompletions, ProtocolAnthropicMessages:
		return true
	default:
		return false
	}
}
