package repository

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const firstTokenTimeoutPolicyInvalidationChannel = "sub2api:first_token_timeout_policy:invalidate"

var errFirstTokenTimeoutPolicyNotifierUnavailable = errors.New("first token timeout policy notifier requires Redis")

type firstTokenTimeoutPolicyNotifier struct {
	rdb *redis.Client
}

func NewFirstTokenTimeoutPolicyNotifier(rdb *redis.Client) service.FirstTokenTimeoutPolicyNotifier {
	return &firstTokenTimeoutPolicyNotifier{rdb: rdb}
}

func (n *firstTokenTimeoutPolicyNotifier) Publish(ctx context.Context) error {
	if n.rdb == nil {
		return errFirstTokenTimeoutPolicyNotifierUnavailable
	}
	return n.rdb.Publish(ctx, firstTokenTimeoutPolicyInvalidationChannel, "reload").Err()
}

func (n *firstTokenTimeoutPolicyNotifier) Subscribe(ctx context.Context) (<-chan struct{}, error) {
	if n.rdb == nil {
		return nil, errFirstTokenTimeoutPolicyNotifierUnavailable
	}

	pubsub := n.rdb.Subscribe(ctx, firstTokenTimeoutPolicyInvalidationChannel)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, err
	}

	events := make(chan struct{}, 1)
	messages := pubsub.Channel()
	go func() {
		defer close(events)
		defer pubsub.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-messages:
				if !ok {
					return
				}
				select {
				case events <- struct{}{}:
				default:
				}
			}
		}
	}()

	return events, nil
}
