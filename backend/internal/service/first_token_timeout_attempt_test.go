package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFirstTokenAttemptPendingTransitionsOnlyOnce(t *testing.T) {
	tests := []struct {
		name      string
		terminate func(*FirstTokenAttempt) bool
		wantState FirstTokenAttemptState
		wantCause error
	}{
		{
			name:      "committed",
			terminate: func(a *FirstTokenAttempt) bool { return a.MarkFirstToken() },
			wantState: FirstTokenCommitted,
		},
		{
			name: "canceled",
			terminate: func(a *FirstTokenAttempt) bool {
				return a.Cancel(ErrFirstTokenPreludeTooLarge)
			},
			wantState: FirstTokenCanceled,
			wantCause: ErrFirstTokenPreludeTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewFirstTokenAttempt(context.Background(), time.Hour)
			t.Cleanup(a.Close)

			require.Equal(t, FirstTokenPending, a.State())
			require.True(t, tt.terminate(a))
			require.Equal(t, tt.wantState, a.State())
			require.False(t, a.MarkFirstToken())
			require.False(t, a.Cancel(errors.New("late cancellation")))
			if tt.wantCause != nil {
				require.ErrorIs(t, context.Cause(a.Context()), tt.wantCause)
			} else {
				require.NoError(t, a.Context().Err())
			}
		})
	}
}

func TestFirstTokenAttemptTimeoutWinsOnceWithDedicatedCause(t *testing.T) {
	a := NewFirstTokenAttempt(context.Background(), time.Millisecond)
	t.Cleanup(a.Close)

	select {
	case <-a.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("attempt timer did not cancel its context")
	}

	require.Equal(t, FirstTokenTimedOut, a.State())
	require.ErrorIs(t, context.Cause(a.Context()), ErrFirstTokenTimeout)
	require.False(t, a.MarkFirstToken())
	require.False(t, a.Cancel(ErrFirstTokenPreludeTooLarge))
}

func TestFirstTokenAttemptCommitAndTimeoutHaveSingleWinner(t *testing.T) {
	for i := 0; i < 500; i++ {
		a := NewFirstTokenAttempt(context.Background(), time.Nanosecond)
		start := make(chan struct{})
		var commitWon atomic.Bool
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			commitWon.Store(a.MarkFirstToken())
		}()

		close(start)
		wg.Wait()
		require.Eventually(t, func() bool {
			return a.State() != FirstTokenPending
		}, time.Second, time.Microsecond)

		winners := 0
		if commitWon.Load() {
			winners++
		}
		if a.State() == FirstTokenTimedOut {
			winners++
		}
		require.Equal(t, 1, winners)
		require.Contains(t, []FirstTokenAttemptState{FirstTokenCommitted, FirstTokenTimedOut}, a.State())
		a.Close()
	}
}

func TestFirstTokenAttemptCancelPreservesPreludeCause(t *testing.T) {
	a := NewFirstTokenAttempt(context.Background(), time.Hour)
	t.Cleanup(a.Close)

	require.True(t, a.Cancel(ErrFirstTokenPreludeTooLarge))
	require.Equal(t, FirstTokenCanceled, a.State())
	require.ErrorIs(t, context.Cause(a.Context()), ErrFirstTokenPreludeTooLarge)
}

func TestFirstTokenAttemptAlreadyCanceledParentTakesPriority(t *testing.T) {
	parent, cancelParent := context.WithCancelCause(context.Background())
	parentCause := errors.New("client disconnected")
	cancelParent(parentCause)

	a := NewFirstTokenAttempt(parent, time.Nanosecond)
	t.Cleanup(a.Close)

	require.Equal(t, FirstTokenCanceled, a.State())
	require.ErrorIs(t, context.Cause(a.Context()), parentCause)
	require.False(t, a.MarkFirstToken())
	require.False(t, a.Cancel(ErrFirstTokenPreludeTooLarge))
}

func TestFirstTokenAttemptParentCancellationStopsPendingAttempt(t *testing.T) {
	parent, cancelParent := context.WithCancelCause(context.Background())
	a := NewFirstTokenAttempt(parent, time.Hour)
	t.Cleanup(a.Close)
	parentCause := errors.New("client disconnected")

	cancelParent(parentCause)
	require.Eventually(t, func() bool {
		return a.State() == FirstTokenCanceled
	}, time.Second, time.Millisecond)
	require.ErrorIs(t, context.Cause(a.Context()), parentCause)
}

func TestFirstTokenAttemptCloseIsIdempotentAndStopsTimer(t *testing.T) {
	a := NewFirstTokenAttempt(context.Background(), 20*time.Millisecond)

	a.Close()
	a.Close()
	require.Equal(t, FirstTokenCanceled, a.State())
	require.ErrorIs(t, context.Cause(a.Context()), context.Canceled)

	time.Sleep(40 * time.Millisecond)
	require.Equal(t, FirstTokenCanceled, a.State())
	require.GreaterOrEqual(t, a.Elapsed(), 40*time.Millisecond)
}

func TestFirstTokenAttemptCloseReleasesCommittedContext(t *testing.T) {
	a := NewFirstTokenAttempt(context.Background(), time.Hour)
	require.True(t, a.MarkFirstToken())
	require.NoError(t, a.Context().Err())

	a.Close()
	require.Equal(t, FirstTokenCommitted, a.State())
	require.ErrorIs(t, context.Cause(a.Context()), context.Canceled)
}
