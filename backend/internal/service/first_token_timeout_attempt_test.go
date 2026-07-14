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

func TestFirstTokenAttemptLosingTransitionWaitsForWinningCause(t *testing.T) {
	a := NewFirstTokenAttempt(context.Background(), time.Hour)
	originalCancel := a.cancel
	cancelEntered := make(chan struct{})
	releaseCancel := make(chan struct{})
	a.cancel = func(cause error) {
		close(cancelEntered)
		<-releaseCancel
		originalCancel(cause)
	}
	customCause := errors.New("custom cancellation")
	cancelResult := make(chan bool, 1)
	markStarted := make(chan struct{})
	markResult := make(chan bool, 1)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseCancel) }) }
	defer release()

	go func() { cancelResult <- a.Cancel(customCause) }()
	<-cancelEntered
	go func() {
		close(markStarted)
		markResult <- a.MarkFirstToken()
	}()
	<-markStarted

	// The winner is held after CAS; this short bound only checks that the loser
	// cannot return before cause publication and protects the test from deadlock.
	select {
	case <-markResult:
		t.Fatal("losing transition returned before the winning cause was published")
	case <-time.After(25 * time.Millisecond):
	}
	release()
	select {
	case won := <-cancelResult:
		require.True(t, won)
	case <-time.After(time.Second):
		t.Fatal("winning cancellation did not finish after release")
	}
	select {
	case won := <-markResult:
		require.False(t, won)
	case <-time.After(time.Second):
		t.Fatal("losing transition did not finish after cause publication")
	}
	require.ErrorIs(t, a.Cause(), customCause)

	a.cancel = originalCancel
	a.Close()
}

func TestFirstTokenAttemptCloseWaitsForWinningCausePublication(t *testing.T) {
	a := NewFirstTokenAttempt(context.Background(), time.Hour)
	originalCancel := a.cancel
	cancelEntered := make(chan struct{})
	prematureCancel := make(chan struct{})
	releaseCancel := make(chan struct{})
	var cancelCalls atomic.Int32
	a.cancel = func(cause error) {
		if cancelCalls.Add(1) == 1 {
			close(cancelEntered)
		} else {
			close(prematureCancel)
		}
		<-releaseCancel
		originalCancel(cause)
	}
	customCause := errors.New("custom cancellation")
	cancelDone := make(chan bool, 1)
	closeStarted := make(chan struct{})
	closeDone := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseCancel) }) }
	defer release()

	go func() { cancelDone <- a.Cancel(customCause) }()
	<-cancelEntered
	go func() {
		close(closeStarted)
		a.Close()
		close(closeDone)
	}()
	<-closeStarted

	// The winner is held after CAS; this short bound only checks that Close
	// waits for cause publication and protects the test from deadlock.
	select {
	case <-prematureCancel:
		release()
		t.Fatal("Close canceled the context before the winning cause was published")
	case <-closeDone:
		release()
		t.Fatal("Close returned before the winning cause was published")
	case <-time.After(25 * time.Millisecond):
	}
	release()
	select {
	case won := <-cancelDone:
		require.True(t, won)
	case <-time.After(time.Second):
		t.Fatal("winning cancellation did not finish after release")
	}
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after cause publication")
	}
	require.ErrorIs(t, a.Cause(), customCause)

	a.cancel = originalCancel
	a.Close()
}

func TestFirstTokenAttemptTimeoutWinnerOwnsContextCauseAgainstParent(t *testing.T) {
	parent, cancelParent := context.WithCancelCause(context.Background())
	a := NewFirstTokenAttempt(parent, time.Hour)
	originalCancel := a.cancel
	timeoutCancelEntered := make(chan struct{})
	releaseTimeoutCancel := make(chan struct{})
	a.cancel = func(cause error) {
		if errors.Is(cause, ErrFirstTokenTimeout) {
			close(timeoutCancelEntered)
			<-releaseTimeoutCancel
		}
		originalCancel(cause)
	}
	timeoutDone := make(chan struct{})
	parentCause := errors.New("client disconnected")

	go func() {
		a.timeout()
		close(timeoutDone)
	}()
	<-timeoutCancelEntered
	cancelParent(parentCause)
	close(releaseTimeoutCancel)
	select {
	case <-timeoutDone:
	case <-time.After(time.Second):
		t.Fatal("timeout transition did not finish after release")
	}

	require.Equal(t, FirstTokenTimedOut, a.State())
	require.ErrorIs(t, a.Cause(), ErrFirstTokenTimeout)
	require.ErrorIs(t, context.Cause(a.Context()), ErrFirstTokenTimeout)
	a.cancel = originalCancel
	a.Close()
}

