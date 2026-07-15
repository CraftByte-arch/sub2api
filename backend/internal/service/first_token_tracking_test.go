package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type firstTokenStatsRecorderSpy struct {
	mu     sync.Mutex
	deltas []FirstTokenStatsDelta
}

func (s *firstTokenStatsRecorderSpy) Record(delta FirstTokenStatsDelta) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deltas = append(s.deltas, delta)
}

func (s *firstTokenStatsRecorderSpy) snapshot(t *testing.T) []FirstTokenStatsDelta {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	deltas := append([]FirstTokenStatsDelta(nil), s.deltas...)
	requireFirstTokenStatsDeltasValid(t, deltas)
	return deltas
}

type blockingFirstTokenStatsRecorder struct {
	firstTokenStatsRecorderSpy
	attemptRecordStarted chan struct{}
	releaseAttemptRecord chan struct{}
	attemptOnce          sync.Once
}

// This test recorder intentionally violates the production non-blocking contract.
func newBlockingFirstTokenStatsRecorder() *blockingFirstTokenStatsRecorder {
	return &blockingFirstTokenStatsRecorder{
		attemptRecordStarted: make(chan struct{}),
		releaseAttemptRecord: make(chan struct{}),
	}
}

func (s *blockingFirstTokenStatsRecorder) Record(delta FirstTokenStatsDelta) {
	if delta.Scope == FirstTokenStatsScopeAttempt {
		s.attemptOnce.Do(func() { close(s.attemptRecordStarted) })
		<-s.releaseAttemptRecord
	}
	s.firstTokenStatsRecorderSpy.Record(delta)
}

func requireFirstTokenStatsDeltasValid(t *testing.T, deltas []FirstTokenStatsDelta) {
	t.Helper()
	validFailureKinds := []string{
		FirstTokenStatsFailureRateLimit,
		FirstTokenStatsFailureAuth,
		FirstTokenStatsFailureUpstream4xx,
		FirstTokenStatsFailureUpstream5xx,
		FirstTokenStatsFailureTransport,
		FirstTokenStatsFailureStreamIdleTimeout,
		FirstTokenStatsFailureProtocol,
		FirstTokenStatsFailureOther,
	}
	for _, delta := range deltas {
		require.Equal(t, int64(1), delta.SampleCount)
		require.Contains(t, []string{string(ProtocolResponses), string(ProtocolChatCompletions), string(ProtocolAnthropicMessages)}, delta.Protocol)
		require.NotEmpty(t, delta.Model)
		require.LessOrEqual(t, len([]rune(delta.Model)), 255)
		require.GreaterOrEqual(t, delta.TimeoutSeconds, firstTokenTimeoutMinSeconds)
		require.LessOrEqual(t, delta.TimeoutSeconds, firstTokenTimeoutMaxSeconds)
		require.GreaterOrEqual(t, delta.TTFTSampleCount, int64(0))
		require.LessOrEqual(t, delta.TTFTSampleCount, delta.SampleCount)
		require.GreaterOrEqual(t, delta.TTFTSumMS, int64(0))
		require.GreaterOrEqual(t, delta.TTFTMaxMS, int64(0))
		require.LessOrEqual(t, delta.TTFTMaxMS, delta.TTFTSumMS)
		require.GreaterOrEqual(t, delta.TTFTAffectedCount, int64(0))
		require.LessOrEqual(t, delta.TTFTAffectedCount, delta.SampleCount)

		if delta.TTFTSampleCount == 0 {
			require.Zero(t, delta.TTFTSumMS)
			require.Zero(t, delta.TTFTMaxMS)
		}
		if delta.Scope == FirstTokenStatsScopeRequest {
			require.Zero(t, delta.AccountID)
			require.Empty(t, delta.Platform)
			require.Zero(t, delta.TTFTSampleCount)
			require.Zero(t, delta.TTFTSumMS)
			require.Zero(t, delta.TTFTMaxMS)
		} else {
			require.Equal(t, FirstTokenStatsScopeAttempt, delta.Scope)
			require.Positive(t, delta.AccountID)
			require.NotEmpty(t, delta.Platform)
			require.LessOrEqual(t, len([]rune(delta.Platform)), 32)
			require.Zero(t, delta.TTFTAffectedCount)
		}
		if delta.Outcome == FirstTokenStatsAttemptTTFTTimeout {
			require.Zero(t, delta.TTFTSampleCount)
		}
		if delta.Outcome == FirstTokenStatsAttemptOtherFailure || delta.Outcome == FirstTokenStatsRequestOtherFailure {
			require.Contains(t, validFailureKinds, delta.FailureKind)
		} else {
			require.Empty(t, delta.FailureKind)
		}
	}
}

