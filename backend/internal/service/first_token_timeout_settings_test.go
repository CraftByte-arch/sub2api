package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseFirstTokenTimeoutSettingsFallbackAndValidation(t *testing.T) {
	require.Equal(t, FirstTokenTimeoutSettings{Enabled: false, TimeoutSeconds: 30},
		parseFirstTokenTimeoutSettings(`{"enabled":true,"timeout_seconds":0}`))
	require.Error(t, validateFirstTokenTimeoutSettings(FirstTokenTimeoutSettings{Enabled: true, TimeoutSeconds: 301}))
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

func TestFirstTokenTimeoutPolicySnapshotSupportsConcurrentReadsAndUpdates(t *testing.T) {
	policy, _, _ := newFirstTokenTimeoutPolicyForTest()
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(5)
	for reader := 0; reader < 4; reader++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				snapshot := policy.Snapshot()
				require.NotZero(t, snapshot.LoadedAt)
				require.GreaterOrEqual(t, snapshot.Timeout, time.Second)
				require.LessOrEqual(t, snapshot.Timeout, 300*time.Second)
			}
		}()
	}
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			require.NoError(t, policy.Update(context.Background(), FirstTokenTimeoutSettings{
				Enabled:        i%2 == 0,
				TimeoutSeconds: i%300 + 1,
			}))
		}
	}()
	wg.Wait()
}

func newFirstTokenTimeoutPolicyForTest() (*FirstTokenTimeoutPolicy, *firstTokenTimeoutMemorySettingRepo, *firstTokenTimeoutTestNotifier) {
	repo := &firstTokenTimeoutMemorySettingRepo{values: make(map[string]string)}
	notifier := &firstTokenTimeoutTestNotifier{events: make(chan struct{}, 8)}
	return NewFirstTokenTimeoutPolicy(repo, notifier), repo, notifier
}

type firstTokenTimeoutMemorySettingRepo struct {
	mu     sync.RWMutex
	values map[string]string
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

type firstTokenTimeoutTestNotifier struct {
	mu         sync.Mutex
	published  int
	events     chan struct{}
	publishErr error
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
