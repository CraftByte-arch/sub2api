package service

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type firstTokenStatsRecorderRepoStub struct {
	upsertBatch  func(context.Context, []FirstTokenStatsDelta) error
	deleteBefore func(context.Context, time.Time) (int64, error)
}

type firstTokenStatsRecorderLogHandler struct {
	records chan slog.Record
}

func (h *firstTokenStatsRecorderLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *firstTokenStatsRecorderLogHandler) Handle(_ context.Context, record slog.Record) error {
	select {
	case h.records <- record.Clone():
	default:
	}
	return nil
}

func (h *firstTokenStatsRecorderLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *firstTokenStatsRecorderLogHandler) WithGroup(string) slog.Handler { return h }

func (s *firstTokenStatsRecorderRepoStub) UpsertBatch(ctx context.Context, deltas []FirstTokenStatsDelta) error {
	if s.upsertBatch != nil {
		return s.upsertBatch(ctx, deltas)
	}
	return nil
}

func (s *firstTokenStatsRecorderRepoStub) QueryOverview(context.Context, FirstTokenStatsOverviewFilter) (*FirstTokenStatsOverview, error) {
	return nil, nil
}

func (s *firstTokenStatsRecorderRepoStub) QueryAccounts(context.Context, FirstTokenStatsAccountFilter) (*FirstTokenStatsAccountPage, error) {
	return nil, nil
}

func (s *firstTokenStatsRecorderRepoStub) DeleteBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if s.deleteBefore != nil {
		return s.deleteBefore(ctx, cutoff)
	}
	return 0, nil
}

func TestFirstTokenStatsRecorderRecordIsNonBlockingWhenQueueIsFull(t *testing.T) {
	flushStarted := make(chan struct{}, 1)
	releaseFlush := make(chan struct{})
	repo := &firstTokenStatsRecorderRepoStub{
		upsertBatch: func(context.Context, []FirstTokenStatsDelta) error {
			select {
			case flushStarted <- struct{}{}:
			default:
			}
			<-releaseFlush
			return nil
		},
	}
	recorder := newFirstTokenTimeoutStatsRecorderWithConfig(repo, firstTokenTimeoutStatsRecorderConfig{
		queueCapacity:    1,
		flushUniqueKeys:  1,
		flushInterval:    time.Hour,
		cleanupInterval:  time.Hour,
		operationTimeout: time.Second,
		now:              time.Now,
	})
	recorder.Start(context.Background())
	t.Cleanup(func() {
		select {
		case <-releaseFlush:
		default:
			close(releaseFlush)
		}
		recorder.Stop()
	})

	recorder.Record(validFirstTokenStatsRecorderDelta(1))
	select {
	case <-flushStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for blocking flush")
	}
	recorder.Record(validFirstTokenStatsRecorderDelta(2))

	recordReturned := make(chan struct{})
	go func() {
		recorder.Record(validFirstTokenStatsRecorderDelta(3))
		close(recordReturned)
	}()
	select {
	case <-recordReturned:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Record blocked while the queue was full")
	}

	require.Equal(t, FirstTokenTimeoutStatsRecorderHealth{
		Status:         FirstTokenStatsCompletenessDegraded,
		DroppedSamples: 1,
		PendingSamples: 2,
	}, recorder.Health())
}

func TestFirstTokenStatsRecorderConcurrentRecordIsRaceFree(t *testing.T) {
	const records = 128
	var (
		mu        sync.Mutex
		persisted int64
	)
	repo := &firstTokenStatsRecorderRepoStub{
		upsertBatch: func(_ context.Context, deltas []FirstTokenStatsDelta) error {
			mu.Lock()
			defer mu.Unlock()
			for _, delta := range deltas {
				persisted += delta.SampleCount
			}
			return nil
		},
	}
	recorder := newFirstTokenTimeoutStatsRecorderWithConfig(repo, firstTokenTimeoutStatsRecorderConfig{
		queueCapacity:    records,
		flushUniqueKeys:  1000,
		flushInterval:    time.Hour,
		cleanupInterval:  time.Hour,
		operationTimeout: time.Second,
		now:              time.Now,
	})
	recorder.Start(context.Background())

	var wg sync.WaitGroup
	for i := 0; i < records; i++ {
		wg.Add(1)
		go func(accountID int64) {
			defer wg.Done()
			recorder.Record(validFirstTokenStatsRecorderDelta(accountID))
		}(int64(i + 1))
	}
	wg.Wait()
	recorder.Stop()

	mu.Lock()
	require.Equal(t, int64(records), persisted)
	mu.Unlock()
	require.Equal(t, int64(0), recorder.Health().PendingSamples)
}