func TestFirstTokenTrackingRecordsAttemptAndRequestExactlyOnce(t *testing.T) {
	recorder := &firstTokenStatsRecorderSpy{}
	now := time.Date(2026, 7, 15, 0, 34, 0, 0, time.FixedZone("CST", 8*60*60))
	request := newFirstTokenRequestTrackerWithClock(recorder, context.Background(), ProtocolResponses, "gpt-5", FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second}, func() time.Time { return now })
	attempt := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 17, Platform: PlatformOpenAI}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 20 * time.Second})

	attempt.MarkFirstToken(1250 * time.Millisecond)
	attempt.MarkFirstToken(9 * time.Second)
	attempt.Finish(nil, FirstTokenCommitted)
	attempt.Finish(errors.New("duplicate"), FirstTokenCommitted)
	request.Finish()
	request.Finish()

	deltas := recorder.snapshot(t)
	require.Len(t, deltas, 2)
	require.Equal(t, FirstTokenStatsDelta{
		BucketStart:     now.UTC().Truncate(time.Hour),
		Scope:           FirstTokenStatsScopeAttempt,
		AccountID:       17,
		Protocol:        string(ProtocolResponses),
		Platform:        PlatformOpenAI,
		Model:           "gpt-5",
		TimeoutSeconds:  20,
		Outcome:         FirstTokenStatsAttemptSuccess,
		SampleCount:     1,
		TTFTSampleCount: 1,
		TTFTSumMS:       1250,
		TTFTMaxMS:       1250,
	}, deltas[0])
	require.Equal(t, FirstTokenStatsScopeRequest, deltas[1].Scope)
	require.Equal(t, FirstTokenStatsRequestSuccess, deltas[1].Outcome)
	require.Equal(t, 20, deltas[1].TimeoutSeconds)
}

func TestFirstTokenTrackingDerivesRecoveredAfterTimeout(t *testing.T) {
	recorder := &firstTokenStatsRecorderSpy{}
	request := NewFirstTokenRequestTracker(recorder, context.Background(), ProtocolChatCompletions, "gpt-5", FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	first := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: PlatformOpenAI}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	first.Finish(NewFirstTokenTimeoutFailoverError(), FirstTokenTimedOut)
	second := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 2, Platform: PlatformOpenAI}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 20 * time.Second})
	second.MarkFirstToken(240 * time.Millisecond)
	second.Finish(nil, FirstTokenCommitted)
	request.Finish()

	deltas := recorder.snapshot(t)
	require.Len(t, deltas, 3)
	require.Equal(t, FirstTokenStatsAttemptTTFTTimeout, deltas[0].Outcome)
	require.Equal(t, 30, deltas[0].TimeoutSeconds)
	require.Equal(t, FirstTokenStatsAttemptSuccess, deltas[1].Outcome)
	require.Equal(t, 20, deltas[1].TimeoutSeconds)
	require.Equal(t, int64(1), deltas[1].TTFTSampleCount)
	require.Equal(t, FirstTokenStatsRequestRecoveredAfterTTFT, deltas[2].Outcome)
	require.Equal(t, int64(1), deltas[2].TTFTAffectedCount)
	require.Equal(t, 20, deltas[2].TimeoutSeconds)
}

