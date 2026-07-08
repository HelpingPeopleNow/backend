package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestNewServerSlowlorisHardening regression-tests P0-2: every audit-flagged
// timeout must be set on the production http.Server. A misconfiguration here
// is invisible at compile-time but exposes the listener to Slowloris-class
// exhaustion (audit F1, RPN 80).
func TestNewServerSlowlorisHardening(t *testing.T) {
	var called bool
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	srv := newServer(":0", mux)

	assert.Equal(t, 10*time.Second, srv.ReadHeaderTimeout,
		"ReadHeaderTimeout must be set to reject slow-header attacks (F1)")
	assert.Equal(t, 30*time.Second, srv.ReadTimeout,
		"ReadTimeout must be set to reject slow-body attacks (F1)")
	assert.Equal(t, 120*time.Second, srv.IdleTimeout,
		"IdleTimeout must be set to reclaim idle keep-alive connections (F1)")
	assert.Zero(t, srv.WriteTimeout,
		"WriteTimeout must remain zero so the SSE /stream endpoint can hold the response open")

	// Sanity: the server actually serves the handler, not just configures fields.
	srv.Handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/", nil))
	assert.True(t, called, "handler must be wired through newServer")
}
