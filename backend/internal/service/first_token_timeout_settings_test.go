package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseFirstTokenTimeoutSettingsFallbackAndValidation(t *testing.T) {
	require.Equal(t, FirstTokenTimeoutSettings{Enabled: false, TimeoutSeconds: 30},
		parseFirstTokenTimeoutSettings(`{"enabled":true,"timeout_seconds":0}`))

	tests := []struct {
		name    string
		enabled bool
		seconds int
		wantErr bool
	}{
		{name: "negative", enabled: true, seconds: -1, wantErr: true},
		{name: "zero", enabled: false, seconds: 0, wantErr: true},
		{name: "minimum", enabled: true, seconds: 1},
		{name: "maximum disabled", enabled: false, seconds: 300},
		{name: "above maximum", enabled: true, seconds: 301, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFirstTokenTimeoutSettings(FirstTokenTimeoutSettings{
				Enabled:        tt.enabled,
				TimeoutSeconds: tt.seconds,
			})
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestFirstTokenTimeoutPolicyUpdatePublishesImmutableSnapshot(t *testing.T) {
	policy, repo, notifier := newFirstTokenTimeoutPolicyForTest()

	require.NoError(t, policy.Update(context.Background(), FirstTokenTimeoutSettings{Enabled: true, TimeoutSeconds: 12}))

	snap := policy.Snapshot()
	require.True(t, snap.Enabled)
	require.Equal(t, 12*time.Second, snap.Timeout)
	require.False(t, snap.LoadedAt.IsZero())
	require.Equal(t, `{"enabled":true,"timeout_seconds":12}`, repo.value(SettingKeyFirstTokenTimeoutSettings))
	require.Equal(t, 1, notifier.publishCount())
}

func TestFirstTokenTimeoutPolicyReloadFallsBackForMissingAndCorruptSettings(t *testing.T) {
	policy, repo, _ := newFirstTokenTimeoutPolicyForTest()

	require.NoError(t, policy.Reload(context.Background()))
	require.Equal(t, 30*time.Second, policy.Snapshot().Timeout)
	require.False(t, policy.Snapshot().Enabled)

	require.NoError(t, repo.Set(context.Background(), SettingKeyFirstTokenTimeoutSettings, `{"enabled":true,"timeout_seconds":`))
	require.NoError(t, policy.Reload(context.Background()))
	require.Equal(t, 30*time.Second, policy.Snapshot().Timeout)
	require.False(t, policy.Snapshot().Enabled)
}

func TestFirstTokenTimeoutPolicyInvalidUpdateKeepsPreviousSnapshot(t *testing.T) {
	policy, repo, notifier := newFirstTokenTimeoutPolicyForTest()
	require.NoError(t, policy.Update(context.Background(), FirstTokenTimeoutSettings{Enabled: true, TimeoutSeconds: 12}))
	before := policy.Snapshot()

	require.Error(t, policy.Update(context.Background(), FirstTokenTimeoutSettings{Enabled: false, TimeoutSeconds: 301}))

	require.Equal(t, before, policy.Snapshot())
	require.Equal(t, `{"enabled":true,"timeout_seconds":12}`, repo.value(SettingKeyFirstTokenTimeoutSettings))
	require.Equal(t, 1, notifier.publishCount())
}

func TestFirstTokenTimeoutPolicyPublishFailureDoesNotRejectSavedUpdate(t *testing.T) {
	policy, repo, notifier := newFirstTokenTimeoutPolicyForTest()
	notifier.publishErr = errors.New("redis unavailable")

	require.NoError(t, policy.Update(context.Background(), FirstTokenTimeoutSettings{Enabled: true, TimeoutSeconds: 24}))

	require.Equal(t, `{"enabled":true,"timeout_seconds":24}`, repo.value(SettingKeyFirstTokenTimeoutSettings))
	require.Equal(t, 24*time.Second, policy.Snapshot().Timeout)
	require.Equal(t, 1, notifier.publishCount())
}

func TestFirstTokenTimeoutPolicySetFailureKeepsSnapshotAndSkipsPublish(t *testing.T) {
	policy, repo, notifier := newFirstTokenTimeoutPolicyForTest()
	before := policy.Snapshot()
	repo.setErr = errors.New("database unavailable")

	require.Error(t, policy.Update(context.Background(), FirstTokenTimeoutSettings{Enabled: true, TimeoutSeconds: 24}))

	require.Equal(t, before, policy.Snapshot())
	require.Equal(t, 0, notifier.publishCount())
}

func TestFirstTokenTimeoutPolicySerializesConcurrentUpdates(t *testing.T) {
	base := &firstTokenTimeoutMemorySettingRepo{values: make(map[string]string)}
	firstSetEntered := make(chan struct{})
	releaseFirstSet := make(chan struct{})
	repo := &firstTokenTimeoutBlockingSettingRepo{
		firstTokenTimeoutMemorySettingRepo: base,
		set: func(ctx context.Context, key, value string) error {
			if value == `{"enabled":true,"timeout_seconds":11}` {
				if err := base.Set(ctx, key, value); err != nil {
					return err
				}
				close(firstSetEntered)
				<-releaseFirstSet
				return nil
			}
			return base.Set(ctx, key, value)
		},
	}
	policy := NewFirstTokenTimeoutPolicy(repo, nil)
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	go func() {
		firstDone <- policy.Update(context.Background(), FirstTokenTimeoutSettings{Enabled: true, TimeoutSeconds: 11})
	}()
	<-firstSetEntered
	go func() {
		secondDone <- policy.Update(context.Background(), FirstTokenTimeoutSettings{Enabled: true, TimeoutSeconds: 22})
	}()

	select {
	case err := <-secondDone:
		require.NoError(t, err)
		close(releaseFirstSet)
	case <-time.After(50 * time.Millisecond):
		close(releaseFirstSet)
		require.NoError(t, <-secondDone)
	}
	require.NoError(t, <-firstDone)
	require.Equal(t, `{"enabled":true,"timeout_seconds":22}`, base.value(SettingKeyFirstTokenTimeoutSettings))
	require.Equal(t, 22*time.Second, policy.Snapshot().Timeout)
}

func TestFirstTokenTimeoutPolicySerializesReloadWithUpdate(t *testing.T) {
	base := &firstTokenTimeoutMemorySettingRepo{values: map[string]string{
		SettingKeyFirstTokenTimeoutSettings: `{"enabled":true,"timeout_seconds":11}`,
	}}
	reloadRead := make(chan struct{})
	releaseReload := make(chan struct{})
	repo := &firstTokenTimeoutBlockingSettingRepo{
		firstTokenTimeoutMemorySettingRepo: base,
		getValue: func(ctx context.Context, key string) (string, error) {
			value, err := base.GetValue(ctx, key)
			close(reloadRead)
			<-releaseReload
			return value, err
		},
	}
	policy := NewFirstTokenTimeoutPolicy(repo, nil)
	reloadDone := make(chan error, 1)
	updateDone := make(chan error, 1)
	go func() { reloadDone <- policy.Reload(context.Background()) }()
	<-reloadRead
	go func() {
		updateDone <- policy.Update(context.Background(), FirstTokenTimeoutSettings{Enabled: true, TimeoutSeconds: 22})
	}()

	select {
	case err := <-updateDone:
		require.NoError(t, err)
		close(releaseReload)
	case <-time.After(50 * time.Millisecond):
		close(releaseReload)
		require.NoError(t, <-updateDone)
	}
	require.NoError(t, <-reloadDone)
	require.Equal(t, `{"enabled":true,"timeout_seconds":22}`, base.value(SettingKeyFirstTokenTimeoutSettings))
	require.Equal(t, 22*time.Second, policy.Snapshot().Timeout)
}

func TestFirstTokenTimeoutPolicyInvalidationReloadsAnotherInstance(t *testing.T) {
	repo := &firstTokenTimeoutMemorySettingRepo{values: make(map[string]string)}
	notifier := &firstTokenTimeoutTestNotifier{events: make(chan struct{}, 8)}
	writer := NewFirstTokenTimeoutPolicy(repo, notifier)
	reader := NewFirstTokenTimeoutPolicy(repo, notifier)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	reader.Start(ctx)

	require.NoError(t, writer.Update(context.Background(), FirstTokenTimeoutSettings{Enabled: true, TimeoutSeconds: 19}))
	require.Eventually(t, func() bool {
		snapshot := reader.Snapshot()
		return snapshot.Enabled && snapshot.Timeout == 19*time.Second
	}, time.Second, 10*time.Millisecond)
}

func TestFirstTokenTimeoutPolicyStartSubscribesBeforeInitialReload(t *testing.T) {
	base := &firstTokenTimeoutMemorySettingRepo{values: map[string]string{
		SettingKeyFirstTokenTimeoutSettings: `{"enabled":true,"timeout_seconds":11}`,
	}}
	getEntered := make(chan struct{})
	releaseGet := make(chan struct{})
	var getOnce sync.Once
	repo := &firstTokenTimeoutBlockingSettingRepo{
		firstTokenTimeoutMemorySettingRepo: base,
		getValue: func(ctx context.Context, key string) (string, error) {
			value, err := base.GetValue(ctx, key)
			getOnce.Do(func() {
				close(getEntered)
				<-releaseGet
			})
			return value, err
		},
	}
	notifier := newFirstTokenTimeoutWindowNotifier()
	policy := NewFirstTokenTimeoutPolicy(repo, notifier)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	t.Cleanup(func() {
		select {
		case <-releaseGet:
		default:
			close(releaseGet)
		}
	})
	go policy.Start(ctx)
	<-getEntered

	select {
	case <-notifier.subscribed:
	default:
		t.Fatal("policy started its initial reload before subscribing")
	}
}

func TestFirstTokenTimeoutPolicyStartDoesNotLoseInvalidationDuringInitialReload(t *testing.T) {
	base := &firstTokenTimeoutMemorySettingRepo{values: map[string]string{
		SettingKeyFirstTokenTimeoutSettings: `{"enabled":true,"timeout_seconds":11}`,
	}}
	getEntered := make(chan struct{})
	releaseGet := make(chan struct{})
	var getOnce sync.Once
	repo := &firstTokenTimeoutBlockingSettingRepo{
		firstTokenTimeoutMemorySettingRepo: base,
		getValue: func(ctx context.Context, key string) (string, error) {
			value, err := base.GetValue(ctx, key)
			getOnce.Do(func() {
				close(getEntered)
				<-releaseGet
			})
			return value, err
		},
	}
	notifier := newFirstTokenTimeoutWindowNotifier()
	policy := NewFirstTokenTimeoutPolicy(repo, notifier)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go policy.Start(ctx)
	<-getEntered
	require.NoError(t, base.Set(context.Background(), SettingKeyFirstTokenTimeoutSettings, `{"enabled":true,"timeout_seconds":22}`))
	require.NoError(t, notifier.Publish(context.Background()))
	close(releaseGet)

	require.Eventually(t, func() bool {
		return policy.Snapshot().Timeout == 22*time.Second
	}, 250*time.Millisecond, 5*time.Millisecond)
}

func TestFirstTokenTimeoutPolicyStartIsIdempotent(t *testing.T) {
	base := &firstTokenTimeoutMemorySettingRepo{values: make(map[string]string)}
	repo := newFirstTokenTimeoutCountingRepo(base)
	events := make(chan struct{})
	releaseSubscribe := make(chan struct{})
	notifier := newFirstTokenTimeoutLifecycleNotifier(firstTokenTimeoutSubscriptionResult{
		events: events,
		block:  releaseSubscribe,
	})
	policy := NewFirstTokenTimeoutPolicy(repo, notifier)
	ticker := newFirstTokenTimeoutManualTicker()
	policy.tickerFactory = func(time.Duration) firstTokenTimeoutPolicyTicker { return ticker }
	ctx, cancel := context.WithCancel(context.Background())

	policy.Start(ctx)
	policy.Start(ctx)
	require.Equal(t, 1, waitForFirstTokenTimeoutSubscribeCall(t, notifier))
	select {
	case call := <-notifier.subscribeCalls:
		t.Fatalf("duplicate Start created subscription call %d", call)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseSubscribe)
	require.Equal(t, 1, waitForFirstTokenTimeoutReload(t, repo))
	cancel()
	waitForFirstTokenTimeoutWorkerExit(t, policy)
}

func TestFirstTokenTimeoutPolicyRetriesSubscriptionAndReloadsAfterReconnect(t *testing.T) {
	base := &firstTokenTimeoutMemorySettingRepo{values: map[string]string{
		SettingKeyFirstTokenTimeoutSettings: `{"enabled":true,"timeout_seconds":11}`,
	}}
	repo := newFirstTokenTimeoutCountingRepo(base)
	firstEvents := make(chan struct{})
	secondEvents := make(chan struct{})
	releaseReconnect := make(chan struct{})
	notifier := newFirstTokenTimeoutLifecycleNotifier(
		firstTokenTimeoutSubscriptionResult{err: errors.New("redis unavailable")},
		firstTokenTimeoutSubscriptionResult{events: firstEvents},
		firstTokenTimeoutSubscriptionResult{events: secondEvents, block: releaseReconnect},
	)
	policy := NewFirstTokenTimeoutPolicy(repo, notifier)
	ticker := newFirstTokenTimeoutManualTicker()
	policy.tickerFactory = func(time.Duration) firstTokenTimeoutPolicyTicker { return ticker }
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	policy.Start(ctx)
	require.Equal(t, 1, waitForFirstTokenTimeoutSubscribeCall(t, notifier))
	require.Equal(t, 1, waitForFirstTokenTimeoutReload(t, repo))
	ticker.Tick()
	require.Equal(t, 2, waitForFirstTokenTimeoutSubscribeCall(t, notifier))
	require.Equal(t, 2, waitForFirstTokenTimeoutReload(t, repo))

	close(firstEvents)
	reconnected := false
	for notifier.subscribeCount() < 3 {
		ticker.Tick()
		select {
		case call := <-notifier.subscribeCalls:
			require.Equal(t, 3, call)
			reconnected = true
		case <-repo.reloadCalls:
		case <-time.After(time.Second):
			t.Fatal("policy did not retry after subscription channel closed")
		}
	}
	if !reconnected {
		require.Equal(t, 3, waitForFirstTokenTimeoutSubscribeCall(t, notifier))
	}
	reloadsBeforeReconnect := repo.reloadCount()
	close(releaseReconnect)
	require.Equal(t, int(reloadsBeforeReconnect+1), waitForFirstTokenTimeoutReload(t, repo))
	cancel()
	waitForFirstTokenTimeoutWorkerExit(t, policy)
}

func TestFirstTokenTimeoutPolicyPeriodicReloadConvergesWithoutInvalidation(t *testing.T) {
	base := &firstTokenTimeoutMemorySettingRepo{values: map[string]string{
		SettingKeyFirstTokenTimeoutSettings: `{"enabled":true,"timeout_seconds":11}`,
	}}
	repo := newFirstTokenTimeoutCountingRepo(base)
	notifier := newFirstTokenTimeoutLifecycleNotifier(firstTokenTimeoutSubscriptionResult{events: make(chan struct{})})
	policy := NewFirstTokenTimeoutPolicy(repo, notifier)
	ticker := newFirstTokenTimeoutManualTicker()
	policy.tickerFactory = func(time.Duration) firstTokenTimeoutPolicyTicker { return ticker }
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	policy.Start(ctx)
	require.Equal(t, 1, waitForFirstTokenTimeoutSubscribeCall(t, notifier))
	require.Equal(t, 1, waitForFirstTokenTimeoutReload(t, repo))
	require.Eventually(t, func() bool {
		return policy.Snapshot().Timeout == 11*time.Second
	}, time.Second, time.Millisecond)
	require.NoError(t, base.Set(context.Background(), SettingKeyFirstTokenTimeoutSettings, `{"enabled":true,"timeout_seconds":22}`))

	ticker.Tick()
	require.Equal(t, 2, waitForFirstTokenTimeoutReload(t, repo))
	require.Eventually(t, func() bool {
		return policy.Snapshot().Timeout == 22*time.Second
	}, time.Second, time.Millisecond)
	cancel()
	waitForFirstTokenTimeoutWorkerExit(t, policy)
}

func TestFirstTokenTimeoutPolicyWorkerStopsOnContextCancellation(t *testing.T) {
	base := &firstTokenTimeoutMemorySettingRepo{values: make(map[string]string)}
	repo := newFirstTokenTimeoutCountingRepo(base)
	notifier := newFirstTokenTimeoutLifecycleNotifier(firstTokenTimeoutSubscriptionResult{events: make(chan struct{})})
	policy := NewFirstTokenTimeoutPolicy(repo, notifier)
	ticker := newFirstTokenTimeoutManualTicker()
	policy.tickerFactory = func(time.Duration) firstTokenTimeoutPolicyTicker { return ticker }
	ctx, cancel := context.WithCancel(context.Background())

	policy.Start(ctx)
	require.Equal(t, 1, waitForFirstTokenTimeoutSubscribeCall(t, notifier))
	require.Equal(t, 1, waitForFirstTokenTimeoutReload(t, repo))
	cancel()
	waitForFirstTokenTimeoutWorkerExit(t, policy)
	select {
	case <-ticker.stopped:
	case <-time.After(time.Second):
		t.Fatal("policy ticker was not stopped")
	}
	require.Equal(t, int32(1), repo.reloadCount())
}

func TestFirstTokenTimeoutPolicySnapshotSupportsConcurrentReadsAndUpdates(t *testing.T) {
	policy, _, _ := newFirstTokenTimeoutPolicyForTest()
	const iterations = 200

	var wg sync.WaitGroup
	errs := make(chan error, 5)
	wg.Add(5)
	for reader := 0; reader < 4; reader++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				snapshot := policy.Snapshot()
				if snapshot.LoadedAt.IsZero() || snapshot.Timeout < time.Second || snapshot.Timeout > 300*time.Second {
					errs <- fmt.Errorf("invalid snapshot: %+v", snapshot)
					return
				}
			}
		}()
	}
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if err := policy.Update(context.Background(), FirstTokenTimeoutSettings{
				Enabled:        i%2 == 0,
				TimeoutSeconds: i%300 + 1,
			}); err != nil {
				errs <- err
				return
			}
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

func newFirstTokenTimeoutPolicyForTest() (*FirstTokenTimeoutPolicy, *firstTokenTimeoutMemorySettingRepo, *firstTokenTimeoutTestNotifier) {
	repo := &firstTokenTimeoutMemorySettingRepo{values: make(map[string]string)}
	notifier := &firstTokenTimeoutTestNotifier{events: make(chan struct{}, 8)}
	return NewFirstTokenTimeoutPolicy(repo, notifier), repo, notifier
}

type firstTokenTimeoutMemorySettingRepo struct {
	mu     sync.RWMutex
	values map[string]string
	setErr error
}

func (r *firstTokenTimeoutMemorySettingRepo) Get(_ context.Context, key string) (*Setting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.values[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: value}, nil
}

func (r *firstTokenTimeoutMemorySettingRepo) GetValue(_ context.Context, key string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *firstTokenTimeoutMemorySettingRepo) Set(_ context.Context, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.setErr != nil {
		return r.setErr
	}
	r.values[key] = value
	return nil
}

func (r *firstTokenTimeoutMemorySettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (r *firstTokenTimeoutMemorySettingRepo) SetMultiple(ctx context.Context, values map[string]string) error {
	for key, value := range values {
		if err := r.Set(ctx, key, value); err != nil {
			return err
		}
	}
	return nil
}

func (r *firstTokenTimeoutMemorySettingRepo) GetAll(_ context.Context) (map[string]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := make(map[string]string, len(r.values))
	for key, value := range r.values {
		values[key] = value
	}
	return values, nil
}

func (r *firstTokenTimeoutMemorySettingRepo) Delete(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.values, key)
	return nil
}

func (r *firstTokenTimeoutMemorySettingRepo) value(key string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.values[key]
}

type firstTokenTimeoutBlockingSettingRepo struct {
	*firstTokenTimeoutMemorySettingRepo
	set      func(context.Context, string, string) error
	getValue func(context.Context, string) (string, error)
}

type firstTokenTimeoutCountingRepo struct {
	*firstTokenTimeoutMemorySettingRepo
	reloads     atomic.Int32
	reloadCalls chan int
}

func newFirstTokenTimeoutCountingRepo(base *firstTokenTimeoutMemorySettingRepo) *firstTokenTimeoutCountingRepo {
	return &firstTokenTimeoutCountingRepo{
		firstTokenTimeoutMemorySettingRepo: base,
		reloadCalls:                        make(chan int, 16),
	}
}

func (r *firstTokenTimeoutCountingRepo) GetValue(ctx context.Context, key string) (string, error) {
	call := int(r.reloads.Add(1))
	value, err := r.firstTokenTimeoutMemorySettingRepo.GetValue(ctx, key)
	r.reloadCalls <- call
	return value, err
}

func (r *firstTokenTimeoutCountingRepo) reloadCount() int32 {
	return r.reloads.Load()
}

func (r *firstTokenTimeoutBlockingSettingRepo) Set(ctx context.Context, key, value string) error {
	if r.set != nil {
		return r.set(ctx, key, value)
	}
	return r.firstTokenTimeoutMemorySettingRepo.Set(ctx, key, value)
}

func (r *firstTokenTimeoutBlockingSettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	if r.getValue != nil {
		return r.getValue(ctx, key)
	}
	return r.firstTokenTimeoutMemorySettingRepo.GetValue(ctx, key)
}

type firstTokenTimeoutTestNotifier struct {
	mu         sync.Mutex
	published  int
	events     chan struct{}
	publishErr error
}

type firstTokenTimeoutWindowNotifier struct {
	subscribed chan struct{}
	events     chan struct{}
	once       sync.Once
}

func newFirstTokenTimeoutWindowNotifier() *firstTokenTimeoutWindowNotifier {
	return &firstTokenTimeoutWindowNotifier{
		subscribed: make(chan struct{}),
		events:     make(chan struct{}, 1),
	}
}

func (n *firstTokenTimeoutWindowNotifier) Publish(context.Context) error {
	select {
	case <-n.subscribed:
		select {
		case n.events <- struct{}{}:
		default:
		}
	default:
	}
	return nil
}

func (n *firstTokenTimeoutWindowNotifier) Subscribe(context.Context) (<-chan struct{}, error) {
	n.once.Do(func() { close(n.subscribed) })
	return n.events, nil
}

type firstTokenTimeoutSubscriptionResult struct {
	events <-chan struct{}
	err    error
	block  <-chan struct{}
}

type firstTokenTimeoutLifecycleNotifier struct {
	mu             sync.Mutex
	results        []firstTokenTimeoutSubscriptionResult
	subscriptions  int
	subscribeCalls chan int
}

func newFirstTokenTimeoutLifecycleNotifier(results ...firstTokenTimeoutSubscriptionResult) *firstTokenTimeoutLifecycleNotifier {
	return &firstTokenTimeoutLifecycleNotifier{
		results:        results,
		subscribeCalls: make(chan int, 16),
	}
}

func (n *firstTokenTimeoutLifecycleNotifier) Publish(context.Context) error {
	return nil
}

func (n *firstTokenTimeoutLifecycleNotifier) Subscribe(context.Context) (<-chan struct{}, error) {
	n.mu.Lock()
	n.subscriptions++
	call := n.subscriptions
	result := n.results[len(n.results)-1]
	if call <= len(n.results) {
		result = n.results[call-1]
	}
	n.mu.Unlock()
	n.subscribeCalls <- call
	if result.block != nil {
		<-result.block
	}
	return result.events, result.err
}

func (n *firstTokenTimeoutLifecycleNotifier) subscribeCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.subscriptions
}