func TestFirstTokenTrackingDerivesOtherFailureAfterTimeout(t *testing.T) {
	recorder := &firstTokenStatsRecorderSpy{}
	request := NewFirstTokenRequestTracker(recorder, context.Background(), ProtocolAnthropicMessages, "claude", FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	timedOut := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: PlatformAnthropic}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	timedOut.Finish(NewFirstTokenTimeoutFailoverError(), FirstTokenTimedOut)
	failed := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 2, Platform: PlatformAnthropic}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 18 * time.Second})
	failed.Finish(io.ErrUnexpectedEOF, FirstTokenPending)
	request.Finish()

	deltas := recorder.snapshot(t)
	require.Len(t, deltas, 3)
	require.Equal(t, FirstTokenStatsAttemptOtherFailure, deltas[1].Outcome)
	require.Equal(t, FirstTokenStatsFailureTransport, deltas[1].FailureKind)
	require.Equal(t, FirstTokenStatsRequestOtherFailure, deltas[2].Outcome)
	require.Equal(t, FirstTokenStatsFailureTransport, deltas[2].FailureKind)
	require.Equal(t, int64(1), deltas[2].TTFTAffectedCount)
	require.Equal(t, 18, deltas[2].TimeoutSeconds)
}

func TestFirstTokenTrackingDerivesExhaustedAndSelectionFailure(t *testing.T) {
	t.Run("last timeout is exhausted", func(t *testing.T) {
		recorder := &firstTokenStatsRecorderSpy{}
		request := NewFirstTokenRequestTracker(recorder, context.Background(), ProtocolResponses, "gpt-5", FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
		attempt := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: PlatformOpenAI}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 11 * time.Second})
		attempt.Finish(NewFirstTokenTimeoutFailoverError(), FirstTokenTimedOut)
		request.Finish()

		deltas := recorder.snapshot(t)
		require.Equal(t, FirstTokenStatsRequestTTFTExhausted, deltas[1].Outcome)
		require.Equal(t, int64(1), deltas[1].TTFTAffectedCount)
		require.Equal(t, 11, deltas[1].TimeoutSeconds)
	})

	t.Run("selection failure uses request snapshot", func(t *testing.T) {
		recorder := &firstTokenStatsRecorderSpy{}
		request := NewFirstTokenRequestTracker(recorder, context.Background(), ProtocolResponses, "gpt-5", FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 27 * time.Second})
		request.Finish()

		deltas := recorder.snapshot(t)
		require.Len(t, deltas, 1)
		require.Equal(t, FirstTokenStatsRequestOtherFailure, deltas[0].Outcome)
		require.Equal(t, FirstTokenStatsFailureOther, deltas[0].FailureKind)
		require.Equal(t, 27, deltas[0].TimeoutSeconds)
	})
}

func TestFirstTokenTrackingClientCancelWinsAndKeepsObservedTTFT(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	recorder := &firstTokenStatsRecorderSpy{}
	request := NewFirstTokenRequestTracker(recorder, parent, ProtocolResponses, "gpt-5", FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	attempt := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: PlatformOpenAI}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	attempt.MarkFirstToken(333 * time.Millisecond)
	cancel()
	attempt.Finish(&UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}, FirstTokenCommitted)
	request.Finish()

	deltas := recorder.snapshot(t)
	require.Equal(t, FirstTokenStatsAttemptClientCanceled, deltas[0].Outcome)
	require.Equal(t, int64(1), deltas[0].TTFTSampleCount)
	require.Equal(t, int64(333), deltas[0].TTFTSumMS)
	require.Equal(t, FirstTokenStatsRequestClientCanceled, deltas[1].Outcome)
	require.Zero(t, deltas[1].TTFTAffectedCount)
}