func TestFirstTokenStatsRecorderAggregatesByCompleteKeyAndFlushesAtUniqueThreshold(t *testing.T) {
	flushed := make(chan []FirstTokenStatsDelta, 1)
	repo := &firstTokenStatsRecorderRepoStub{
		upsertBatch: func(_ context.Context, deltas []FirstTokenStatsDelta) error {
			flushed <- append([]FirstTokenStatsDelta(nil), deltas...)
			return nil
		},
	}
	recorder := newFirstTokenTimeoutStatsRecorderWithConfig(repo, firstTokenTimeoutStatsRecorderConfig{
		queueCapacity:    8,
		flushUniqueKeys:  2,
		flushInterval:    time.Hour,
		cleanupInterval:  time.Hour,
		operationTimeout: time.Second,
		now:              time.Now,
	})
	recorder.Start(context.Background())
	defer recorder.Stop()

	first := validFirstTokenStatsRecorderDelta(42)
	first.TTFTSampleCount = 1
	first.TTFTSumMS = 10
	first.TTFTMaxMS = 10
	second := first
	second.SampleCount = 2
	second.TTFTSampleCount = 2
	second.TTFTSumMS = 50
	second.TTFTMaxMS = 30
	differentThreshold := validFirstTokenStatsRecorderDelta(42)
	differentThreshold.TimeoutSeconds = 20

	recorder.Record(first)
	recorder.Record(second)
	recorder.Record(differentThreshold)

	var batch []FirstTokenStatsDelta
	select {
	case batch = <-flushed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for unique-key flush")
	}
	require.Len(t, batch, 2)
	byThreshold := make(map[int]FirstTokenStatsDelta, len(batch))
	for _, delta := range batch {
		byThreshold[delta.TimeoutSeconds] = delta
	}
	require.Equal(t, int64(3), byThreshold[30].SampleCount)
	require.Equal(t, int64(3), byThreshold[30].TTFTSampleCount)
	require.Equal(t, int64(60), byThreshold[30].TTFTSumMS)
	require.Equal(t, int64(30), byThreshold[30].TTFTMaxMS)
	require.Equal(t, int64(1), byThreshold[20].SampleCount)
}

func TestFirstTokenStatsRecorderFlushesOnTicker(t *testing.T) {
	flushed := make(chan []FirstTokenStatsDelta, 1)
	repo := &firstTokenStatsRecorderRepoStub{
		upsertBatch: func(_ context.Context, deltas []FirstTokenStatsDelta) error {
			flushed <- append([]FirstTokenStatsDelta(nil), deltas...)
			return nil
		},
	}
	recorder := newFirstTokenTimeoutStatsRecorderWithConfig(repo, firstTokenTimeoutStatsRecorderConfig{
		queueCapacity:    2,
		flushUniqueKeys:  1000,
		flushInterval:    5 * time.Millisecond,
		cleanupInterval:  time.Hour,
		operationTimeout: time.Second,
		now:              time.Now,
	})
	recorder.Start(context.Background())
	defer recorder.Stop()
	recorder.Record(validFirstTokenStatsRecorderDelta(42))

	select {
	case batch := <-flushed:
		require.Len(t, batch, 1)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ticker flush")
	}
}

func TestFirstTokenStatsRecorderRejectsInvalidDeltaWithoutPoisoningBatch(t *testing.T) {
	var (
		mu      sync.Mutex
		batches [][]FirstTokenStatsDelta
	)
	repo := &firstTokenStatsRecorderRepoStub{
		upsertBatch: func(_ context.Context, deltas []FirstTokenStatsDelta) error {
			mu.Lock()
			defer mu.Unlock()
			batches = append(batches, append([]FirstTokenStatsDelta(nil), deltas...))
			return nil
		},
	}
	recorder := newFirstTokenTimeoutStatsRecorderWithConfig(repo, firstTokenTimeoutStatsRecorderConfig{
		queueCapacity:    8,
		flushUniqueKeys:  1000,
		flushInterval:    time.Hour,
		cleanupInterval:  time.Hour,
		operationTimeout: time.Second,
		now:              time.Now,
	})
	recorder.Start(context.Background())

	invalidNUL := validFirstTokenStatsRecorderDelta(1)
	invalidNUL.Protocol = "responses\x00v2"
	invalidNUL.SampleCount = 3
	invalidUTF8 := validFirstTokenStatsRecorderDelta(2)
	invalidUTF8.Model = string([]byte{0xff})
	invalidInvariant := validFirstTokenStatsRecorderDelta(3)
	invalidInvariant.TTFTSampleCount = 1
	invalidInvariant.TTFTSumMS = 1
	invalidInvariant.TTFTMaxMS = 1
	invalidInvariant.Outcome = FirstTokenStatsAttemptTTFTTimeout
	invalidThreshold := validFirstTokenStatsRecorderDelta(4)
	invalidThreshold.TimeoutSeconds = 301

	recorder.Record(invalidNUL)
	recorder.Record(invalidUTF8)
	recorder.Record(invalidInvariant)
	recorder.Record(invalidThreshold)
	recorder.Record(validFirstTokenStatsRecorderDelta(5))
	recorder.Stop()

	mu.Lock()
	require.Len(t, batches, 1)
	require.Equal(t, []FirstTokenStatsDelta{validFirstTokenStatsRecorderDelta(5)}, batches[0])
	mu.Unlock()
	require.Equal(t, int64(6), recorder.Health().DroppedSamples)
	require.Equal(t, int64(0), recorder.Health().PendingSamples)
}