type firstTokenTimeoutManualTicker struct {
	ticks   chan time.Time
	stopped chan struct{}
	once    sync.Once
}

func newFirstTokenTimeoutManualTicker() *firstTokenTimeoutManualTicker {
	return &firstTokenTimeoutManualTicker{
		ticks:   make(chan time.Time, 16),
		stopped: make(chan struct{}),
	}
}

func (t *firstTokenTimeoutManualTicker) Chan() <-chan time.Time {
	return t.ticks
}

func (t *firstTokenTimeoutManualTicker) Stop() {
	t.once.Do(func() { close(t.stopped) })
}

func (t *firstTokenTimeoutManualTicker) Tick() {
	t.ticks <- time.Now()
}

func waitForFirstTokenTimeoutSubscribeCall(t *testing.T, notifier *firstTokenTimeoutLifecycleNotifier) int {
	t.Helper()
	select {
	case call := <-notifier.subscribeCalls:
		return call
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for policy subscription")
		return 0
	}
}

func waitForFirstTokenTimeoutReload(t *testing.T, repo *firstTokenTimeoutCountingRepo) int {
	t.Helper()
	select {
	case call := <-repo.reloadCalls:
		return call
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for policy reload")
		return 0
	}
}

func waitForFirstTokenTimeoutWorkerExit(t *testing.T, policy *FirstTokenTimeoutPolicy) {
	t.Helper()
	select {
	case <-policy.workerDone:
	case <-time.After(time.Second):
		t.Fatal("policy worker did not stop after context cancellation")
	}
}

func (n *firstTokenTimeoutTestNotifier) Publish(context.Context) error {
	n.mu.Lock()
	n.published++
	err := n.publishErr
	n.mu.Unlock()
	select {
	case n.events <- struct{}{}:
	default:
	}
	return err
}

func (n *firstTokenTimeoutTestNotifier) Subscribe(context.Context) (<-chan struct{}, error) {
	return n.events, nil
}

func (n *firstTokenTimeoutTestNotifier) publishCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.published
}