func TestFirstTokenTrackingCommitThenFailureKeepsTTFTSample(t *testing.T) {
	recorder := &firstTokenStatsRecorderSpy{}
	request := NewFirstTokenRequestTracker(recorder, context.Background(), ProtocolResponses, "gpt-5", FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	attempt := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: PlatformOpenAI}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	attempt.MarkFirstToken(410 * time.Millisecond)
	attempt.Finish(ErrStreamDataIntervalTimeout, FirstTokenCommitted)
	request.Finish()

	deltas := recorder.snapshot(t)
	require.Equal(t, FirstTokenStatsAttemptOtherFailure, deltas[0].Outcome)
	require.Equal(t, FirstTokenStatsFailureStreamIdleTimeout, deltas[0].FailureKind)
	require.Equal(t, int64(1), deltas[0].TTFTSampleCount)
	require.Equal(t, FirstTokenStatsRequestOtherFailure, deltas[1].Outcome)
}

func TestFirstTokenTrackingZeroMillisecondSampleIsObservedExactlyOnce(t *testing.T) {
	recorder := &firstTokenStatsRecorderSpy{}
	request := NewFirstTokenRequestTracker(recorder, context.Background(), ProtocolResponses, "gpt-5", FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	attempt := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: PlatformOpenAI}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})

	attempt.MarkFirstToken(0)
	attempt.MarkFirstToken(1500 * time.Millisecond)
	attempt.Finish(nil, FirstTokenCommitted)
	request.Finish()

	deltas := recorder.snapshot(t)
	require.Len(t, deltas, 2)
	require.Equal(t, int64(1), deltas[0].TTFTSampleCount)
	require.Zero(t, deltas[0].TTFTSumMS)
	require.Zero(t, deltas[0].TTFTMaxMS)
}

func TestFirstTokenTrackingRequestFinishReturnsBeforeActiveAttempt(t *testing.T) {
	recorder := newBlockingFirstTokenStatsRecorder()
	request := NewFirstTokenRequestTracker(recorder, context.Background(), ProtocolResponses, "gpt-5", FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	attempt := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: PlatformOpenAI}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 20 * time.Second})
	attempt.MarkFirstToken(75 * time.Millisecond)

	attemptDone := make(chan struct{})
	go func() {
		defer close(attemptDone)
		attempt.Finish(nil, FirstTokenCommitted)
	}()
	<-recorder.attemptRecordStarted

	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		request.Finish()
	}()

	select {
	case <-requestDone:
	case <-time.After(100 * time.Millisecond):
		close(recorder.releaseAttemptRecord)
		<-attemptDone
		<-requestDone
		t.Fatal("request Finish blocked on the active attempt")
	}
	require.Empty(t, recorder.snapshot(t), "request delta must wait for the active attempt outcome")

	close(recorder.releaseAttemptRecord)
	<-attemptDone
	<-requestDone
	deltas := recorder.snapshot(t)
	require.Len(t, deltas, 2)
	require.Equal(t, FirstTokenStatsScopeAttempt, deltas[0].Scope)
	require.Equal(t, FirstTokenStatsAttemptSuccess, deltas[0].Outcome)
	require.Equal(t, FirstTokenStatsScopeRequest, deltas[1].Scope)
	require.Equal(t, FirstTokenStatsRequestSuccess, deltas[1].Outcome)
}

func TestFirstTokenTrackingSuccessClosesNewAttempts(t *testing.T) {
	recorder := &firstTokenStatsRecorderSpy{}
	request := NewFirstTokenRequestTracker(recorder, context.Background(), ProtocolResponses, "gpt-5", FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	attempt := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: PlatformOpenAI}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	attempt.Finish(nil, FirstTokenCommitted)

	require.Nil(t, request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 2, Platform: PlatformOpenAI}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 20 * time.Second}))
	request.Finish()
	deltas := recorder.snapshot(t)
	require.Len(t, deltas, 2)
	require.Equal(t, FirstTokenStatsRequestSuccess, deltas[1].Outcome)
}