func TestFirstTokenStatsRecorderMergeOverflowDropsOnlyIncomingDelta(t *testing.T) {
	flushed := make(chan []FirstTokenStatsDelta, 1)
	repo := &firstTokenStatsRecorderRepoStub{
		upsertBatch: func(_ context.Context, deltas []FirstTokenStatsDelta) error {
			flushed <- append([]FirstTokenStatsDelta(nil), deltas...)
			return nil
		},
	}
	recorder := newFirstTokenTimeoutStatsRecorderWithConfig(repo, firstTokenTimeoutStatsRecorderConfig{
		queueCapacity:    2,
		flushUniqueKeys:  1000,
		flushInterval:    time.Hour,
		cleanupInterval:  time.Hour,
		operationTimeout: time.Second,
		now:              time.Now,
	})
	recorder.Start(context.Background())

	base := validFirstTokenStatsRecorderDelta(42)
	base.TTFTSampleCount = 1
	base.TTFTSumMS = math.MaxInt64
	base.TTFTMaxMS = math.MaxInt32
	incoming := validFirstTokenStatsRecorderDelta(42)
	incoming.TTFTSampleCount = 1
	incoming.TTFTSumMS = 1
	incoming.TTFTMaxMS = 1
	recorder.Record(base)
	recorder.Record(incoming)
	recorder.Stop()

	select {
	case batch := <-flushed:
		require.Equal(t, []FirstTokenStatsDelta{base}, batch)
	default:
		t.Fatal("expected shutdown flush")
	}
	require.Equal(t, int64(1), recorder.Health().DroppedSamples)
	require.Equal(t, int64(0), recorder.Health().PendingSamples)
}

func TestFirstTokenStatsRecorderPendingOverflowRejectsIncomingDelta(t *testing.T) {
	flushStarted := make(chan struct{}, 1)
	releaseFlush := make(chan struct{})
	repo := &firstTokenStatsRecorderRepoStub{
		upsertBatch: func(context.Context, []FirstTokenStatsDelta) error {
			flushStarted <- struct{}{}
			<-releaseFlush
			return nil
		},
	}
	recorder := newFirstTokenTimeoutStatsRecorderWithConfig(repo, firstTokenTimeoutStatsRecorderConfig{
		queueCapacity:    1,
		flushUniqueKeys:  1,
		flushInterval:    time.Hour,
		cleanupInterval:  time.Hour,
		operationTimeout: time.Second,
		now:              time.Now,
	})
	recorder.Start(context.Background())
	base := validFirstTokenStatsRecorderDelta(42)
	base.SampleCount = math.MaxInt64
	recorder.Record(base)
	select {
	case <-flushStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for blocking flush")
	}
	recorder.Record(validFirstTokenStatsRecorderDelta(43))
	require.Equal(t, int64(math.MaxInt64), recorder.Health().PendingSamples)
	require.Equal(t, int64(1), recorder.Health().DroppedSamples)
	close(releaseFlush)
	recorder.Stop()
	require.Equal(t, int64(0), recorder.Health().PendingSamples)
}

func TestFirstTokenStatsRecorderInvalidDeltaUsesSafeDroppedWeight(t *testing.T) {
	tests := map[string]func(*FirstTokenStatsDelta){
		"zero bucket":   func(delta *FirstTokenStatsDelta) { delta.BucketStart = time.Time{} },
		"unknown scope": func(delta *FirstTokenStatsDelta) { delta.Scope = "unknown" },
		"request account sentinel": func(delta *FirstTokenStatsDelta) {
			delta.Scope = FirstTokenStatsScopeRequest
			delta.Outcome = FirstTokenStatsRequestSuccess
		},
		"empty protocol":       func(delta *FirstTokenStatsDelta) { delta.Protocol = " " },
		"long protocol":        func(delta *FirstTokenStatsDelta) { delta.Protocol = strings.Repeat("p", 33) },
		"long platform":        func(delta *FirstTokenStatsDelta) { delta.Platform = strings.Repeat("p", 33) },
		"long model":           func(delta *FirstTokenStatsDelta) { delta.Model = strings.Repeat("m", 256) },
		"zero sample":          func(delta *FirstTokenStatsDelta) { delta.SampleCount = 0 },
		"negative ttft sample": func(delta *FirstTokenStatsDelta) { delta.TTFTSampleCount = -1 },
		"ttft max over int4":   func(delta *FirstTokenStatsDelta) { delta.TTFTMaxMS = math.MaxInt32 + 1 },
		"unknown outcome":      func(delta *FirstTokenStatsDelta) { delta.Outcome = "unknown" },
		"unexpected failure kind": func(delta *FirstTokenStatsDelta) {
			delta.FailureKind = FirstTokenStatsFailureTransport
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			recorder := newFirstTokenTimeoutStatsRecorderWithConfig(&firstTokenStatsRecorderRepoStub{}, firstTokenTimeoutStatsRecorderConfig{
				queueCapacity:    1,
				flushUniqueKeys:  1,
				flushInterval:    time.Hour,
				cleanupInterval:  time.Hour,
				operationTimeout: time.Second,
				now:              time.Now,
			})
			recorder.Start(context.Background())
			delta := validFirstTokenStatsRecorderDelta(42)
			mutate(&delta)
			recorder.Record(delta)
			recorder.Stop()
			require.Equal(t, int64(1), recorder.Health().DroppedSamples)
			require.Equal(t, int64(0), recorder.Health().PendingSamples)
		})
	}
}

