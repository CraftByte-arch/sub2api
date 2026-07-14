package handler

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFirstTokenResponseGateBuffersAndCommitsInOrder(t *testing.T) {
	base, raw := newFirstTokenResponseGateTestWriter()
	raw.header.Set("X-Existing", "original")
	raw.header["X-Multi"] = []string{"one", "two"}
	attempt := service.NewFirstTokenAttempt(context.Background(), time.Hour)
	t.Cleanup(attempt.Close)
	gate := NewFirstTokenResponseGate(base, attempt)

	gate.Header().Set("X-Upstream-Request-ID", "success")
	gate.Header().Set("X-Existing", "replacement")
	gate.Header()["X-Multi"][0] = "changed"
	gate.WriteHeader(http.StatusAccepted)
	n, err := gate.Write([]byte("event: one\n\n"))
	require.NoError(t, err)
	require.Equal(t, len("event: one\n\n"), n)
	n, err = gate.WriteString("event: two\n\n")
	require.NoError(t, err)
	require.Equal(t, len("event: two\n\n"), n)
	gate.WriteHeaderNow()
	gate.Flush()

	require.Equal(t, "original", raw.header.Get("X-Existing"))
	require.Equal(t, []string{"one", "two"}, raw.header.Values("X-Multi"))
	require.Empty(t, raw.header.Get("X-Upstream-Request-ID"))
	require.Empty(t, raw.body.String())
	require.Empty(t, raw.calls)
	require.Equal(t, http.StatusAccepted, gate.Status())
	require.False(t, gate.Written())
	require.Equal(t, -1, gate.Size())

	require.NoError(t, gate.Commit())
	require.Equal(t, service.FirstTokenCommitted, attempt.State())
	require.Equal(t, http.StatusAccepted, raw.status)
	require.Equal(t, "replacement", raw.header.Get("X-Existing"))
	require.Equal(t, []string{"changed", "two"}, raw.header.Values("X-Multi"))
	require.Equal(t, "success", raw.header.Get("X-Upstream-Request-ID"))
	require.Equal(t, "event: one\n\nevent: two\n\n", raw.body.String())
	require.Equal(t, []string{
		"header:202",
		"write:event: one\n\nevent: two\n\n",
	}, raw.calls)
	require.True(t, gate.Written())
	require.Equal(t, len("event: one\n\nevent: two\n\n"), gate.Size())

	n, err = gate.WriteString("tail")
	require.NoError(t, err)
	require.Equal(t, 4, n)
	gate.Flush()
	require.Equal(t, "event: one\n\nevent: two\n\ntail", raw.body.String())
	require.Equal(t, "flush", raw.calls[len(raw.calls)-1])
}

func TestFirstTokenResponseGateRollbackLeaksNothing(t *testing.T) {
	base, raw := newFirstTokenResponseGateTestWriter()
	raw.header.Set("X-Existing", "keep")
	attempt := service.NewFirstTokenAttempt(context.Background(), time.Hour)
	t.Cleanup(attempt.Close)
	gate := NewFirstTokenResponseGate(base, attempt)

	gate.Header().Set("X-Upstream-Request-ID", "failed")
	gate.Header().Del("X-Existing")
	gate.WriteHeader(http.StatusTeapot)
	_, err := gate.WriteString("event: ping\n\n")
	require.NoError(t, err)
	gate.Flush()
	gate.Rollback()
	gate.Rollback()

	require.Equal(t, service.FirstTokenCanceled, attempt.State())
	require.False(t, base.Written())
	require.Equal(t, "keep", raw.header.Get("X-Existing"))
	require.Empty(t, raw.header.Get("X-Upstream-Request-ID"))
	require.Empty(t, raw.body.String())
	require.Empty(t, raw.calls)
}

func TestFirstTokenResponseGateTimeoutRollbackLeaksNothing(t *testing.T) {
	base, raw := newFirstTokenResponseGateTestWriter()
	attempt := service.NewFirstTokenAttempt(context.Background(), time.Millisecond)
	t.Cleanup(attempt.Close)
	gate := NewFirstTokenResponseGate(base, attempt)
	gate.Header().Set("X-Failed", "secret")
	_, err := gate.WriteString("secret prelude")
	require.NoError(t, err)

	select {
	case <-attempt.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("attempt did not time out")
	}
	gate.Rollback()

	require.Equal(t, service.FirstTokenTimedOut, attempt.State())
	require.ErrorIs(t, context.Cause(attempt.Context()), service.ErrFirstTokenTimeout)
	require.Empty(t, raw.header.Get("X-Failed"))
	require.Empty(t, raw.body.String())
	require.Empty(t, raw.calls)
	require.ErrorIs(t, gate.Commit(), service.ErrFirstTokenTimeout)
}