func TestFirstTokenTrackingFinalAttemptOverridesHistoricalSuccess(t *testing.T) {
	recorder := &firstTokenStatsRecorderSpy{}
	request := NewFirstTokenRequestTracker(recorder, context.Background(), ProtocolResponses, "gpt-5", FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	succeeded := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: PlatformOpenAI}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	laterFailure := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 2, Platform: PlatformOpenAI}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 20 * time.Second})

	succeeded.Finish(nil, FirstTokenCommitted)
	laterFailure.Finish(io.ErrUnexpectedEOF, FirstTokenPending)
	request.Finish()

	deltas := recorder.snapshot(t)
	require.Len(t, deltas, 3)
	require.Equal(t, FirstTokenStatsRequestOtherFailure, deltas[2].Outcome)
	require.Equal(t, FirstTokenStatsFailureTransport, deltas[2].FailureKind)
	require.Equal(t, 20, deltas[2].TimeoutSeconds)
}

func TestFirstTokenTrackingRejectsInvalidRequestDimensions(t *testing.T) {
	tests := []struct {
		name     string
		protocol FirstTokenProtocol
		model    string
	}{
		{name: "empty protocol", protocol: "", model: "gpt-5"},
		{name: "unknown protocol", protocol: FirstTokenProtocol("completions"), model: "gpt-5"},
		{name: "empty model", protocol: ProtocolResponses, model: ""},
		{name: "whitespace model", protocol: ProtocolResponses, model: "   "},
		{name: "invalid UTF-8 model", protocol: ProtocolResponses, model: string([]byte{0xff})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &firstTokenStatsRecorderSpy{}
			request := NewFirstTokenRequestTracker(recorder, context.Background(), tt.protocol, tt.model, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
			require.Nil(t, request)
			request.Finish()
			require.Empty(t, recorder.snapshot(t))
		})
	}
}

func TestFirstTokenTrackingTruncatesUnicodeModelByRunes(t *testing.T) {
	recorder := &firstTokenStatsRecorderSpy{}
	request := NewFirstTokenRequestTracker(recorder, context.Background(), ProtocolResponses, strings.Repeat("模", 300), FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	require.NotNil(t, request)
	attempt := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: PlatformOpenAI}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	require.NotNil(t, attempt)
	attempt.Finish(nil, FirstTokenCommitted)
	request.Finish()

	deltas := recorder.snapshot(t)
	require.Len(t, deltas, 2)
	for _, delta := range deltas {
		require.Equal(t, strings.Repeat("模", 255), delta.Model)
		require.Len(t, []rune(delta.Model), 255)
	}
}

func TestFirstTokenTrackingRejectsInvalidAttemptDimensions(t *testing.T) {
	tests := []struct {
		name string
		meta FirstTokenStatsAttemptMetadata
	}{
		{name: "zero account", meta: FirstTokenStatsAttemptMetadata{AccountID: 0, Platform: PlatformOpenAI}},
		{name: "negative account", meta: FirstTokenStatsAttemptMetadata{AccountID: -1, Platform: PlatformOpenAI}},
		{name: "empty platform", meta: FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: ""}},
		{name: "whitespace platform", meta: FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: "   "}},
		{name: "platform too long", meta: FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: strings.Repeat("平", 33)}},
		{name: "invalid UTF-8 platform", meta: FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: string([]byte{0xff})}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &firstTokenStatsRecorderSpy{}
			request := NewFirstTokenRequestTracker(recorder, context.Background(), ProtocolResponses, "gpt-5", FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
			require.Nil(t, request.BeginAttempt(tt.meta, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second}))
			request.Finish()
			deltas := recorder.snapshot(t)
			require.Len(t, deltas, 1)
			require.Equal(t, FirstTokenStatsScopeRequest, deltas[0].Scope)
		})
	}
}

func TestFirstTokenTrackingAcceptsPlatformAtRuneLimit(t *testing.T) {
	recorder := &firstTokenStatsRecorderSpy{}
	request := NewFirstTokenRequestTracker(recorder, context.Background(), ProtocolResponses, "gpt-5", FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	platform := strings.Repeat("平", 32)
	attempt := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: platform}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	require.NotNil(t, attempt)
	attempt.Finish(nil, FirstTokenCommitted)
	request.Finish()

	deltas := recorder.snapshot(t)
	require.Equal(t, platform, deltas[0].Platform)
}