func TestFirstTokenStatsRecorderRepositoryFailureDropsBatchAndLaterSuccessKeepsDegraded(t *testing.T) {
	var calls int
	repo := &firstTokenStatsRecorderRepoStub{
		upsertBatch: func(context.Context, []FirstTokenStatsDelta) error {
			calls++
			if calls == 1 {
				return errors.New("db unavailable")
			}
			return nil
		},
	}
	fixedNow := time.Date(2026, 7, 15, 8, 30, 0, 0, time.FixedZone("test", 8*60*60))
	recorder := newFirstTokenTimeoutStatsRecorderWithConfig(repo, firstTokenTimeoutStatsRecorderConfig{
		queueCapacity:    2,
		flushUniqueKeys:  1,
		flushInterval:    time.Hour,
		cleanupInterval:  time.Hour,
		operationTimeout: time.Second,
		now:              func() time.Time { return fixedNow },
	})
	recorder.Start(context.Background())
	defer recorder.Stop()

	failed := validFirstTokenStatsRecorderDelta(1)
	failed.SampleCount = 3
	recorder.Record(failed)
	waitForFirstTokenStatsRecorderHealth(t, recorder, func(health FirstTokenTimeoutStatsRecorderHealth) bool {
		return health.PendingSamples == 0 && health.DroppedSamples == 3
	})
	require.Nil(t, recorder.Health().LastSuccessfulFlushAt)

	succeeded := validFirstTokenStatsRecorderDelta(2)
	succeeded.SampleCount = 2
	recorder.Record(succeeded)
	waitForFirstTokenStatsRecorderHealth(t, recorder, func(health FirstTokenTimeoutStatsRecorderHealth) bool {
		return health.PendingSamples == 0 && health.LastSuccessfulFlushAt != nil
	})
	health := recorder.Health()
	require.Equal(t, FirstTokenStatsCompletenessDegraded, health.Status)
	require.Equal(t, int64(3), health.DroppedSamples)
	require.Equal(t, fixedNow.UTC(), *health.LastSuccessfulFlushAt)
	require.Equal(t, 2, calls)
}

func TestFirstTokenStatsRecorderStartStopAreIdempotentAndStopDrains(t *testing.T) {
	var (
		mu      sync.Mutex
		batches [][]FirstTokenStatsDelta
	)
	repo := &firstTokenStatsRecorderRepoStub{
		upsertBatch: func(_ context.Context, deltas []FirstTokenStatsDelta) error {
			mu.Lock()
			defer mu.Unlock()
			batches = append(batches, append([]FirstTokenStatsDelta(nil), deltas...))
			return nil
		},
	}
	recorder := newFirstTokenTimeoutStatsRecorderWithConfig(repo, firstTokenTimeoutStatsRecorderConfig{
		queueCapacity:    4,
		flushUniqueKeys:  1000,
		flushInterval:    time.Hour,
		cleanupInterval:  time.Hour,
		operationTimeout: time.Second,
		now:              time.Now,
	})
	recorder.Start(context.Background())
	recorder.Start(context.Background())
	recorder.Record(validFirstTokenStatsRecorderDelta(42))
	recorder.Stop()
	recorder.Stop()

	mu.Lock()
	require.Len(t, batches, 1)
	require.Equal(t, int64(1), batches[0][0].SampleCount)
	mu.Unlock()
	require.Equal(t, int64(0), recorder.Health().PendingSamples)

	afterStop := validFirstTokenStatsRecorderDelta(43)
	afterStop.SampleCount = 2
	recorder.Record(afterStop)
	require.Equal(t, int64(2), recorder.Health().DroppedSamples)
}