func TestFirstTokenResponseGateAttemptTerminationAutomaticallyClearsPrelude(t *testing.T) {
	tests := []struct {
		name      string
		terminate func(*service.FirstTokenAttempt)
		wantCause error
	}{
		{
			name: "timeout",
			terminate: func(a *service.FirstTokenAttempt) {
				select {
				case <-a.Context().Done():
				case <-time.After(time.Second):
					t.Fatal("attempt did not time out")
				}
			},
			wantCause: service.ErrFirstTokenTimeout,
		},
		{
			name: "canceled",
			terminate: func(a *service.FirstTokenAttempt) {
				require.True(t, a.Cancel(service.ErrFirstTokenPreludeTooLarge))
			},
			wantCause: service.ErrFirstTokenPreludeTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, raw := newFirstTokenResponseGateTestWriter()
			timeout := time.Hour
			if tt.name == "timeout" {
				timeout = time.Millisecond
			}
			attempt := service.NewFirstTokenAttempt(context.Background(), timeout)
			t.Cleanup(attempt.Close)
			gate := NewFirstTokenResponseGate(base, attempt)
			gate.Header().Set("X-Local", "discard")
			_, err := gate.WriteString("discard")
			require.NoError(t, err)

			tt.terminate(attempt)
			require.Eventually(t, func() bool {
				gate.mu.Lock()
				defer gate.mu.Unlock()
				return gate.state == firstTokenResponseGateRolledBack &&
					gate.header == nil && gate.prelude == nil && gate.status == 0
			}, time.Second, time.Millisecond)
			require.ErrorIs(t, gate.Commit(), tt.wantCause)
			require.Empty(t, raw.body.String())
			require.Empty(t, raw.calls)
		})
	}
}

func TestFirstTokenResponseGatePreludeLimitCancelsWithoutGrowing(t *testing.T) {
	base, raw := newFirstTokenResponseGateTestWriter()
	attempt := service.NewFirstTokenAttempt(context.Background(), time.Hour)
	t.Cleanup(attempt.Close)
	gate := NewFirstTokenResponseGate(base, attempt)
	prelude := bytes.Repeat([]byte("p"), firstTokenResponseGatePreludeLimit)

	n, err := gate.Write(prelude)
	require.NoError(t, err)
	require.Equal(t, len(prelude), n)
	n, err = gate.Write([]byte("overflow"))
	require.Zero(t, n)
	require.ErrorIs(t, err, service.ErrFirstTokenPreludeTooLarge)
	require.Equal(t, service.FirstTokenCanceled, attempt.State())
	require.ErrorIs(t, context.Cause(attempt.Context()), service.ErrFirstTokenPreludeTooLarge)
	require.False(t, gate.Written())
	require.Equal(t, -1, gate.Size())
	require.Empty(t, raw.body.String())
	require.Empty(t, raw.calls)
	require.ErrorIs(t, gate.Commit(), service.ErrFirstTokenPreludeTooLarge)
}

func TestFirstTokenResponseGateOverflowUsesAttemptWinningCause(t *testing.T) {
	base, raw := newFirstTokenResponseGateTestWriter()
	realAttempt := service.NewFirstTokenAttempt(context.Background(), time.Hour)
	t.Cleanup(realAttempt.Close)
	gate := NewFirstTokenResponseGate(base, realAttempt)
	winner := service.ErrFirstTokenTimeout
	controlled := newFirstTokenResponseGateControlledAttempt(winner)
	gate.attempt = controlled

	n, err := gate.Write(bytes.Repeat([]byte("x"), firstTokenResponseGatePreludeLimit+1))
	require.Zero(t, n)
	require.ErrorIs(t, err, winner)
	require.ErrorIs(t, controlled.requestedCause, service.ErrFirstTokenPreludeTooLarge)
	require.ErrorIs(t, gate.Commit(), winner)
	require.Empty(t, raw.body.String())
	require.Empty(t, raw.calls)
}

func TestFirstTokenResponseGateRollbackUsesAttemptWinningCause(t *testing.T) {
	parentCause := errors.New("client disconnected")
	for _, winner := range []error{service.ErrFirstTokenTimeout, parentCause} {
		t.Run(winner.Error(), func(t *testing.T) {
			base, raw := newFirstTokenResponseGateTestWriter()
			realAttempt := service.NewFirstTokenAttempt(context.Background(), time.Hour)
			t.Cleanup(realAttempt.Close)
			gate := NewFirstTokenResponseGate(base, realAttempt)
			controlled := newFirstTokenResponseGateControlledAttempt(winner)
			gate.attempt = controlled

			gate.Rollback()

			require.ErrorIs(t, controlled.requestedCause, context.Canceled)
			require.ErrorIs(t, gate.Commit(), winner)
			require.Empty(t, raw.body.String())
			require.Empty(t, raw.calls)
		})
	}
}