func TestFirstTokenTrackingTimeoutSecondsCeilsWithoutOverflow(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		want    int
	}{
		{name: "zero clamps to minimum", timeout: 0, want: 1},
		{name: "subsecond rounds up", timeout: time.Nanosecond, want: 1},
		{name: "whole second stays exact", timeout: time.Second, want: 1},
		{name: "fraction rounds up", timeout: time.Second + time.Nanosecond, want: 2},
		{name: "maximum stays exact", timeout: 300 * time.Second, want: 300},
		{name: "above maximum clamps", timeout: 300*time.Second + time.Nanosecond, want: 300},
		{name: "duration maximum does not overflow", timeout: time.Duration(1<<63 - 1), want: 300},
		{name: "duration minimum clamps", timeout: time.Duration(-1 << 63), want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, firstTokenTimeoutSeconds(tt.timeout))
		})
	}
}

func TestFirstTokenTrackingConcurrentFirstMarkAndFinish(t *testing.T) {
	recorder := &firstTokenStatsRecorderSpy{}
	request := NewFirstTokenRequestTracker(recorder, context.Background(), ProtocolResponses, "gpt-5", FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	attempt := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: PlatformOpenAI}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 19 * time.Second})

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(3)
		go func(elapsed time.Duration) {
			defer wg.Done()
			<-start
			attempt.MarkFirstToken(elapsed)
		}(time.Duration(i+1) * time.Millisecond)
		go func() {
			defer wg.Done()
			<-start
			attempt.Finish(nil, FirstTokenCommitted)
		}()
		go func() {
			defer wg.Done()
			<-start
			request.Finish()
		}()
	}
	close(start)
	wg.Wait()

	deltas := recorder.snapshot(t)
	require.Len(t, deltas, 2)
	require.Equal(t, FirstTokenStatsAttemptSuccess, deltas[0].Outcome)
	require.Contains(t, []int64{0, 1}, deltas[0].TTFTSampleCount)
	if deltas[0].TTFTSampleCount == 1 {
		require.GreaterOrEqual(t, deltas[0].TTFTSumMS, int64(1))
		require.LessOrEqual(t, deltas[0].TTFTSumMS, int64(32))
		require.Equal(t, deltas[0].TTFTSumMS, deltas[0].TTFTMaxMS)
	}
	require.Equal(t, FirstTokenStatsRequestSuccess, deltas[1].Outcome)
	require.Equal(t, 19, deltas[1].TimeoutSeconds)
}

func TestFirstTokenTrackingTimeoutClassificationWinsOverOtherFailure(t *testing.T) {
	recorder := &firstTokenStatsRecorderSpy{}
	request := NewFirstTokenRequestTracker(recorder, context.Background(), ProtocolResponses, "gpt-5", FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	attempt := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: PlatformOpenAI}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	attempt.Finish(&UpstreamFailoverError{StatusCode: http.StatusUnauthorized}, FirstTokenTimedOut)
	request.Finish()

	deltas := recorder.snapshot(t)
	require.Equal(t, FirstTokenStatsAttemptTTFTTimeout, deltas[0].Outcome)
	require.Empty(t, deltas[0].FailureKind)
	require.Equal(t, FirstTokenStatsRequestTTFTExhausted, deltas[1].Outcome)
}

func TestFirstTokenTrackingTTFTUsesRequestClientContext(t *testing.T) {
	recorder := &firstTokenStatsRecorderSpy{}
	request := NewFirstTokenRequestTracker(recorder, context.Background(), ProtocolResponses, "gpt-5", FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	attempt := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: PlatformOpenAI}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	attempt.Finish(NewFirstTokenTimeoutFailoverError(), FirstTokenTimedOut)
	request.Finish()

	deltas := recorder.snapshot(t)
	require.Equal(t, FirstTokenStatsAttemptTTFTTimeout, deltas[0].Outcome)
	require.Equal(t, FirstTokenStatsRequestTTFTExhausted, deltas[1].Outcome)
}

