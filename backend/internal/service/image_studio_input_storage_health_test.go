package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestImageStudioInputStorageHealthTracksTransitionsWithoutSensitiveErrors(t *testing.T) {
	probeErr := errors.New("open /private/mount/secret/probe: permission denied")
	prober := &imageStudioInputStorageProberStub{errors: []error{probeErr, probeErr, nil}}
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	health := NewImageStudioInputStorageHealth(prober, time.Minute)
	health.now = func() time.Time { return now }
	var transitions []ImageStudioInputStorageHealthSnapshot
	health.observeTransition = func(snapshot ImageStudioInputStorageHealthSnapshot) {
		transitions = append(transitions, snapshot)
	}

	require.False(t, health.Available())
	require.ErrorIs(t, health.Probe(context.Background()), ErrImageStudioInputStorageUnavailable)
	first := health.Snapshot()
	require.True(t, first.Initialized)
	require.False(t, first.Healthy)
	require.Equal(t, ImageStudioInputCodeStorageUnavailable, first.LastErrorKind)
	require.Equal(t, now, first.LastTransition)
	require.NotContains(t, first.LastErrorKind, "/private")
	require.Len(t, transitions, 1)

	now = now.Add(time.Minute)
	require.ErrorIs(t, health.Probe(context.Background()), ErrImageStudioInputStorageUnavailable)
	require.Len(t, transitions, 1, "same unhealthy state must not log every tick")
	require.Equal(t, first.LastTransition, health.Snapshot().LastTransition)

	now = now.Add(time.Minute)
	require.NoError(t, health.Probe(context.Background()))
	recovered := health.Snapshot()
	require.True(t, recovered.Healthy)
	require.Equal(t, now, recovered.LastTransition)
	require.Equal(t, ImageStudioInputCodeStorageUnavailable, recovered.LastErrorKind)
	require.Len(t, transitions, 2)
}

func TestImageStudioInputStorageHealthSingleflightsConcurrentProbes(t *testing.T) {
	start := make(chan struct{})
	prober := &imageStudioInputStorageProberStub{probe: func(context.Context) error {
		time.Sleep(80 * time.Millisecond)
		return nil
	}}
	health := NewImageStudioInputStorageHealth(prober, time.Minute)

	errs := make(chan error, 8)
	for i := 0; i < cap(errs); i++ {
		go func() {
			<-start
			errs <- health.Probe(context.Background())
		}()
	}
	close(start)
	for i := 0; i < cap(errs); i++ {
		require.NoError(t, <-errs)
	}
	require.Equal(t, 1, prober.Calls())
	require.True(t, health.Available())
}

func TestImageStudioInputStorageHealthRunUsesInjectedTicker(t *testing.T) {
	prober := &imageStudioInputStorageProberStub{}
	health := NewImageStudioInputStorageHealth(prober, 17*time.Second)
	ticker := &imageStudioInputStorageHealthTickerStub{ticks: make(chan time.Time, 2)}
	health.newTicker = func(interval time.Duration) imageStudioInputStorageHealthTicker {
		require.Equal(t, 17*time.Second, interval)
		return ticker
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		health.Run(ctx)
		close(done)
	}()

	ticker.ticks <- time.Now()
	require.Eventually(t, func() bool { return prober.Calls() == 1 }, time.Second, time.Millisecond)
	ticker.ticks <- time.Now()
	require.Eventually(t, func() bool { return prober.Calls() == 2 }, time.Second, time.Millisecond)
	cancel()
	<-done
	require.True(t, ticker.Stopped())
}

func TestImageStudioInputStorageHealthRunCancelsInFlightProbe(t *testing.T) {
	entered := make(chan struct{})
	prober := &imageStudioInputStorageProberStub{probe: func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}}
	health := NewImageStudioInputStorageHealth(prober, time.Minute)
	ticker := &imageStudioInputStorageHealthTickerStub{ticks: make(chan time.Time, 1)}
	health.newTicker = func(time.Duration) imageStudioInputStorageHealthTicker { return ticker }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		health.Run(ctx)
		close(done)
	}()
	ticker.ticks <- time.Now()
	<-entered

	cancel()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("health loop did not cancel its in-flight probe")
	}
	require.True(t, ticker.Stopped())
}

func TestImageStudioInputStorageHealthProbeHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prober := &imageStudioInputStorageProberStub{}
	health := NewImageStudioInputStorageHealth(prober, time.Minute)

	err := health.Probe(ctx)

	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, ErrImageStudioInputStorageUnavailable)
	require.Zero(t, prober.Calls())
	require.False(t, health.Available())
}

type imageStudioInputStorageProberStub struct {
	mu     sync.Mutex
	errors []error
	probe  func(context.Context) error
	calls  int
}

func (s *imageStudioInputStorageProberStub) Probe(ctx context.Context) error {
	s.mu.Lock()
	s.calls++
	call := s.calls
	probe := s.probe
	var err error
	if call <= len(s.errors) {
		err = s.errors[call-1]
	}
	s.mu.Unlock()
	if probe != nil {
		return probe(ctx)
	}
	return err
}

func (s *imageStudioInputStorageProberStub) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type imageStudioInputStorageHealthTickerStub struct {
	ticks   chan time.Time
	mu      sync.Mutex
	stopped bool
}

func (t *imageStudioInputStorageHealthTickerStub) C() <-chan time.Time { return t.ticks }

func (t *imageStudioInputStorageHealthTickerStub) Stop() {
	t.mu.Lock()
	t.stopped = true
	t.mu.Unlock()
}

func (t *imageStudioInputStorageHealthTickerStub) Stopped() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stopped
}