func TestFirstTokenResponseGateCommitAndTimeoutCannotBothWin(t *testing.T) {
	for i := 0; i < 500; i++ {
		base, raw := newFirstTokenResponseGateTestWriter()
		attempt := service.NewFirstTokenAttempt(context.Background(), time.Nanosecond)
		gate := NewFirstTokenResponseGate(base, attempt)
		_, err := gate.WriteString("only committed output")
		if err != nil {
			require.ErrorIs(t, err, service.ErrFirstTokenTimeout)
		}

		commitDone := make(chan error, 1)
		go func() { commitDone <- gate.Commit() }()
		commitErr := <-commitDone
		if attempt.State() == service.FirstTokenTimedOut {
			gate.Rollback()
			require.ErrorIs(t, commitErr, service.ErrFirstTokenTimeout)
			require.Empty(t, raw.body.String())
		} else {
			require.NoError(t, commitErr)
			require.Equal(t, service.FirstTokenCommitted, attempt.State())
			require.Equal(t, "only committed output", raw.body.String())
		}
		attempt.Close()
	}
}

func TestFirstTokenResponseGateCommitWriteFailureRemainsCommitted(t *testing.T) {
	base, raw := newFirstTokenResponseGateTestWriter()
	raw.writeErr = errors.New("client disconnected")
	attempt := service.NewFirstTokenAttempt(context.Background(), time.Hour)
	t.Cleanup(attempt.Close)
	gate := NewFirstTokenResponseGate(base, attempt)
	_, err := gate.WriteString("semantic token")
	require.NoError(t, err)

	err = gate.Commit()
	require.ErrorIs(t, err, raw.writeErr)
	require.Empty(t, raw.body.String())
	require.Equal(t, service.FirstTokenCommitted, attempt.State())
	require.True(t, base.Written())
	require.Zero(t, base.Size())
	gate.Rollback()
	require.Equal(t, service.FirstTokenCommitted, attempt.State())
	require.False(t, attempt.Cancel(service.ErrFirstTokenPreludeTooLarge))

	_, err = gate.WriteString("tail")
	require.ErrorIs(t, err, raw.writeErr)
}

func TestFirstTokenResponseGatePartialCommitWriteFailureRemainsCommitted(t *testing.T) {
	base, raw := newFirstTokenResponseGateTestWriter()
	raw.writeErr = errors.New("client disconnected mid-write")
	raw.writeN = 5
	attempt := service.NewFirstTokenAttempt(context.Background(), time.Hour)
	t.Cleanup(attempt.Close)
	gate := NewFirstTokenResponseGate(base, attempt)
	_, err := gate.WriteString("semantic token")
	require.NoError(t, err)

	err = gate.Commit()
	require.ErrorIs(t, err, raw.writeErr)
	require.Equal(t, "seman", raw.body.String())
	require.Equal(t, service.FirstTokenCommitted, attempt.State())
	require.True(t, base.Written())
	require.Equal(t, 5, base.Size())

	gate.Rollback()
	require.Equal(t, "seman", raw.body.String())
	require.Equal(t, service.FirstTokenCommitted, attempt.State())
	require.False(t, attempt.Cancel(service.ErrFirstTokenPreludeTooLarge))
	require.ErrorIs(t, gate.Commit(), raw.writeErr)
}

func TestFirstTokenResponseGatePendingOptionalInterfacesCannotBypassGate(t *testing.T) {
	base, raw := newFirstTokenResponseGateTestWriter()
	attempt := service.NewFirstTokenAttempt(context.Background(), time.Hour)
	t.Cleanup(attempt.Close)
	gate := NewFirstTokenResponseGate(base, attempt)

	conn, rw, err := gate.Hijack()
	require.Nil(t, conn)
	require.Nil(t, rw)
	require.ErrorIs(t, err, ErrFirstTokenResponseGateHijackUnsupported)
	pusher := gate.Pusher()
	require.NotNil(t, pusher)
	require.ErrorIs(t, pusher.Push("/asset", nil), ErrFirstTokenResponseGatePushUnsupported)
	require.True(t, (<-chan bool)(raw.closeNotify) == gate.CloseNotify())
	require.Zero(t, raw.hijacks)
	require.Zero(t, raw.pushes)

	require.NoError(t, gate.Commit())
	_, _, err = gate.Hijack()
	require.ErrorIs(t, err, raw.hijackErr)
	require.Equal(t, 1, raw.hijacks)
	require.ErrorIs(t, gate.Pusher().Push("/asset", nil), raw.pushErr)
	require.Equal(t, 1, raw.pushes)
}

