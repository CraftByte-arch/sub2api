package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
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

type firstTokenRunnerStatsRecorderSpy struct {
	mu     sync.Mutex
	deltas []service.FirstTokenStatsDelta
}

type firstTokenRunnerPanicStringer struct {
	formatted bool
}

func (v *firstTokenRunnerPanicStringer) String() string {
	v.formatted = true
	return "panic value"
}

func (s *firstTokenRunnerStatsRecorderSpy) Record(delta service.FirstTokenStatsDelta) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deltas = append(s.deltas, delta)
}

func (s *firstTokenRunnerStatsRecorderSpy) snapshot() []service.FirstTokenStatsDelta {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]service.FirstTokenStatsDelta(nil), s.deltas...)
}

func bindFirstTokenRunnerRequestTracker(c *gin.Context, recorder service.FirstTokenStatsRecorder, snapshot service.FirstTokenTimeoutSnapshot) *service.FirstTokenRequestTracker {
	tracker := service.NewFirstTokenRequestTracker(recorder, c.Request.Context(), service.ProtocolResponses, "gpt-5", snapshot)
	c.Request = c.Request.WithContext(service.WithFirstTokenRequestTracker(c.Request.Context(), tracker))
	return tracker
}

func TestRunFirstTokenAttemptTracksCommitElapsedAndFinalOutcome(t *testing.T) {
	c, _ := newFirstTokenRunnerContext()
	recorder := &firstTokenRunnerStatsRecorderSpy{}
	snapshot := service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: time.Second}
	tracker := bindFirstTokenRunnerRequestTracker(c, recorder, snapshot)
	policy := firstTokenRunnerPolicyStub{snapshot: snapshot}

	started := time.Now()
	_, err := runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{
		Protocol: service.ProtocolResponses, AccountID: 42, Platform: service.PlatformOpenAI, Model: "gpt-5",
	}, func(ctx context.Context) (*service.ForwardResult, error) {
		time.Sleep(20 * time.Millisecond)
		require.NoError(t, service.CommitFirstTokenFromContext(ctx))
		time.Sleep(100 * time.Millisecond)
		return &service.ForwardResult{}, nil
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, time.Since(started), 100*time.Millisecond)
	tracker.Finish()

	deltas := recorder.snapshot()
	require.Len(t, deltas, 2)
	require.Equal(t, service.FirstTokenStatsScopeAttempt, deltas[0].Scope)
	require.Equal(t, int64(42), deltas[0].AccountID)
	require.Equal(t, service.FirstTokenStatsAttemptSuccess, deltas[0].Outcome)
	require.Equal(t, int64(1), deltas[0].TTFTSampleCount)
	require.Positive(t, deltas[0].TTFTSumMS)
	require.Less(t, deltas[0].TTFTSumMS, int64(80), "TTFT must be captured at semantic commit, not forward completion")
	require.Equal(t, service.FirstTokenStatsRequestSuccess, deltas[1].Outcome)
}

func TestRunFirstTokenAttemptTracksTTFTWhenCommittedPreludeWriteFails(t *testing.T) {
	base, raw := newFirstTokenResponseGateTestWriter()
	raw.writeErr = errors.New("client write failed")
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Writer = base
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	recorder := &firstTokenRunnerStatsRecorderSpy{}
	snapshot := service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: time.Second}
	tracker := bindFirstTokenRunnerRequestTracker(c, recorder, snapshot)

	_, err := runFirstTokenAttempt(c, firstTokenRunnerPolicyStub{snapshot: snapshot}, FirstTokenAttemptMetadata{
		Protocol: service.ProtocolResponses, AccountID: 42, Platform: service.PlatformOpenAI, Model: "gpt-5",
	}, func(ctx context.Context) (*service.ForwardResult, error) {
		_, writeErr := c.Writer.WriteString("data: {\"type\":\"response.created\"}\n\n")
		require.NoError(t, writeErr)
		time.Sleep(20 * time.Millisecond)
		commitErr := service.CommitFirstTokenEventFromContext(ctx, service.ProtocolResponses, "", []byte(`{"type":"response.output_text.delta","delta":"ok"}`))
		return nil, commitErr
	})
	require.ErrorIs(t, err, raw.writeErr)
	tracker.Finish()

	deltas := recorder.snapshot()
	require.Len(t, deltas, 2)
	require.Equal(t, service.FirstTokenStatsAttemptOtherFailure, deltas[0].Outcome)
	require.Equal(t, int64(1), deltas[0].TTFTSampleCount)
	require.GreaterOrEqual(t, deltas[0].TTFTSumMS, int64(15))
	require.Equal(t, deltas[0].TTFTSumMS, deltas[0].TTFTMaxMS)
	require.Equal(t, service.FirstTokenStatsRequestOtherFailure, deltas[1].Outcome)
}