func TestFirstTokenStatsRecorderContextCancellationDrainsAndStopsAccepting(t *testing.T) {
	flushed := make(chan []FirstTokenStatsDelta, 1)
	repo := &firstTokenStatsRecorderRepoStub{
		upsertBatch: func(_ context.Context, deltas []FirstTokenStatsDelta) error {
			flushed <- append([]FirstTokenStatsDelta(nil), deltas...)
			return nil
		},
	}
	recorder := newFirstTokenTimeoutStatsRecorderWithConfig(repo, firstTokenTimeoutStatsRecorderConfig{
		queueCapacity:    2,
		flushUniqueKeys:  1000,
		flushInterval:    time.Hour,
		cleanupInterval:  time.Hour,
		operationTimeout: time.Second,
		now:              time.Now,
	})
	ctx, cancel := context.WithCancel(context.Background())
	recorder.Start(ctx)
	recorder.Record(validFirstTokenStatsRecorderDelta(42))
	cancel()

	select {
	case batch := <-flushed:
		require.Len(t, batch, 1)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancellation flush")
	}
	select {
	case <-recorder.workerDone:
	case <-time.After(time.Second):
		t.Fatal("recorder worker did not exit after context cancellation")
	}

	recorder.Record(validFirstTokenStatsRecorderDelta(43))
	require.Equal(t, int64(1), recorder.Health().DroppedSamples)
}

func TestFirstTokenStatsRecorderStopReturnsWhenRepositoryIgnoresContext(t *testing.T) {
	flushStarted := make(chan struct{}, 1)
	flushContext := make(chan context.Context, 1)
	releaseFlush := make(chan struct{})
	repo := &firstTokenStatsRecorderRepoStub{
		upsertBatch: func(ctx context.Context, _ []FirstTokenStatsDelta) error {
			flushContext <- ctx
			flushStarted <- struct{}{}
			<-releaseFlush
			return nil
		},
	}
	recorder := newFirstTokenTimeoutStatsRecorderWithConfig(repo, firstTokenTimeoutStatsRecorderConfig{
		queueCapacity:    1,
		flushUniqueKeys:  1,
		flushInterval:    time.Hour,
		cleanupInterval:  time.Hour,
		operationTimeout: 30 * time.Millisecond,
		now:              time.Now,
	})
	recorder.Start(context.Background())
	recorder.Record(validFirstTokenStatsRecorderDelta(42))
	select {
	case <-flushStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for blocking repository")
	}

	stopReturned := make(chan struct{})
	go func() {
		recorder.Stop()
		close(stopReturned)
	}()
	select {
	case <-stopReturned:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Stop exceeded its configured upper bound")
	}

	ctx := <-flushContext
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("flush context did not reach its deadline")
	}
	close(releaseFlush)
	select {
	case <-recorder.workerDone:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit after blocking repository was released")
	}
	require.Equal(t, int64(1), recorder.Health().DroppedSamples)
	require.Equal(t, int64(0), recorder.Health().PendingSamples)
}

func TestFirstTokenStatsRecorderShutdownUsesOneBudgetAcrossAllRemainingBatches(t *testing.T) {
	const operationTimeout = 40 * time.Millisecond
	var calls atomic.Int64
	callStarted := make(chan time.Time, 4)
	repo := &firstTokenStatsRecorderRepoStub{
		upsertBatch: func(ctx context.Context, _ []FirstTokenStatsDelta) error {
			calls.Add(1)
			deadline, _ := ctx.Deadline()
			callStarted <- deadline
			<-ctx.Done()
			return ctx.Err()
		},
	}
	recorder := newFirstTokenTimeoutStatsRecorderWithConfig(repo, firstTokenTimeoutStatsRecorderConfig{
		queueCapacity:    3,
		flushUniqueKeys:  1,
		flushInterval:    time.Hour,
		cleanupInterval:  time.Hour,
		operationTimeout: operationTimeout,
		now:              time.Now,
	})
	recorder.Start(context.Background())
	recorder.Record(validFirstTokenStatsRecorderDelta(1))
	select {
	case <-callStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the first flush")
	}
	recorder.Record(validFirstTokenStatsRecorderDelta(2))
	recorder.Record(validFirstTokenStatsRecorderDelta(3))

	recorder.Stop()
	select {
	case <-recorder.workerDone:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit after shutdown deadline")
	}
	shutdownDeadline, ok := recorder.shutdownCtx.Deadline()
	require.True(t, ok)
	for {
		select {
		case deadline := <-callStarted:
			require.False(t, deadline.After(shutdownDeadline), "repository call exceeded the shared shutdown deadline")
		default:
			goto deadlinesChecked
		}
	}
deadlinesChecked:
	require.Positive(t, calls.Load())
	health := recorder.Health()
	require.Equal(t, int64(3), health.DroppedSamples)
	require.Equal(t, int64(0), health.PendingSamples)
}

