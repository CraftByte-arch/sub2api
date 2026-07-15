package handler

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const firstTokenResponseGatePreludeLimit = 256 << 10

var ErrFirstTokenResponseGateHijackUnsupported = errors.New("first token response gate: hijack is unsupported while pending")
var ErrFirstTokenResponseGatePushUnsupported = errors.New("first token response gate: server push is unsupported while pending")

type firstTokenResponseGateState uint8

const (
	firstTokenResponseGatePending firstTokenResponseGateState = iota
	firstTokenResponseGateCommitted
	firstTokenResponseGateRolledBack
)

type FirstTokenResponseGate struct {
	base    gin.ResponseWriter
	attempt firstTokenAttemptController

	mu          sync.Mutex
	state       firstTokenResponseGateState
	header      http.Header
	status      int
	prelude     []byte
	terminalErr error
	stopAttempt func() bool
}

var _ gin.ResponseWriter = (*FirstTokenResponseGate)(nil)

type firstTokenAttemptController interface {
	Context() context.Context
	MarkFirstToken() bool
	Cancel(error) bool
	State() service.FirstTokenAttemptState
	Cause() error
}

func NewFirstTokenResponseGate(base gin.ResponseWriter, attempt *service.FirstTokenAttempt) *FirstTokenResponseGate {
	w := &FirstTokenResponseGate{
		base:    base,
		attempt: attempt,
		header:  base.Header().Clone(),
		status:  base.Status(),
	}
	w.mu.Lock()
	w.stopAttempt = context.AfterFunc(attempt.Context(), w.rollbackFromAttempt)
	w.mu.Unlock()
	return w
}

func (w *FirstTokenResponseGate) Header() http.Header {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch w.state {
	case firstTokenResponseGateCommitted:
		return w.base.Header()
	case firstTokenResponseGateRolledBack:
		return make(http.Header)
	default:
		return w.header
	}
}

func (w *FirstTokenResponseGate) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch w.state {
	case firstTokenResponseGateCommitted:
		w.base.WriteHeader(status)
	case firstTokenResponseGatePending:
		if status > 0 {
			w.status = status
		}
	}
}

func (w *FirstTokenResponseGate) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writePendingOrPassthrough(p, false)
}

func (w *FirstTokenResponseGate) WriteString(s string) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writePendingOrPassthrough([]byte(s), true)
}

func (w *FirstTokenResponseGate) Status() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state == firstTokenResponseGatePending {
		return w.status
	}
	return w.base.Status()
}

func (w *FirstTokenResponseGate) Size() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state != firstTokenResponseGateCommitted {
		return -1
	}
	return w.base.Size()
}

func (w *FirstTokenResponseGate) Written() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state == firstTokenResponseGateCommitted && w.base.Written()
}

func (w *FirstTokenResponseGate) Committed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state == firstTokenResponseGateCommitted
}

func (w *FirstTokenResponseGate) WriteHeaderNow() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state == firstTokenResponseGateCommitted {
		w.base.WriteHeaderNow()
	}
}

func (w *FirstTokenResponseGate) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state == firstTokenResponseGateCommitted {
		w.base.Flush()
	}
}

func (w *FirstTokenResponseGate) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state != firstTokenResponseGateCommitted {
		return nil, nil, ErrFirstTokenResponseGateHijackUnsupported
	}
	return w.base.Hijack()
}

func (w *FirstTokenResponseGate) CloseNotify() <-chan bool {
	return w.base.CloseNotify()
}

func (w *FirstTokenResponseGate) Pusher() http.Pusher {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state != firstTokenResponseGateCommitted {
		return firstTokenResponseGateUnsupportedPusher{}
	}
	return w.base.Pusher()
}

func (w *FirstTokenResponseGate) Commit() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	switch w.state {
	case firstTokenResponseGateCommitted:
		return w.terminalErr
	case firstTokenResponseGateRolledBack:
		return w.terminalCause()
	}

	if !w.attempt.MarkFirstToken() {
		cause := w.attemptCause()
		w.rollbackLocked(cause)
		w.stopAttemptCallback()
		return cause
	}

	w.state = firstTokenResponseGateCommitted
	w.stopAttemptCallback()
	return w.commitBufferedLocked()
}