func TestRunFirstTokenAttemptDisabledObservesDirectForwardWithoutAttemptDelta(t *testing.T) {
	c, _ := newFirstTokenRunnerContext()
	recorder := &firstTokenRunnerStatsRecorderSpy{}
	initial := service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second}
	tracker := bindFirstTokenRunnerRequestTracker(c, recorder, initial)
	timedOut := tracker.BeginAttempt(service.FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: service.PlatformOpenAI}, service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 20 * time.Second})
	timedOut.Finish(service.NewFirstTokenTimeoutFailoverError(), service.FirstTokenTimedOut)
	policy := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: false, Timeout: 9 * time.Second}}

	_, err := runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{AccountID: 2, Platform: service.PlatformOpenAI}, func(context.Context) (*service.ForwardResult, error) {
		return &service.ForwardResult{}, nil
	})
	require.NoError(t, err)
	tracker.Finish()

	deltas := recorder.snapshot()
	require.Len(t, deltas, 2)
	require.Equal(t, service.FirstTokenStatsAttemptTTFTTimeout, deltas[0].Outcome)
	require.Equal(t, service.FirstTokenStatsRequestRecoveredAfterTTFT, deltas[1].Outcome)
	require.Equal(t, 9, deltas[1].TimeoutSeconds)
}

func TestRunFirstTokenAttemptPanicStillFinishesTrackedAttempt(t *testing.T) {
	c, response := newFirstTokenRunnerContext()
	recorder := &firstTokenRunnerStatsRecorderSpy{}
	snapshot := service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: time.Second}
	tracker := bindFirstTokenRunnerRequestTracker(c, recorder, snapshot)
	policy := firstTokenRunnerPolicyStub{snapshot: snapshot}
	panicValue := &firstTokenRunnerPanicStringer{}

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{
			Protocol: service.ProtocolResponses, AccountID: 42, Platform: service.PlatformOpenAI, Model: "gpt-5",
		}, func(context.Context) (*service.ForwardResult, error) {
			c.Header("X-Leaked", "panic")
			_, err := c.Writer.WriteString("pending prelude")
			require.NoError(t, err)
			panic(panicValue)
		})
	}()
	require.Same(t, panicValue, recovered)
	require.False(t, panicValue.formatted, "tracking must not inspect or replace the panic value")
	require.Empty(t, response.Header().Get("X-Leaked"))
	require.Empty(t, response.Body.String())
	tracker.Finish()

	deltas := recorder.snapshot()
	require.Len(t, deltas, 2)
	require.Equal(t, service.FirstTokenStatsAttemptOtherFailure, deltas[0].Outcome)
	require.Equal(t, service.FirstTokenStatsFailureOther, deltas[0].FailureKind)
	require.Zero(t, deltas[0].TTFTSampleCount)
	require.Equal(t, service.FirstTokenStatsRequestOtherFailure, deltas[1].Outcome)
}