func TestFirstTokenStatsRecorderIgnoringContextDoesNotHoldWorkerPastShutdownBudget(t *testing.T) {
	releaseRepo := make(chan struct{})
	repoStarted := make(chan struct{}, 1)
	repoDone := make(chan struct{}, 4)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseRepo) }) }
	t.Cleanup(release)
	repo := &firstTokenStatsRecorderRepoStub{
		upsertBatch: func(context.Context, []FirstTokenStatsDelta) error {
			select {
			case repoStarted <- struct{}{}:
			default:
			}
			defer func() { repoDone <- struct{}{} }()
			<-releaseRepo
			return nil
		},
	}
	recorder := newFirstTokenTimeoutStatsRecorderWithConfig(repo, firstTokenTimeoutStatsRecorderConfig{
		queueCapacity:    2,
		flushUniqueKeys:  1,
		flushInterval:    time.Hour,
		cleanupInterval:  time.Hour,
		operationTimeout: 30 * time.Millisecond,
		now:              time.Now,
	})
	recorder.Start(context.Background())
	recorder.Record(validFirstTokenStatsRecorderDelta(1))
	select {
	case <-repoStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the blocking repository")
	}
	recorder.Record(validFirstTokenStatsRecorderDelta(2))
	recorder.Stop()

	select {
	case <-recorder.workerDone:
	case <-time.After(time.Second):
		t.Fatal("repository that ignores context held the recorder worker")
	}
	health := recorder.Health()
	require.Equal(t, int64(2), health.DroppedSamples)
	require.Equal(t, int64(0), health.PendingSamples)

	release()
	select {
	case <-repoDone:
	case <-time.After(time.Second):
		t.Fatal("blocking repository goroutine did not exit after release")
	}
	require.Equal(t, health, recorder.Health())
}

func TestFirstTokenStatsRecorderIgnoringContextCleanupDoesNotHoldWorker(t *testing.T) {
	releaseRepo := make(chan struct{})
	cleanupStarted := make(chan struct{}, 1)
	cleanupDone := make(chan struct{}, 4)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseRepo) }) }
	t.Cleanup(release)
	repo := &firstTokenStatsRecorderRepoStub{
		deleteBefore: func(context.Context, time.Time) (int64, error) {
			select {
			case cleanupStarted <- struct{}{}:
			default:
			}
			defer func() { cleanupDone <- struct{}{} }()
			<-releaseRepo
			return 0, nil
		},
	}
	recorder := newFirstTokenTimeoutStatsRecorderWithConfig(repo, firstTokenTimeoutStatsRecorderConfig{
		queueCapacity:    1,
		flushUniqueKeys:  1,
		flushInterval:    time.Hour,
		cleanupInterval:  time.Millisecond,
		operationTimeout: 30 * time.Millisecond,
		now:              time.Now,
	})
	recorder.Start(context.Background())
	select {
	case <-cleanupStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for blocking cleanup")
	}
	recorder.Stop()

	select {
	case <-recorder.workerDone:
	case <-time.After(time.Second):
		t.Fatal("cleanup that ignores context held the recorder worker")
	}
	require.Equal(t, FirstTokenTimeoutStatsRecorderHealth{
		Status: FirstTokenStatsCompletenessComplete,
	}, recorder.Health())

	release()
	select {
	case <-cleanupDone:
	case <-time.After(time.Second):
		t.Fatal("blocking cleanup goroutine did not exit after release")
	}
}

func TestFirstTokenStatsRecorderStopBeforeStartPermanentlyRejectsRecords(t *testing.T) {
	recorder := NewFirstTokenTimeoutStatsRecorder(&firstTokenStatsRecorderRepoStub{})
	recorder.Stop()
	recorder.Start(context.Background())
	recorder.Record(validFirstTokenStatsRecorderDelta(42))
	require.Equal(t, int64(1), recorder.Health().DroppedSamples)
	select {
	case <-recorder.workerDone:
	default:
		t.Fatal("Stop before Start must complete the lifecycle")
	}
}