func (w *FirstTokenResponseGate) CommitTerminal() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	switch w.state {
	case firstTokenResponseGateCommitted:
		return w.terminalErr
	case firstTokenResponseGateRolledBack:
		return w.terminalCause()
	}

	w.stopAttemptCallback()
	if !w.attempt.Cancel(context.Canceled) {
		cause := w.attemptCause()
		w.rollbackLocked(cause)
		return cause
	}
	w.state = firstTokenResponseGateCommitted
	return w.commitBufferedLocked()
}

func (w *FirstTokenResponseGate) commitBufferedLocked() error {
	destinationHeader := w.base.Header()
	clear(destinationHeader)
	for key, values := range w.header {
		destinationHeader[key] = append([]string(nil), values...)
	}
	w.base.WriteHeader(w.status)
	w.base.WriteHeaderNow()
	if len(w.prelude) > 0 {
		n, err := w.base.Write(w.prelude)
		if err == nil && n != len(w.prelude) {
			err = io.ErrShortWrite
		}
		w.terminalErr = err
	}
	w.clearLocalState()
	return w.terminalErr
}

func (w *FirstTokenResponseGate) Rollback() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state != firstTokenResponseGatePending {
		return
	}
	cause := w.cancelAttempt(context.Canceled)
	w.rollbackLocked(cause)
	w.stopAttemptCallback()
}

func (w *FirstTokenResponseGate) writePendingOrPassthrough(p []byte, asString bool) (int, error) {
	switch w.state {
	case firstTokenResponseGateCommitted:
		if asString {
			return w.base.WriteString(string(p))
		}
		return w.base.Write(p)
	case firstTokenResponseGateRolledBack:
		return 0, w.terminalCause()
	}

	if w.attempt.State() != service.FirstTokenPending {
		cause := w.attemptCause()
		w.rollbackLocked(cause)
		w.stopAttemptCallback()
		return 0, cause
	}
	if len(p) > firstTokenResponseGatePreludeLimit-len(w.prelude) {
		cause := w.cancelAttempt(service.ErrFirstTokenPreludeTooLarge)
		w.rollbackLocked(cause)
		w.stopAttemptCallback()
		return 0, cause
	}
	w.prelude = append(w.prelude, p...)
	return len(p), nil
}

func (w *FirstTokenResponseGate) rollbackLocked(cause error) {
	w.state = firstTokenResponseGateRolledBack
	w.terminalErr = cause
	w.clearLocalState()
}

func (w *FirstTokenResponseGate) clearLocalState() {
	w.header = nil
	w.prelude = nil
	w.status = 0
}

func (w *FirstTokenResponseGate) rollbackFromAttempt() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state == firstTokenResponseGatePending {
		w.rollbackLocked(w.attemptCause())
	}
	w.stopAttempt = nil
}

func (w *FirstTokenResponseGate) stopAttemptCallback() {
	if w.stopAttempt != nil {
		w.stopAttempt()
		w.stopAttempt = nil
	}
}

func (w *FirstTokenResponseGate) attemptCause() error {
	if cause := w.attempt.Cause(); cause != nil {
		return cause
	}
	return context.Canceled
}

func (w *FirstTokenResponseGate) cancelAttempt(cause error) error {
	if w.attempt.Cancel(cause) {
		return cause
	}
	return w.attemptCause()
}

func (w *FirstTokenResponseGate) terminalCause() error {
	if w.terminalErr != nil {
		return w.terminalErr
	}
	return context.Canceled
}

func firstTokenResponseGateEligible(c *gin.Context) bool {
	return c != nil && c.Request != nil && !c.IsWebsocket()
}

type firstTokenResponseGateUnsupportedPusher struct{}

var _ http.Pusher = firstTokenResponseGateUnsupportedPusher{}

func (firstTokenResponseGateUnsupportedPusher) Push(string, *http.PushOptions) error {
	return ErrFirstTokenResponseGatePushUnsupported
}