func TestRunFirstTokenAttemptTrackingFailureOutcomes(t *testing.T) {
	t.Run("timeout then other failure", func(t *testing.T) {
		c, response := newFirstTokenRunnerContext()
		recorder := &firstTokenRunnerStatsRecorderSpy{}
		snapshot := service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 5 * time.Millisecond}
		tracker := bindFirstTokenRunnerRequestTracker(c, recorder, snapshot)
		policy := &firstTokenRunnerPolicyStub{snapshot: snapshot}

		_, err := runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{Protocol: service.ProtocolResponses, AccountID: 1, Platform: service.PlatformOpenAI, Model: "gpt-5"}, func(ctx context.Context) (*service.ForwardResult, error) {
			c.Header("X-Leaked", "slow")
			_, writeErr := c.Writer.WriteString("data: {\"type\":\"response.created\"}\n\n")
			require.NoError(t, writeErr)
			<-ctx.Done()
			return nil, context.Cause(ctx)
		})
		require.Error(t, err)
		require.Empty(t, response.Header().Get("X-Leaked"))
		require.Empty(t, response.Body.String())

		policy.snapshot = service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 17 * time.Second}
		_, err = runFirstTokenAttempt(c, policy, FirstTokenAttemptMetadata{Protocol: service.ProtocolResponses, AccountID: 2, Platform: service.PlatformOpenAI, Model: "gpt-5"}, func(context.Context) (*service.ForwardResult, error) {
			return nil, io.ErrUnexpectedEOF
		})
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		tracker.Finish()

		deltas := recorder.snapshot()
		require.Len(t, deltas, 3)
		require.Equal(t, service.FirstTokenStatsAttemptTTFTTimeout, deltas[0].Outcome)
		require.Equal(t, int64(1), deltas[0].AccountID)
		require.Equal(t, service.FirstTokenStatsAttemptOtherFailure, deltas[1].Outcome)
		require.Equal(t, service.FirstTokenStatsFailureTransport, deltas[1].FailureKind)
		require.Equal(t, int64(2), deltas[1].AccountID)
		require.Equal(t, 17, deltas[1].TimeoutSeconds)
		require.Equal(t, service.FirstTokenStatsRequestOtherFailure, deltas[2].Outcome)
		require.Equal(t, service.FirstTokenStatsFailureTransport, deltas[2].FailureKind)
		require.Equal(t, int64(1), deltas[2].TTFTAffectedCount)
		require.Equal(t, 17, deltas[2].TimeoutSeconds)
	})

	t.Run("timeout exhausted", func(t *testing.T) {
		c, _ := newFirstTokenRunnerContext()
		recorder := &firstTokenRunnerStatsRecorderSpy{}
		snapshot := service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 5 * time.Millisecond}
		tracker := bindFirstTokenRunnerRequestTracker(c, recorder, snapshot)
		_, err := runFirstTokenAttempt(c, firstTokenRunnerPolicyStub{snapshot: snapshot}, FirstTokenAttemptMetadata{Protocol: service.ProtocolResponses, AccountID: 1, Platform: service.PlatformOpenAI, Model: "gpt-5"}, func(ctx context.Context) (*service.ForwardResult, error) {
			<-ctx.Done()
			return nil, context.Cause(ctx)
		})
		require.Error(t, err)
		tracker.Finish()
		deltas := recorder.snapshot()
		require.Len(t, deltas, 2)
		require.Equal(t, service.FirstTokenStatsAttemptTTFTTimeout, deltas[0].Outcome)
		require.Equal(t, service.FirstTokenStatsRequestTTFTExhausted, deltas[1].Outcome)
	})

	t.Run("client cancel", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		c, _ := newFirstTokenRunnerContextWithParent(parent)
		recorder := &firstTokenRunnerStatsRecorderSpy{}
		snapshot := service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: time.Second}
		tracker := bindFirstTokenRunnerRequestTracker(c, recorder, snapshot)
		_, err := runFirstTokenAttempt(c, firstTokenRunnerPolicyStub{snapshot: snapshot}, FirstTokenAttemptMetadata{Protocol: service.ProtocolResponses, AccountID: 1, Platform: service.PlatformOpenAI, Model: "gpt-5"}, func(ctx context.Context) (*service.ForwardResult, error) {
			cancel()
			<-ctx.Done()
			return nil, context.Cause(ctx)
		})
		require.ErrorIs(t, err, context.Canceled)
		tracker.Finish()
		deltas := recorder.snapshot()
		require.Len(t, deltas, 2)
		require.Equal(t, service.FirstTokenStatsAttemptClientCanceled, deltas[0].Outcome)
		require.Equal(t, service.FirstTokenStatsRequestClientCanceled, deltas[1].Outcome)
	})

	t.Run("commit then idle timeout", func(t *testing.T) {
		c, _ := newFirstTokenRunnerContext()
		recorder := &firstTokenRunnerStatsRecorderSpy{}
		snapshot := service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: time.Second}
		tracker := bindFirstTokenRunnerRequestTracker(c, recorder, snapshot)
		_, err := runFirstTokenAttempt(c, firstTokenRunnerPolicyStub{snapshot: snapshot}, FirstTokenAttemptMetadata{Protocol: service.ProtocolResponses, AccountID: 1, Platform: service.PlatformOpenAI, Model: "gpt-5"}, func(ctx context.Context) (*service.ForwardResult, error) {
			require.NoError(t, service.CommitFirstTokenEventFromContext(ctx, service.ProtocolResponses, "", []byte(`{"type":"response.output_text.delta","delta":"ok"}`)))
			return nil, service.ErrStreamDataIntervalTimeout
		})
		require.ErrorIs(t, err, service.ErrStreamDataIntervalTimeout)
		tracker.Finish()
		deltas := recorder.snapshot()
		require.Len(t, deltas, 2)
		require.Equal(t, service.FirstTokenStatsAttemptOtherFailure, deltas[0].Outcome)
		require.Equal(t, service.FirstTokenStatsFailureStreamIdleTimeout, deltas[0].FailureKind)
		require.Equal(t, int64(1), deltas[0].TTFTSampleCount)
		require.Equal(t, service.FirstTokenStatsRequestOtherFailure, deltas[1].Outcome)
	})
}

