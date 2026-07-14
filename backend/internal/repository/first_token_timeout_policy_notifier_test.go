package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestFirstTokenTimeoutPolicyNotifierPublishesAcrossInstances(t *testing.T) {
	server := miniredis.RunT(t)
	publisherClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	subscriberClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		require.NoError(t, publisherClient.Close())
		require.NoError(t, subscriberClient.Close())
	})

	publisher := NewFirstTokenTimeoutPolicyNotifier(publisherClient)
	subscriber := NewFirstTokenTimeoutPolicyNotifier(subscriberClient)
	ctx, cancel := context.WithCancel(context.Background())
	events, err := subscriber.Subscribe(ctx)
	require.NoError(t, err)

	require.NoError(t, publisher.Publish(context.Background()))
	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first token timeout policy invalidation")
	}

	cancel()
	select {
	case _, ok := <-events:
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("subscription did not stop after context cancellation")
	}
}

func TestFirstTokenTimeoutPolicyNotifierRejectsNilRedis(t *testing.T) {
	notifier := NewFirstTokenTimeoutPolicyNotifier(nil)
	_, err := notifier.Subscribe(context.Background())
	require.Error(t, err)
	require.Error(t, notifier.Publish(context.Background()))
}