func TestFirstTokenStatsRecorderCleanupUsesUTCNinetyDayHourlyCutoffAndIsIdempotent(t *testing.T) {
	fixedNow := time.Date(2026, 7, 15, 8, 45, 0, 0, time.FixedZone("CST", 8*60*60))
	type cleanupCall struct {
		cutoff      time.Time
		hasDeadline bool
	}
	cleanupCalls := make(chan cleanupCall, 8)
	repo := &firstTokenStatsRecorderRepoStub{
		deleteBefore: func(ctx context.Context, cutoff time.Time) (int64, error) {
			_, hasDeadline := ctx.Deadline()
			cleanupCalls <- cleanupCall{cutoff: cutoff, hasDeadline: hasDeadline}
			return 0, errors.New("cleanup unavailable")
		},
	}
	recorder := newFirstTokenTimeoutStatsRecorderWithConfig(repo, firstTokenTimeoutStatsRecorderConfig{
		queueCapacity:    1,
		flushUniqueKeys:  1000,
		flushInterval:    time.Hour,
		cleanupInterval:  5 * time.Millisecond,
		operationTimeout: 50 * time.Millisecond,
		now:              func() time.Time { return fixedNow },
	})
	recorder.Start(context.Background())
	wantCutoff := fixedNow.UTC().Add(-90 * 24 * time.Hour).Truncate(time.Hour)
	for i := 0; i < 2; i++ {
		select {
		case call := <-cleanupCalls:
			require.Equal(t, wantCutoff, call.cutoff)
			require.True(t, call.hasDeadline)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for cleanup call")
		}
	}
	require.Equal(t, int64(0), recorder.Health().DroppedSamples)
	recorder.Stop()
	for {
		select {
		case <-cleanupCalls:
			continue
		default:
			goto drained
		}
	}
drained:
	select {
	case <-cleanupCalls:
		t.Fatal("cleanup ticker fired after Stop")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestFirstTokenStatsRecorderDefaultProductionConfig(t *testing.T) {
	recorder := NewFirstTokenTimeoutStatsRecorder(&firstTokenStatsRecorderRepoStub{})
	require.Equal(t, firstTokenStatsRecorderDefaultQueueCapacity, cap(recorder.queue))
	require.Equal(t, firstTokenStatsRecorderDefaultFlushUniqueKeys, recorder.config.flushUniqueKeys)
	require.Equal(t, firstTokenStatsRecorderDefaultFlushInterval, recorder.config.flushInterval)
	require.Equal(t, firstTokenStatsRecorderDefaultCleanupInterval, recorder.config.cleanupInterval)
	require.Equal(t, firstTokenStatsRecorderDefaultTimeout, recorder.config.operationTimeout)
}

func TestFirstTokenStatsRecorderFlushFailureLogsOnlySafeFields(t *testing.T) {
	handler := &firstTokenStatsRecorderLogHandler{records: make(chan slog.Record, 16)}
	logger := slog.New(handler)
	recorder := newFirstTokenTimeoutStatsRecorderWithConfig(&firstTokenStatsRecorderRepoStub{
		upsertBatch: func(context.Context, []FirstTokenStatsDelta) error {
			return errors.New("db unavailable")
		},
	}, firstTokenTimeoutStatsRecorderConfig{
		queueCapacity:    1,
		flushUniqueKeys:  1,
		flushInterval:    time.Hour,
		cleanupInterval:  time.Hour,
		operationTimeout: time.Second,
		now:              time.Now,
		logger:           logger,
	})
	recorder.Start(context.Background())
	defer recorder.Stop()
	delta := validFirstTokenStatsRecorderDelta(42)
	delta.SampleCount = 2
	recorder.Record(delta)
	waitForFirstTokenStatsRecorderHealth(t, recorder, func(health FirstTokenTimeoutStatsRecorderHealth) bool {
		return health.PendingSamples == 0
	})

	record := waitForFirstTokenStatsRecorderLog(t, handler.records)
	require.Equal(t, map[string]any{
		"operation":    "flush",
		"sample_count": int64(2),
		"error":        "db unavailable",
	}, firstTokenStatsRecorderLogAttrs(record))
}

func TestFirstTokenStatsRecorderCleanupFailureLogsOnlySafeFields(t *testing.T) {
	handler := &firstTokenStatsRecorderLogHandler{records: make(chan slog.Record, 16)}
	logger := slog.New(handler)
	recorder := newFirstTokenTimeoutStatsRecorderWithConfig(&firstTokenStatsRecorderRepoStub{
		deleteBefore: func(context.Context, time.Time) (int64, error) {
			return 0, errors.New("cleanup unavailable")
		},
	}, firstTokenTimeoutStatsRecorderConfig{
		queueCapacity:    1,
		flushUniqueKeys:  1,
		flushInterval:    time.Hour,
		cleanupInterval:  time.Millisecond,
		operationTimeout: time.Second,
		now:              time.Now,
		logger:           logger,
	})
	recorder.Start(context.Background())
	defer recorder.Stop()

	record := waitForFirstTokenStatsRecorderLog(t, handler.records)
	require.Equal(t, map[string]any{
		"operation": "cleanup",
		"error":     "cleanup unavailable",
	}, firstTokenStatsRecorderLogAttrs(record))
}

func TestFirstTokenStatsRecorderHealthFailureTransitionIsLinear(t *testing.T) {
	recorder := NewFirstTokenTimeoutStatsRecorder(&firstTokenStatsRecorderRepoStub{})
	for i := 0; i < 2000; i++ {
		recorder.health.Store(&firstTokenTimeoutStatsRecorderHealthState{pendingSamples: 1})
		start := make(chan struct{})
		healthResult := make(chan FirstTokenTimeoutStatsRecorderHealth, 1)
		transitionDone := make(chan struct{})
		go func() {
			<-start
			healthResult <- recorder.Health()
		}()
		go func() {
			<-start
			recorder.transitionFlushFailure(1)
			close(transitionDone)
		}()
		close(start)
		health := <-healthResult
		<-transitionDone
		before := health.Status == FirstTokenStatsCompletenessComplete &&
			health.DroppedSamples == 0 && health.PendingSamples == 1
		after := health.Status == FirstTokenStatsCompletenessDegraded &&
			health.DroppedSamples == 1 && health.PendingSamples == 0
		if !before && !after {
			t.Fatalf("observed non-linear failure health snapshot: %+v", health)
		}
	}
}

func TestFirstTokenStatsRecorderHealthSuccessTransitionIsLinear(t *testing.T) {
	recorder := NewFirstTokenTimeoutStatsRecorder(&firstTokenStatsRecorderRepoStub{})
	oldFlush := time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)
	newFlush := oldFlush.Add(time.Hour)
	for i := 0; i < 2000; i++ {
		recorder.health.Store(&firstTokenTimeoutStatsRecorderHealthState{
			pendingSamples:         1,
			lastSuccessfulFlushAt:  oldFlush,
			hasLastSuccessfulFlush: true,
		})
		start := make(chan struct{})
		healthResult := make(chan FirstTokenTimeoutStatsRecorderHealth, 1)
		transitionDone := make(chan struct{})
		go func() {
			<-start
			healthResult <- recorder.Health()
		}()
		go func() {
			<-start
			recorder.transitionFlushSuccess(1, newFlush)
			close(transitionDone)
		}()
		close(start)
		health := <-healthResult
		<-transitionDone
		require.NotNil(t, health.LastSuccessfulFlushAt)
		before := health.PendingSamples == 1 && health.LastSuccessfulFlushAt.Equal(oldFlush)
		after := health.PendingSamples == 0 && health.LastSuccessfulFlushAt.Equal(newFlush)
		if !before && !after {
			t.Fatalf("observed non-linear success health snapshot: %+v", health)
		}
	}
}

func TestFirstTokenStatsRecorderRecordRacesWithStopConservesSamples(t *testing.T) {
	testFirstTokenStatsRecorderRecordShutdownRace(t, func(recorder *FirstTokenTimeoutStatsRecorder, _ context.CancelFunc) {
		recorder.Stop()
	})
}

func TestFirstTokenStatsRecorderRecordRacesWithContextCancelConservesSamples(t *testing.T) {
	testFirstTokenStatsRecorderRecordShutdownRace(t, func(_ *FirstTokenTimeoutStatsRecorder, cancel context.CancelFunc) {
		cancel()
	})
}

func testFirstTokenStatsRecorderRecordShutdownRace(
	t *testing.T,
	shutdown func(*FirstTokenTimeoutStatsRecorder, context.CancelFunc),
) {
	t.Helper()
	const attempted = 128
	var persisted atomic.Int64
	repo := &firstTokenStatsRecorderRepoStub{
		upsertBatch: func(_ context.Context, deltas []FirstTokenStatsDelta) error {
			for _, delta := range deltas {
				persisted.Add(delta.SampleCount)
			}
			return nil
		},
	}
	recorder := newFirstTokenTimeoutStatsRecorderWithConfig(repo, firstTokenTimeoutStatsRecorderConfig{
		queueCapacity:    attempted,
		flushUniqueKeys:  16,
		flushInterval:    time.Hour,
		cleanupInterval:  time.Hour,
		operationTimeout: time.Second,
		now:              time.Now,
	})
	ctx, cancel := context.WithCancel(context.Background())
	recorder.Start(ctx)
	start := make(chan struct{})
	var records sync.WaitGroup
	for i := 0; i < attempted; i++ {
		records.Add(1)
		go func(accountID int64) {
			defer records.Done()
			<-start
			recorder.Record(validFirstTokenStatsRecorderDelta(accountID))
		}(int64(i + 1))
	}
	shutdownDone := make(chan struct{})
	go func() {
		<-start
		shutdown(recorder, cancel)
		close(shutdownDone)
	}()
	close(start)
	records.Wait()
	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not return")
	}
	select {
	case <-recorder.workerDone:
	case <-time.After(time.Second):
		t.Fatal("recorder worker did not exit")
	}
	health := recorder.Health()
	require.Equal(t, int64(attempted), persisted.Load()+health.DroppedSamples)
	require.Equal(t, int64(0), health.PendingSamples)
}