func TestRunFirstTokenAttemptOnlyDisabledDirectForwardAbandonsTracking(t *testing.T) {
	c, _ := newFirstTokenRunnerContext()
	recorder := &firstTokenRunnerStatsRecorderSpy{}
	tracker := bindFirstTokenRunnerRequestTracker(c, recorder, service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})

	_, err := runFirstTokenAttempt(c, firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: false, Timeout: 7 * time.Second}}, FirstTokenAttemptMetadata{AccountID: 1, Platform: service.PlatformOpenAI}, func(context.Context) (*service.ForwardResult, error) {
		return &service.ForwardResult{}, nil
	})
	require.NoError(t, err)
	tracker.Finish()
	require.Empty(t, recorder.snapshot())
}

func TestRunFirstTokenAttemptDisabledPanicTracksUncontrolledTermination(t *testing.T) {
	t.Run("only uncontrolled panic abandons request", func(t *testing.T) {
		c, _ := newFirstTokenRunnerContext()
		recorder := &firstTokenRunnerStatsRecorderSpy{}
		tracker := bindFirstTokenRunnerRequestTracker(c, recorder, service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
		panicValue := &firstTokenRunnerPanicStringer{}

		var recovered any
		func() {
			defer func() { recovered = recover() }()
			_, _ = runFirstTokenAttempt(c, firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: false, Timeout: 7 * time.Second}}, FirstTokenAttemptMetadata{}, func(context.Context) (*service.ForwardResult, error) {
				panic(panicValue)
			})
		}()
		require.Same(t, panicValue, recovered)
		require.False(t, panicValue.formatted)
		tracker.Finish()
		require.Empty(t, recorder.snapshot())
	})

	t.Run("prior timeout plus uncontrolled panic is affected other failure", func(t *testing.T) {
		c, _ := newFirstTokenRunnerContext()
		recorder := &firstTokenRunnerStatsRecorderSpy{}
		tracker := bindFirstTokenRunnerRequestTracker(c, recorder, service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
		timedOut := tracker.BeginAttempt(service.FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: service.PlatformOpenAI}, service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 20 * time.Second})
		timedOut.Finish(service.NewFirstTokenTimeoutFailoverError(), service.FirstTokenTimedOut)
		panicValue := &firstTokenRunnerPanicStringer{}

		var recovered any
		func() {
			defer func() { recovered = recover() }()
			_, _ = runFirstTokenAttempt(c, firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: false, Timeout: 9 * time.Second}}, FirstTokenAttemptMetadata{}, func(context.Context) (*service.ForwardResult, error) {
				panic(panicValue)
			})
		}()
		require.Same(t, panicValue, recovered)
		require.False(t, panicValue.formatted)
		tracker.Finish()

		deltas := recorder.snapshot()
		require.Len(t, deltas, 2)
		require.Equal(t, service.FirstTokenStatsAttemptTTFTTimeout, deltas[0].Outcome)
		require.Equal(t, service.FirstTokenStatsRequestOtherFailure, deltas[1].Outcome)
		require.Equal(t, service.FirstTokenStatsFailureOther, deltas[1].FailureKind)
		require.Equal(t, int64(1), deltas[1].TTFTAffectedCount)
		require.Equal(t, 9, deltas[1].TimeoutSeconds)
	})
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
	recorder := &firstTokenRunnerStatsRecorderSpy{}

	gateway := NewGatewayHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, policy, recorder)
	openAI := NewOpenAIGatewayHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, policy, recorder)

	require.Same(t, policy, gateway.firstTokenTimeoutPolicy)
	require.Same(t, policy, openAI.firstTokenTimeoutPolicy)
	require.Same(t, recorder, gateway.firstTokenStatsRecorder)
	require.Same(t, recorder, openAI.firstTokenStatsRecorder)
}

