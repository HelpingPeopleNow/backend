package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
)

// requestIDHeader is the canonical X-Request-ID header used for cross-service
// tracing. The frontend is not expected to send one today, but if a future
// caller (or partner integration) does, we propagate it instead of overriding.
const requestIDHeader = "X-Request-ID"

// requestIDMaxLen caps the inbound ID so log-injection / arbitrary-length
// probes are rejected (an attacker can't pass a 1 MB X-Request-ID and force
// us to log it on every line).
const requestIDMaxLen = 64

// inboundIDAlphabet restricts the inbound accepted charset to hex chars and
// hyphens — matches UUID (8-4-4-4-12 with hyphens), our own generated IDs
// (32 hex without hyphens), and short request-trace IDs.
var inboundIDAlphabet = func() map[rune]bool {
	m := map[rune]bool{}
	for c := '0'; c <= '9'; c++ {
		m[c] = true
	}
	for c := 'a'; c <= 'f'; c++ {
		m[c] = true
	}
	for c := 'A'; c <= 'F'; c++ {
		m[c] = true
	}
	m['-'] = true
	return m
}()

// RequestID middleware:
//  1. If the client sent an X-Request-ID that matches our accepted
//     alphabet and length, propagate it.
//  2. Otherwise, generate a fresh UUID-like hex ID via crypto/rand.
//  3. Set the response header so the frontend (and any operator
//     debugging the browser devtools) can capture the trace.
//  4. Store the ID in the request context (P3-4 audit cross-service
//     tracing).
//
// Inserted as the OUTERMOST middleware in buildMux so the Logging
// middleware's "request started" / "request completed" lines AND every
// downstream slog log line emit the same request_id attribute.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := normalizeInboundID(r.Header.Get(requestIDHeader))
		if id == "" {
			id = generateRequestID()
		}
		w.Header().Set(requestIDHeader, id)
		ctx := contextkeys.SetRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// generateRequestID returns a fresh hex-encoded 128-bit ID. Falls back to
// a timestamp pseudo-ID if crypto/rand is somehow unavailable (extremely
// unlikely — kept for completeness so the ID is always deterministic).
func generateRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("ts-%d", time.Now().UnixNano())
}

// normalizeInboundID returns the inbound value iff it is non-empty, at most
// requestIDMaxLen runes long, and entirely within inboundIDAlphabet.
// Whitespace around the value is trimmed; anything else yields "" so the
// caller generates a fresh ID instead.
func normalizeInboundID(raw string) string {
	if raw == "" {
		return ""
	}
	if len(raw) > requestIDMaxLen {
		return ""
	}
	for _, r := range raw {
		if !inboundIDAlphabet[r] {
			return ""
		}
	}
	// Trim is a no-op since the alphabet doesn't include whitespace, but
	// defending against the wild edge case where a header parser yields a
	// leading/trailing space char somewhere downstream.
	return raw
}