func TestFirstTokenTrackingTimedOutAttemptDiscardsObservedFirstToken(t *testing.T) {
	recorder := &firstTokenStatsRecorderSpy{}
	request := NewFirstTokenRequestTracker(recorder, context.Background(), ProtocolResponses, "gpt-5", FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	attempt := request.BeginAttempt(FirstTokenStatsAttemptMetadata{AccountID: 1, Platform: PlatformOpenAI}, FirstTokenTimeoutSnapshot{Enabled: true, Timeout: 30 * time.Second})
	attempt.MarkFirstToken(275 * time.Millisecond)
	attempt.Finish(NewFirstTokenTimeoutFailoverError(), FirstTokenTimedOut)
	request.Finish()

	deltas := recorder.snapshot(t)
	require.Equal(t, FirstTokenStatsAttemptTTFTTimeout, deltas[0].Outcome)
	require.Zero(t, deltas[0].TTFTSampleCount)
	require.Zero(t, deltas[0].TTFTSumMS)
	require.Zero(t, deltas[0].TTFTMaxMS)
}

func TestFirstTokenFailureKindPriority(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "rate status", err: &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}, want: FirstTokenStatsFailureRateLimit},
		{name: "rate type", err: &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ErrorType: " RATE_LIMIT_ERROR "}, want: FirstTokenStatsFailureRateLimit},
		{name: "rate wins over auth status", err: &UpstreamFailoverError{StatusCode: http.StatusUnauthorized, ErrorType: "rate_limit_error"}, want: FirstTokenStatsFailureRateLimit},
		{name: "rate status wins over auth type", err: &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests, ErrorType: "authentication_error"}, want: FirstTokenStatsFailureRateLimit},
		{name: "auth status", err: &UpstreamFailoverError{StatusCode: http.StatusUnauthorized}, want: FirstTokenStatsFailureAuth},
		{name: "forbidden status", err: &UpstreamFailoverError{StatusCode: http.StatusForbidden}, want: FirstTokenStatsFailureAuth},
		{name: "auth type", err: &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ErrorType: "authentication_error"}, want: FirstTokenStatsFailureAuth},
		{name: "other 4xx", err: &UpstreamFailoverError{StatusCode: http.StatusBadRequest}, want: FirstTokenStatsFailureUpstream4xx},
		{name: "5xx", err: &UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable}, want: FirstTokenStatsFailureUpstream5xx},
		{name: "wrapped reset", err: &net.OpError{Op: "read", Err: syscall.ECONNRESET}, want: FirstTokenStatsFailureTransport},
		{name: "eof", err: io.EOF, want: FirstTokenStatsFailureTransport},
		{name: "tls", err: tls.RecordHeaderError{}, want: FirstTokenStatsFailureTransport},
		{name: "tls pointer", err: &tls.RecordHeaderError{}, want: FirstTokenStatsFailureTransport},
		{name: "x509 hostname", err: x509.HostnameError{Certificate: &x509.Certificate{DNSNames: []string{"other.example"}}, Host: "api.example"}, want: FirstTokenStatsFailureTransport},
		{name: "context canceled without canceled parent", err: context.Canceled, want: FirstTokenStatsFailureTransport},
		{name: "context deadline", err: context.DeadlineExceeded, want: FirstTokenStatsFailureTransport},
		{name: "idle", err: ErrStreamDataIntervalTimeout, want: FirstTokenStatsFailureStreamIdleTimeout},
		{name: "transport wins over joined idle sentinel", err: errors.Join(context.DeadlineExceeded, ErrStreamDataIntervalTimeout), want: FirstTokenStatsFailureTransport},
		{name: "json", err: &json.SyntaxError{}, want: FirstTokenStatsFailureProtocol},
		{name: "json type", err: &json.UnmarshalTypeError{}, want: FirstTokenStatsFailureProtocol},
		{name: "sse event", err: &sseStreamErrorEventError{}, want: FirstTokenStatsFailureProtocol},
		{name: "stable invalid json", err: errors.New("invalid json"), want: FirstTokenStatsFailureProtocol},
		{name: "prelude", err: NewFirstTokenPreludeOverflowFailoverError(), want: FirstTokenStatsFailureProtocol},
		{name: "wrapped prelude sentinel", err: errors.Join(errors.New("decode failed"), ErrFirstTokenPreludeTooLarge), want: FirstTokenStatsFailureProtocol},
		{name: "unknown rate-like type", err: &UpstreamFailoverError{ErrorType: "rate_limit_errorish"}, want: FirstTokenStatsFailureOther},
		{name: "unknown auth-like type", err: &UpstreamFailoverError{ErrorType: "authentication_errorish"}, want: FirstTokenStatsFailureOther},
		{name: "rate-like body is ignored", err: &UpstreamFailoverError{ResponseBody: []byte(`{"error":"rate limit"}`)}, want: FirstTokenStatsFailureOther},
		{name: "transport-like body is ignored", err: errors.New("dial tcp connection reset by peer"), want: FirstTokenStatsFailureOther},
		{name: "json-like body is ignored", err: errors.New("invalid json from database"), want: FirstTokenStatsFailureOther},
		{name: "nil", err: nil, want: FirstTokenStatsFailureOther},
		{name: "other", err: errors.New("database unavailable"), want: FirstTokenStatsFailureOther},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, classifyFirstTokenFailureKind(tt.err))
		})
	}
}