func TestFirstTokenAttemptCustomWinnerOwnsContextCauseAgainstParent(t *testing.T) {
	parent, cancelParent := context.WithCancelCause(context.Background())
	a := NewFirstTokenAttempt(parent, time.Hour)
	originalCancel := a.cancel
	customCancelEntered := make(chan struct{})
	releaseCustomCancel := make(chan struct{})
	customCause := errors.New("prelude overflow")
	a.cancel = func(cause error) {
		if errors.Is(cause, customCause) {
			close(customCancelEntered)
			<-releaseCustomCancel
		}
		originalCancel(cause)
	}
	cancelDone := make(chan bool, 1)

	go func() { cancelDone <- a.Cancel(customCause) }()
	<-customCancelEntered
	cancelParent(errors.New("client disconnected"))
	close(releaseCustomCancel)
	select {
	case won := <-cancelDone:
		require.True(t, won)
	case <-time.After(time.Second):
		t.Fatal("custom cancellation did not finish after release")
	}

	require.Equal(t, FirstTokenCanceled, a.State())
	require.ErrorIs(t, a.Cause(), customCause)
	require.ErrorIs(t, context.Cause(a.Context()), customCause)
	a.cancel = originalCancel
	a.Close()
}

func TestFirstTokenAttemptCommittedParentCancellationStillCancelsStream(t *testing.T) {
	parent, cancelParent := context.WithCancelCause(context.Background())
	a := NewFirstTokenAttempt(parent, time.Hour)
	t.Cleanup(a.Close)
	parentCause := errors.New("client disconnected")
	require.True(t, a.MarkFirstToken())

	cancelParent(parentCause)
	select {
	case <-a.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("committed attempt did not propagate parent cancellation")
	}

	require.Equal(t, FirstTokenCommitted, a.State())
	require.NoError(t, a.Cause())
	require.ErrorIs(t, context.Cause(a.Context()), parentCause)
}

func TestFirstTokenAttemptNonPositiveTimeoutIsSynchronous(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second} {
		a := NewFirstTokenAttempt(context.Background(), timeout)
		require.Equal(t, FirstTokenTimedOut, a.State())
		require.ErrorIs(t, a.Cause(), ErrFirstTokenTimeout)
		require.ErrorIs(t, context.Cause(a.Context()), ErrFirstTokenTimeout)
		a.Close()
	}
}

func TestFirstTokenAttemptCancelDefersToAlreadyCanceledParent(t *testing.T) {
	parentCause := errors.New("client disconnected")
	a := newFirstTokenAttemptWithUnpublishedParentCancellation(parentCause)

	won := a.Cancel(ErrFirstTokenPreludeTooLarge)

	require.False(t, won)
	require.Equal(t, FirstTokenCanceled, a.State())
	require.ErrorIs(t, a.Cause(), parentCause)
	require.ErrorIs(t, context.Cause(a.Context()), parentCause)
	a.Close()
}

func TestFirstTokenAttemptTimeoutDefersToAlreadyCanceledParent(t *testing.T) {
	parentCause := errors.New("client disconnected")
	a := newFirstTokenAttemptWithUnpublishedParentCancellation(parentCause)

	a.timeout()

	require.Equal(t, FirstTokenCanceled, a.State())
	require.ErrorIs(t, a.Cause(), parentCause)
	require.ErrorIs(t, context.Cause(a.Context()), parentCause)
	a.Close()
}

func TestFirstTokenAttemptPreservesParentValueAndDeadlineCause(t *testing.T) {
	type contextKey string
	const key contextKey = "request-id"
	parentWithValue := context.WithValue(context.Background(), key, "req-123")
	parent, cancelParent := context.WithDeadline(parentWithValue, time.Now().Add(-time.Second))
	defer cancelParent()

	a := NewFirstTokenAttempt(parent, time.Hour)
	t.Cleanup(a.Close)

	require.Equal(t, "req-123", a.Context().Value(key))
	require.Equal(t, FirstTokenCanceled, a.State())
	require.ErrorIs(t, a.Cause(), context.DeadlineExceeded)
	require.ErrorIs(t, context.Cause(a.Context()), context.DeadlineExceeded)
}

func newFirstTokenAttemptWithUnpublishedParentCancellation(parentCause error) *FirstTokenAttempt {
	parent, cancelParent := context.WithCancelCause(context.Background())
	ctx, cancelAttempt := context.WithCancelCause(context.WithoutCancel(parent))
	a := &FirstTokenAttempt{
		ctx:           ctx,
		cancel:        cancelAttempt,
		parent:        parent,
		startedAt:     time.Now(),
		causeReady:    make(chan struct{}),
		terminalCause: nil,
	}
	cancelParent(parentCause)
	return a
}