func TestBeginFirstTokenRequestTrackingUsesRunnerEligibility(t *testing.T) {
	enabled := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 15 * time.Second}}
	disabled := firstTokenRunnerPolicyStub{snapshot: service.FirstTokenTimeoutSnapshot{Enabled: false, Timeout: 15 * time.Second}}

	tests := []struct {
		name      string
		policy    firstTokenTimeoutPolicySnapshotter
		stream    bool
		model     string
		body      []byte
		websocket bool
	}{
		{name: "disabled policy", policy: disabled, stream: true, model: "gpt-5", body: []byte(`{"stream":true}`)},
		{name: "non stream", policy: enabled, stream: false, model: "gpt-5", body: []byte(`{"stream":false}`)},
		{name: "websocket", policy: enabled, stream: true, model: "gpt-5", body: []byte(`{"stream":true}`), websocket: true},
		{name: "image", policy: enabled, stream: true, model: "gpt-image-1", body: []byte(`{"stream":true}`)},
		{name: "background", policy: enabled, stream: true, model: "gpt-5", body: []byte(`{"stream":true,"background":true}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newFirstTokenRunnerContext()
			if tt.websocket {
				c.Request.Header.Set("Connection", "upgrade")
				c.Request.Header.Set("Upgrade", "websocket")
			}
			recorder := &firstTokenRunnerStatsRecorderSpy{}
			tracker := beginFirstTokenRequestTracking(c, tt.policy, recorder, service.ProtocolResponses, tt.stream, tt.model, tt.body)
			require.Nil(t, tracker)
			require.Nil(t, service.FirstTokenRequestTrackerFromContext(c.Request.Context()))
			require.Empty(t, recorder.snapshot())
		})
	}

	c, _ := newFirstTokenRunnerContext()
	recorder := &firstTokenRunnerStatsRecorderSpy{}
	tracker := beginFirstTokenRequestTracking(c, enabled, recorder, service.ProtocolResponses, true, "gpt-5", []byte(`{"stream":true}`))
	require.NotNil(t, tracker)
	require.Same(t, tracker, service.FirstTokenRequestTrackerFromContext(c.Request.Context()))
	tracker.Finish()
	deltas := recorder.snapshot()
	require.Len(t, deltas, 1)
	require.Equal(t, service.FirstTokenStatsRequestOtherFailure, deltas[0].Outcome)
	require.Equal(t, 15, deltas[0].TimeoutSeconds)
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