func TestFirstTokenResponseGateEligibilityRejectsWebSocket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	require.True(t, firstTokenResponseGateEligible(ctx))

	ctx.Request.Header.Set("Connection", "keep-alive, Upgrade")
	ctx.Request.Header.Set("Upgrade", "websocket")
	require.False(t, firstTokenResponseGateEligible(ctx))
}

func TestFirstTokenResponseGateCommitTerminalFlushesWithoutSemanticCommit(t *testing.T) {
	base, raw := newFirstTokenResponseGateTestWriter()
	attempt := service.NewFirstTokenAttempt(context.Background(), time.Hour)
	t.Cleanup(attempt.Close)
	gate := NewFirstTokenResponseGate(base, attempt)

	gate.Header().Set("Content-Type", "application/json")
	gate.WriteHeader(http.StatusBadRequest)
	_, err := gate.WriteString(`{"error":"rejected"}`)
	require.NoError(t, err)

	require.NoError(t, gate.CommitTerminal())
	require.Equal(t, service.FirstTokenCanceled, attempt.State())
	require.Equal(t, http.StatusBadRequest, raw.status)
	require.Equal(t, "application/json", raw.header.Get("Content-Type"))
	require.JSONEq(t, `{"error":"rejected"}`, raw.body.String())
}

type firstTokenResponseGateTestWriter struct {
	mu          sync.Mutex
	header      http.Header
	body        bytes.Buffer
	status      int
	calls       []string
	writeErr    error
	writeN      int
	hijackErr   error
	pushErr     error
	hijacks     int
	pushes      int
	closeNotify chan bool
}

type firstTokenResponseGateControlledAttempt struct {
	ctx            context.Context
	winner         error
	cancelCalled   bool
	requestedCause error
}

func newFirstTokenResponseGateControlledAttempt(winner error) *firstTokenResponseGateControlledAttempt {
	return &firstTokenResponseGateControlledAttempt{
		ctx:    context.Background(),
		winner: winner,
	}
}

func (a *firstTokenResponseGateControlledAttempt) Context() context.Context {
	return a.ctx
}

func (a *firstTokenResponseGateControlledAttempt) MarkFirstToken() bool {
	return false
}

func (a *firstTokenResponseGateControlledAttempt) Cancel(cause error) bool {
	a.cancelCalled = true
	a.requestedCause = cause
	return false
}

func (a *firstTokenResponseGateControlledAttempt) State() service.FirstTokenAttemptState {
	return service.FirstTokenPending
}

func (a *firstTokenResponseGateControlledAttempt) Cause() error {
	if !a.cancelCalled {
		return nil
	}
	return a.winner
}

func newFirstTokenResponseGateTestWriter() (gin.ResponseWriter, *firstTokenResponseGateTestWriter) {
	raw := &firstTokenResponseGateTestWriter{
		header:      make(http.Header),
		hijackErr:   errors.New("hijack delegated"),
		pushErr:     errors.New("push delegated"),
		closeNotify: make(chan bool),
	}
	ctx, _ := gin.CreateTestContext(raw)
	return ctx.Writer, raw
}

func (w *firstTokenResponseGateTestWriter) Header() http.Header {
	return w.header
}

func (w *firstTokenResponseGateTestWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status != 0 {
		return
	}
	w.status = status
	w.calls = append(w.calls, fmt.Sprintf("header:%d", status))
}

func (w *firstTokenResponseGateTestWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == 0 {
		w.status = http.StatusOK
		w.calls = append(w.calls, "header:200")
	}
	w.calls = append(w.calls, "write:"+string(p))
	if w.writeErr != nil {
		n := min(w.writeN, len(p))
		if n > 0 {
			_, _ = w.body.Write(p[:n])
		}
		return n, w.writeErr
	}
	return w.body.Write(p)
}

func (w *firstTokenResponseGateTestWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, "flush")
}

func (w *firstTokenResponseGateTestWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.hijacks++
	return nil, nil, w.hijackErr
}

func (w *firstTokenResponseGateTestWriter) CloseNotify() <-chan bool {
	return w.closeNotify
}

func (w *firstTokenResponseGateTestWriter) Push(string, *http.PushOptions) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pushes++
	return w.pushErr
}
