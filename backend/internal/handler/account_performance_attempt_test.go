package handler

import (
	"context"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type accountPerformanceRecorderSpy struct {
	mu     sync.Mutex
	deltas []service.AccountPerformanceDelta
}

func (s *accountPerformanceRecorderSpy) Record(delta service.AccountPerformanceDelta) {
	s.mu.Lock()
	s.deltas = append(s.deltas, delta)
	s.mu.Unlock()
}

func (s *accountPerformanceRecorderSpy) Health() service.AccountPerformanceCollectionHealth {
	return service.AccountPerformanceCollectionHealth{Status: service.AccountPerformanceCollectionComplete}
}

func TestAccountPerformanceAttemptRecordsTTFTTimeout(t *testing.T) {
	recorder := &accountPerformanceRecorderSpy{}
	attempt := beginAccountPerformanceAttempt(recorder, 7, service.PlatformOpenAI, 3, service.ProtocolResponses, "gpt-5", 1)
	attempt.Finish(context.Background(), service.NewFirstTokenTimeoutFailoverError(), nil)
	require.Len(t, recorder.deltas, 1)
	require.Equal(t, service.AccountPerformanceOutcomeTTFTTimeout, recorder.deltas[0].Outcome)
	require.True(t, recorder.deltas[0].Failover)
	require.Nil(t, recorder.deltas[0].TTFTMS)
	require.Nil(t, recorder.deltas[0].DurationMS)
}

func TestAccountPerformanceAttemptRecordsClientCancellationSeparately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recorder := &accountPerformanceRecorderSpy{}
	attempt := beginAccountPerformanceAttempt(recorder, 7, service.PlatformOpenAI, 3, service.ProtocolResponses, "gpt-5", 0)
	attempt.Finish(ctx, context.Canceled, nil)
	require.Len(t, recorder.deltas, 1)
	require.Equal(t, service.AccountPerformanceOutcomeClientCanceled, recorder.deltas[0].Outcome)
}

func TestAccountPerformanceAttemptRecordsSuccessfulLatency(t *testing.T) {
	recorder := &accountPerformanceRecorderSpy{}
	attempt := beginAccountPerformanceAttempt(recorder, 7, service.PlatformOpenAI, 3, service.ProtocolResponses, "gpt-5", 0)
	ttft := int64(125)
	attempt.Finish(context.Background(), nil, &service.OpenAIForwardResult{FirstTokenMs: &ttft})
	require.Len(t, recorder.deltas, 1)
	require.Equal(t, service.AccountPerformanceOutcomeSuccess, recorder.deltas[0].Outcome)
	require.Equal(t, &ttft, recorder.deltas[0].TTFTMS)
	require.NotNil(t, recorder.deltas[0].DurationMS)
}
