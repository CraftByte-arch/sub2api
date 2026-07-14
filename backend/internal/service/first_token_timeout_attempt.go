package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

type FirstTokenAttemptState uint32

const (
	FirstTokenPending FirstTokenAttemptState = iota
	FirstTokenCommitted
	FirstTokenTimedOut
	FirstTokenCanceled
)

var ErrFirstTokenTimeout = errors.New("first token timeout")
var ErrFirstTokenPreludeTooLarge = errors.New("first token prelude too large")

type FirstTokenAttempt struct {
	ctx           context.Context
	cancel        context.CancelCauseFunc
	parent        context.Context
	state         atomic.Uint32
	startedAt     time.Time
	causeReady    chan struct{}
	terminalCause error

	timerMu sync.Mutex
	timer   *time.Timer

	parentMu   sync.Mutex
	stopParent func() bool
}

func NewFirstTokenAttempt(parent context.Context, timeout time.Duration) *FirstTokenAttempt {
	ctx, cancel := context.WithCancelCause(context.WithoutCancel(parent))
	a := &FirstTokenAttempt{
		ctx:        ctx,
		cancel:     cancel,
		parent:     parent,
		startedAt:  time.Now(),
		causeReady: make(chan struct{}),
	}

	a.parentMu.Lock()
	a.stopParent = context.AfterFunc(parent, a.cancelFromParent)
	a.parentMu.Unlock()
	if parent.Err() != nil {
		a.cancelFromParent()
		return a
	}
	if timeout <= 0 {
		a.timeout()
		return a
	}

	a.timerMu.Lock()
	if a.State() == FirstTokenPending {
		a.timer = time.AfterFunc(timeout, a.timeout)
	}
	a.timerMu.Unlock()
	return a
}

func (a *FirstTokenAttempt) Context() context.Context {
	return a.ctx
}

func (a *FirstTokenAttempt) MarkFirstToken() bool {
	if a.parent.Err() != nil {
		a.cancelFromParent()
		return false
	}
	if !a.transition(FirstTokenCommitted, nil) {
		return false
	}
	a.stopTimer()
	return true
}

func (a *FirstTokenAttempt) Cancel(cause error) bool {
	if cause == nil {
		cause = context.Canceled
	}
	if !a.transition(FirstTokenCanceled, cause) {
		return false
	}
	a.stopTimer()
	a.stopParentWatcher()
	return true
}

func (a *FirstTokenAttempt) State() FirstTokenAttemptState {
	return FirstTokenAttemptState(a.state.Load())
}

func (a *FirstTokenAttempt) Cause() error {
	state := a.State()
	if state == FirstTokenPending {
		if cause := context.Cause(a.ctx); cause != nil {
			return cause
		}
		if a.State() == FirstTokenPending {
			return nil
		}
	}
	<-a.causeReady
	return a.terminalCause
}

func (a *FirstTokenAttempt) Elapsed() time.Duration {
	return time.Since(a.startedAt)
}

func (a *FirstTokenAttempt) Close() {
	state := a.State()
	if state == FirstTokenPending {
		a.Cancel(context.Canceled)
	} else {
		a.waitForTerminalCause()
	}
	a.stopTimer()
	a.stopParentWatcher()
	if a.State() == FirstTokenCommitted {
		a.cancel(context.Canceled)
	}
}

func (a *FirstTokenAttempt) timeout() {
	if !a.transition(FirstTokenTimedOut, ErrFirstTokenTimeout) {
		return
	}
	a.stopTimer()
	a.stopParentWatcher()
}

func (a *FirstTokenAttempt) cancelFromParent() {
	cause := context.Cause(a.parent)
	if cause == nil {
		cause = context.Canceled
	}
	if a.transition(FirstTokenCanceled, cause) {
		a.stopTimer()
		a.clearParentWatcher()
		return
	}
	if a.State() == FirstTokenCommitted {
		a.cancel(cause)
	}
	a.clearParentWatcher()
}

func (a *FirstTokenAttempt) transition(state FirstTokenAttemptState, cause error) bool {
	if !a.state.CompareAndSwap(uint32(FirstTokenPending), uint32(state)) {
		a.waitForTerminalCause()
		return false
	}
	if state != FirstTokenCommitted {
		a.cancel(cause)
	}
	a.terminalCause = cause
	close(a.causeReady)
	return true
}

func (a *FirstTokenAttempt) waitForTerminalCause() {
	<-a.causeReady
}

func (a *FirstTokenAttempt) stopTimer() {
	a.timerMu.Lock()
	timer := a.timer
	a.timer = nil
	a.timerMu.Unlock()
	if timer != nil {
		timer.Stop()
	}
}

func (a *FirstTokenAttempt) stopParentWatcher() {
	a.parentMu.Lock()
	stop := a.stopParent
	a.stopParent = nil
	a.parentMu.Unlock()
	if stop != nil {
		stop()
	}
}

func (a *FirstTokenAttempt) clearParentWatcher() {
	a.parentMu.Lock()
	a.stopParent = nil
	a.parentMu.Unlock()
}
