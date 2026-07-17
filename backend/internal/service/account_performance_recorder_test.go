package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type accountPerformanceRecorderRepoSpy struct {
	mu          sync.Mutex
	upserts     [][]AccountPerformanceDelta
	upsert      func(context.Context, []AccountPerformanceDelta) error
	rollups     int
	minuteClean int
	hourlyClean int
}

func (s *accountPerformanceRecorderRepoSpy) UpsertMinuteBatch(ctx context.Context, deltas []AccountPerformanceDelta) error {
	if s.upsert != nil {
		if err := s.upsert(ctx, deltas); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.upserts = append(s.upserts, append([]AccountPerformanceDelta(nil), deltas...))
	s.mu.Unlock()
	return nil
}

func (s *accountPerformanceRecorderRepoSpy) RollupClosedHours(context.Context, time.Time) error {
	s.mu.Lock()
	s.rollups++
	s.mu.Unlock()
	return nil
}

func (s *accountPerformanceRecorderRepoSpy) DeleteMinuteBefore(context.Context, time.Time) (int64, error) {
	s.mu.Lock()
	s.minuteClean++
	s.mu.Unlock()
	return 0, nil
}

func (s *accountPerformanceRecorderRepoSpy) DeleteHourlyBefore(context.Context, time.Time) (int64, error) {
	s.mu.Lock()
	s.hourlyClean++
	s.mu.Unlock()
	return 0, nil
}

func (s *accountPerformanceRecorderRepoSpy) QueryOverview(context.Context, AccountPerformanceOverviewFilter) (*AccountPerformanceOverview, error) {
	return nil, nil
}
func (s *accountPerformanceRecorderRepoSpy) QueryAccounts(context.Context, AccountPerformanceAccountFilter) (*AccountPerformanceAccountPage, error) {
	return nil, nil
}
func (s *accountPerformanceRecorderRepoSpy) QueryInvestigation(context.Context, AccountPerformanceInvestigationFilter) (*AccountPerformanceInvestigation, error) {
	return nil, nil
}

func newAccountPerformanceRecorderForTest(repo AccountPerformanceRepository, capacity, keys int) *AsyncAccountPerformanceRecorder {
	return newAccountPerformanceRecorderWithConfig(repo, accountPerformanceRecorderConfig{
		queueCapacity: capacity, flushKeys: keys, flushInterval: time.Hour, rollupInterval: time.Hour, cleanupInterval: time.Hour,
		operationTimeout: time.Second,
	})
}

func validAccountPerformanceRecorderDelta(accountID int64) AccountPerformanceDelta {
	return AccountPerformanceDelta{BucketStart: time.Now(), AccountID: accountID, Platform: "openai", GroupID: 1, Model: "gpt-5", Protocol: "responses", Outcome: AccountPerformanceOutcomeSuccess, AttemptCount: 1}
}

func TestAccountPerformanceRecorderDropsWithoutBlockingWhenQueueIsFull(t *testing.T) {
	started, release := make(chan struct{}, 1), make(chan struct{})
	repo := &accountPerformanceRecorderRepoSpy{upsert: func(context.Context, []AccountPerformanceDelta) error {
		started <- struct{}{}
		<-release
		return nil
	}}
	recorder := newAccountPerformanceRecorderForTest(repo, 1, 1)
	recorder.Start(context.Background())
	t.Cleanup(func() { close(release); recorder.Stop() })
	recorder.Record(validAccountPerformanceRecorderDelta(1))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for flush")
	}
	recorder.Record(validAccountPerformanceRecorderDelta(2))
	returned := make(chan struct{})
	go func() { recorder.Record(validAccountPerformanceRecorderDelta(3)); close(returned) }()
	select {
	case <-returned:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Record blocked while queue was full")
	}
	require.Eventually(t, func() bool { return recorder.Health().DroppedSamples == 1 }, time.Second, time.Millisecond)
}

func TestAccountPerformanceRecorderFlushesAndReportsHealthy(t *testing.T) {
	repo := &accountPerformanceRecorderRepoSpy{}
	recorder := newAccountPerformanceRecorderForTest(repo, 8, 1)
	recorder.Start(context.Background())
	recorder.Record(validAccountPerformanceRecorderDelta(7))
	require.Eventually(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return len(repo.upserts) == 1
	}, time.Second, time.Millisecond)
	health := recorder.Health()
	require.Equal(t, AccountPerformanceCollectionComplete, health.Status)
	require.Zero(t, health.DroppedSamples)
	require.NotNil(t, health.LastSuccessfulFlushAt)
	recorder.Stop()
	require.Zero(t, recorder.Health().PendingSamples)
}

func TestAccountPerformanceRecorderDropsInvalidDeltasAndRunsMaintenance(t *testing.T) {
	repo := &accountPerformanceRecorderRepoSpy{}
	recorder := newAccountPerformanceRecorderWithConfig(repo, accountPerformanceRecorderConfig{
		queueCapacity: 4, flushKeys: 4, flushInterval: time.Hour, rollupInterval: time.Millisecond, cleanupInterval: time.Millisecond, operationTimeout: time.Second,
	})
	recorder.Start(context.Background())
	invalid := validAccountPerformanceRecorderDelta(1)
	invalid.Protocol = "\x00"
	recorder.Record(invalid)
	require.Eventually(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return repo.rollups > 0 && repo.minuteClean > 0 && repo.hourlyClean > 0
	}, time.Second, time.Millisecond)
	require.Equal(t, AccountPerformanceCollectionDegraded, recorder.Health().Status)
	require.EqualValues(t, 1, recorder.Health().DroppedSamples)
	recorder.Stop()
}
