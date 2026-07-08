package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/HelpingPeopleNow/backend/internal/testingutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// safeSSEResponseWriter is an http.ResponseWriter + http.Flusher whose
// underlying buffer is guarded by a mutex so the handler goroutine's
// Write calls and the test goroutine's Snapshot reads don't race under
// -race. *httptest.ResponseRecorder.Body is a bare *bytes.Buffer —
// String() on it from another goroutine while Write is happening
// triggers the race detector even though rw.Write itself takes an
// internal lock. We dodge that by serialising String() access in our
// own mutex-protected snapshot.
type safeSSEResponseWriter struct {
	header  http.Header
	code    int
	bodyMu  sync.Mutex
	body    bytes.Buffer
	flushed atomic.Bool
}

func newSafeSSEResponseWriter() *safeSSEResponseWriter {
	return &safeSSEResponseWriter{
		header: make(http.Header),
	}
}

func (w *safeSSEResponseWriter) Header() http.Header { return w.header }

func (w *safeSSEResponseWriter) Write(p []byte) (int, error) {
	w.bodyMu.Lock()
	defer w.bodyMu.Unlock()
	if w.code == 0 {
		w.code = http.StatusOK
	}
	return w.body.Write(p)
}

func (w *safeSSEResponseWriter) WriteHeader(statusCode int) {
	w.bodyMu.Lock()
	defer w.bodyMu.Unlock()
	if w.code == 0 {
		w.code = statusCode
	}
}

func (w *safeSSEResponseWriter) Flush() { w.flushed.Store(true) }

// Snapshot returns the body so-far under the body mutex. Safe to call
// from any goroutine.
func (w *safeSSEResponseWriter) Snapshot() string {
	w.bodyMu.Lock()
	defer w.bodyMu.Unlock()
	return w.body.String()
}

// Code returns the status code. 0 if WriteHeader / Write was never
// called.
func (w *safeSSEResponseWriter) Code() int {
	w.bodyMu.Lock()
	defer w.bodyMu.Unlock()
	return w.code
}

// TestSSEMaxStreamDurationReaperClosesConnection is the P2-6 streaming-
// loop regression the audit doc explicitly punted on: end-to-end drive
// of streamSSE through the DirectMessagingHandler's ServeHTTP dispatch
// and verify that the SSE_MAX_STREAM_DURATION deadline fires the
// broker's cleanup goroutine and that AccessConnections() drops to 0.
//
// We drive the handler in-process (NOT httptest.NewServer) because
// context.WithValue values do NOT propagate over the wire — setting
// `userID` on the client-side context would not be visible to the
// server's ServeHTTP, and the handler would 401 before subscribing.
// The in-process safeSSEResponseWriter approach makes the synthesised
// userID visible to the handler while still flowing through the
// production dispatch path.
//
// Setup:
//   - SSE_MAX_STREAM_DURATION=200ms (via t.Setenv).
//   - DirectMessagingHandler wired against a real *MockBroker.
//
// Assertions:
//   - Handler flushes "event: open" within 1s of starting.
//   - Headers serialize Content-Type: text/event-stream.
//   - Broker.ActiveConnections() === 1 while the stream is open.
//   - Broker.ActiveConnections() === 0 within 2s of the deadline firing.
//   - Handler goroutine has returned within 500ms of the deadline.
func TestSSEMaxStreamDurationReaperClosesConnection(t *testing.T) {
	t.Setenv("SSE_MAX_STREAM_DURATION", "200ms")

	broker := testingutil.NewMockBroker()
	dmHandler := NewDirectMessagingHandler(
		&testingutil.MockDMRepo{},
		&testingutil.MockProfiles{},
		broker,
		nil,
	)

	rec := newSafeSSEResponseWriter()
	ctx := contextkeys.SetUserID(context.Background(), "u-sse-test")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/direct-messages/stream", nil)
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		dmHandler.ServeHTTP(rec, req)
	}()

	// Phase 1: handler flushes the initial "event: open" frame.
	require.Eventually(t, func() bool {
		return strings.Contains(rec.Snapshot(), "event: open")
	}, 1*time.Second, 10*time.Millisecond,
		"streamSSE must flush the initial 'event: open' frame on subscription")

	// Phase 2: handler schedules a flush (the Flush() call sets the
	// atomic; we don't actually flush OS buffers, but streamSSE's
	// flusher, ok := w.(http.Flusher) assertion has fired by now).
	require.True(t, rec.flushed.Load(),
		"ResponseWriter must implement http.Flusher so streamSSE schedules a flush")

	// Phase 3: Content-Type was set.
	require.Equal(t, "text/event-stream", rec.header.Get("Content-Type"))

	// Phase 4: broker reports exactly 1 active subscription while
	// the stream is alive (handler subscribed, deadline not yet fired).
	require.Eventually(t, func() bool {
		return broker.ActiveConnections() == 1
	}, 1*time.Second, 10*time.Millisecond,
		"broker must report exactly 1 subscription once streamSSE subscribes")

	// Phase 5: SSE_MAX_STREAM_DURATION=200ms deadline fires; broker
	// cleanup goroutine decrements ActiveConnections to 0.
	require.Eventually(t, func() bool {
		return broker.ActiveConnections() == 0
	}, 2*time.Second, 20*time.Millisecond,
		"broker must drop ActiveConnections to 0 after the SSE max-stream duration deadline")

	// Phase 6: the handler goroutine must have returned.
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("streamSSE must have returned after the maxCtx deadline fired")
	}
}

func TestSSEMaxStreamDurationEnvHelperPicksUpTwoHundredMs(t *testing.T) {
	t.Setenv("SSE_MAX_STREAM_DURATION", "200ms")
	assert.Equal(t, 200*time.Millisecond, maxSSEStreamDuration())
}

// TestSSEMaxStreamDurationEnvHelperFallsBackWhenUnset uses t.Setenv
// with empty value (rather than os.Unsetenv) so t.Setenv reverts on
// cleanup and works correctly across all Go versions —
// os.Unsetenv mutates the process environment globally without
// revert (a test-pollution risk).
func TestSSEMaxStreamDurationEnvHelperFallsBackWhenUnset(t *testing.T) {
	t.Setenv("SSE_MAX_STREAM_DURATION", "")
	assert.Equal(t, 15*time.Minute, maxSSEStreamDuration(),
		"unset SSE_MAX_STREAM_DURATION must fall back to the 15m default")
}
