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

	resourcesMu sync.Mutex
	timer       *time.Timer
	stopParent  func() bool

	beforeTerminalCauseWait func()
}

func NewFirstTokenAttempt(parent context.Context, timeout time.Duration) *FirstTokenAttempt {
	ctx, cancel := context.WithCancelCause(parent)
	a := &FirstTokenAttempt{
		ctx:        ctx,
		cancel:     cancel,
		parent:     parent,
		startedAt:  time.Now(),
		causeReady: make(chan struct{}),
	}

	if parent.Err() != nil {
		a.transition(FirstTokenCanceled, context.Cause(parent))
		return a
	}

	a.resourcesMu.Lock()
	a.stopParent = context.AfterFunc(parent, a.cancelFromParent)
	a.timer = time.AfterFunc(timeout, a.timeout)
	a.resourcesMu.Unlock()
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
	return a.transition(FirstTokenCommitted, nil)
}

func (a *FirstTokenAttempt) Cancel(cause error) bool {
	if a.parent.Err() != nil {
		cause = context.Cause(a.parent)
	} else if cause == nil {
		cause = context.Canceled
	}
	return a.transition(FirstTokenCanceled, cause)
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
	a.stopResources()
	if a.State() == FirstTokenCommitted {
		a.cancel(context.Canceled)
	}
}

func (a *FirstTokenAttempt) timeout() {
	if a.parent.Err() != nil {
		a.cancelFromParent()
		return
	}
	a.transition(FirstTokenTimedOut, ErrFirstTokenTimeout)
}

func (a *FirstTokenAttempt) cancelFromParent() {
	a.transition(FirstTokenCanceled, context.Cause(a.parent))
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
	a.stopResources()
	return true
}

func (a *FirstTokenAttempt) waitForTerminalCause() {
	if a.beforeTerminalCauseWait != nil {
		a.beforeTerminalCauseWait()
	}
	<-a.causeReady
}

func (a *FirstTokenAttempt) stopResources() {
	a.resourcesMu.Lock()
	defer a.resourcesMu.Unlock()
	if a.timer != nil {
		a.timer.Stop()
		a.timer = nil
	}
	if a.stopParent != nil {
		a.stopParent()
		a.stopParent = nil
	}
}