func TestFirstTokenFailureKindEligibleStreamsUseIdleTimeoutSentinel(t *testing.T) {
	tests := []struct {
		file         string
		function     string
		wantSentinel bool
	}{
		{file: "bedrock_stream.go", function: "handleBedrockStreamingResponse", wantSentinel: true},
		{file: "antigravity_gateway_streaming.go", function: "handleGeminiStreamingResponse", wantSentinel: true},
		{file: "antigravity_gateway_streaming.go", function: "handleGeminiStreamToNonStreaming", wantSentinel: false},
		{file: "antigravity_gateway_streaming.go", function: "handleClaudeStreamToNonStreaming", wantSentinel: false},
		{file: "antigravity_gateway_streaming.go", function: "handleClaudeStreamingResponse", wantSentinel: true},
		{file: "gateway_anthropic_passthrough.go", function: "handleStreamingResponseAnthropicAPIKeyPassthrough", wantSentinel: true},
		{file: "gateway_upstream_response.go", function: "handleStreamingResponse", wantSentinel: true},
		{file: "openai_gateway_chat_completions.go", function: "handleChatStreamingResponse", wantSentinel: true},
		{file: "openai_gateway_messages.go", function: "readOpenAICompatBufferedTerminal", wantSentinel: false},
		{file: "openai_gateway_messages.go", function: "handleAnthropicStreamingResponse", wantSentinel: true},
		{file: "openai_gateway_response_handling.go", function: "handleStreamingResponse", wantSentinel: true},
	}
	for _, tt := range tests {
		t.Run(tt.file+"/"+tt.function, func(t *testing.T) {
			body := firstTokenFunctionSource(t, tt.file, tt.function)
			require.Equal(t, tt.wantSentinel, strings.Contains(body, "ErrStreamDataIntervalTimeout"), "sentinel ownership mismatch")
			require.Equal(t, !tt.wantSentinel, strings.Contains(body, `fmt.Errorf("stream data interval timeout")`), "literal error ownership mismatch")
		})
	}
}

func firstTokenFunctionSource(t *testing.T, filename, function string) string {
	t.Helper()
	source, err := os.ReadFile(filename)
	require.NoError(t, err)
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, filename, source, 0)
	require.NoError(t, err)
	for _, declaration := range parsed.Decls {
		functionDeclaration, ok := declaration.(*ast.FuncDecl)
		if !ok || functionDeclaration.Name.Name != function {
			continue
		}
		start := fset.Position(functionDeclaration.Pos()).Offset
		end := fset.Position(functionDeclaration.End()).Offset
		return string(source[start:end])
	}
	require.FailNow(t, "function not found", "%s in %s", function, filename)
	return ""
}