func waitForFirstTokenStatsRecorderHealth(
	t *testing.T,
	recorder *FirstTokenTimeoutStatsRecorder,
	condition func(FirstTokenTimeoutStatsRecorderHealth) bool,
) FirstTokenTimeoutStatsRecorderHealth {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		health := recorder.Health()
		if condition(health) {
			return health
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatalf("timed out waiting for recorder health condition; last=%+v", health)
			return health
		}
	}
}

func waitForFirstTokenStatsRecorderLog(t *testing.T, records <-chan slog.Record) slog.Record {
	t.Helper()
	select {
	case record := <-records:
		return record
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for recorder log")
		return slog.Record{}
	}
}

func firstTokenStatsRecorderLogAttrs(record slog.Record) map[string]any {
	attrs := make(map[string]any, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		value := attr.Value.Any()
		if err, ok := value.(error); ok {
			value = err.Error()
		}
		attrs[attr.Key] = value
		return true
	})
	return attrs
}

func validFirstTokenStatsRecorderDelta(accountID int64) FirstTokenStatsDelta {
	return FirstTokenStatsDelta{
		BucketStart:    time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC),
		Scope:          FirstTokenStatsScopeAttempt,
		AccountID:      accountID,
		Protocol:       "openai_responses",
		Platform:       "openai",
		Model:          "gpt-5.2",
		TimeoutSeconds: 30,
		Outcome:        FirstTokenStatsAttemptSuccess,
		SampleCount:    1,
	}
}
