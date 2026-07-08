package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper: returns the captured request_id from the inner handler's context.
func captureRequestID(t *testing.T, h http.Handler) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := contextkeys.GetRequestID(r.Context())
		w.Header().Set("captured-request-id", id)
		h.ServeHTTP(w, r)
	})
}

func TestRequestIDGeneratesWhenAbsent(t *testing.T) {
	var observed string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = contextkeys.GetRequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	captured := captureRequestID(t, inner)
	handler := RequestID(captured)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotEmpty(t, observed, "RequestID middleware must generate an ID when absent")
	assert.Len(t, observed, 32, "generated ID is 16 random bytes hex-encoded = 32 chars")
	assert.Equal(t, observed, rec.Header().Get("X-Request-ID"),
		"response X-Request-ID must match the request-context ID")
	assert.Equal(t, observed, rec.Header().Get("captured-request-id"))
}

func TestRequestIDLengthIsHex(t *testing.T) {
	// Verify the generated ID is decodable as hex (alphabet guard).
	var id string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id = contextkeys.GetRequestID(r.Context())
	})
	handler := RequestID(inner)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	for _, r := range id {
		assert.True(t,
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'f'),
			"generated ID must be lowercase hex; got %q", string(r))
	}
}

func TestRequestIDPropagatesValidInbound(t *testing.T) {
	const inbound = "abc123-deadbeef-cafe-1234-5678abcdef"
	var observed string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = contextkeys.GetRequestID(r.Context())
	})
	handler := RequestID(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", inbound)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, inbound, observed,
		"a valid inbound X-Request-ID must be propagated unchanged")
	assert.Equal(t, inbound, rec.Header().Get("X-Request-ID"))
}

func TestRequestIDRejectsOversizedInbound(t *testing.T) {
	oversized := strings.Repeat("a", requestIDMaxLen+1)
	var observed string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = contextkeys.GetRequestID(r.Context())
	})
	handler := RequestID(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", oversized)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.NotEqual(t, oversized, observed,
		"oversized inbound ID must NOT be propagated (rejected; new ID generated)")
	assert.NotEmpty(t, observed, "instead generate a new ID")
}

func TestRequestIDRejectsNonHexInbound(t *testing.T) {
	cases := []string{
		"contains spaces", // space not in alphabet
		"unicode-not-allowed-α",
		"crlf\r\ninjection",
		"trailing-cr\n",
		"semicolon;injection",
	}
	for _, bad := range cases {
		t.Run(bad, func(t *testing.T) {
			var observed string
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				observed = contextkeys.GetRequestID(r.Context())
			})
			handler := RequestID(inner)
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-Request-ID", bad)
			handler.ServeHTTP(httptest.NewRecorder(), req)
			assert.NotEqual(t, bad, observed,
				"non-hex/whitespace inbound must be rejected; got propagated: %q", observed)
			assert.NotEmpty(t, observed,
				"rejected inbound must yield a freshly generated ID")
		})
	}
}

func TestRequestIDAcceptsBoundaryLength(t *testing.T) {
	// Exactly requestIDMaxLen chars must be accepted.
	inbound := strings.Repeat("a", requestIDMaxLen)
	var observed string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = contextkeys.GetRequestID(r.Context())
	})
	handler := RequestID(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", inbound)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, inbound, observed,
		"exactly requestIDMaxLen inbound must be accepted")
}

func TestNormalizeInboundID(t *testing.T) {
	assert.Equal(t, "", normalizeInboundID(""), "empty input rejected")
	assert.Equal(t, "abcd-ef", normalizeInboundID("abcd-ef"))
	assert.Equal(t, "", normalizeInboundID("contains spaces"))
	assert.Equal(t, "", normalizeInboundID(strings.Repeat("a", requestIDMaxLen+1)),
		"oversized rejected")
	assert.Equal(t, strings.Repeat("a", requestIDMaxLen),
		normalizeInboundID(strings.Repeat("a", requestIDMaxLen)),
		"boundary length accepted")
	// Uppercase hex is in our alphabet (per normalizeInboundID).
	assert.Equal(t, "ABCD", normalizeInboundID("ABCD"))
	// Garbage chars (semicolon, etc) rejected.
	assert.Equal(t, "", normalizeInboundID("abc;def"))
}

func TestGenerateRequestIDUnique(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 50; i++ {
		id := generateRequestID()
		_, dup := seen[id]
		require.False(t, dup, "generateRequestID returned a duplicate: %q", id)
		seen[id] = struct{}{}
	}
}

func TestRequestIDEmptyHeaderGeneratesAnyway(t *testing.T) {
	// Client sent an empty-string X-Request-ID — that's "absent" in protocol
	// terms: middleware must still generate a new one.
	var observed string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = contextkeys.GetRequestID(r.Context())
	})
	handler := RequestID(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.NotEmpty(t, observed, "empty inbound ID must yield a fresh generated ID")
}

func TestRequestIDContextNotExistingAfterResponse(t *testing.T) {
	// Sanity: the request_id is bound to the middleware-created ctx; the
	// original r.Context() doesn't carry it. Useful for downstream middleware
	// that accepts only the unmodified request — they'd need to read from
	// r.Context() AFTER setRequestID was applied.
	var sid string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sid = contextkeys.GetRequestID(r.Context())
	})
	handler := RequestID(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.NotEmpty(t, sid)
}

func TestContextkeysGetRequestIDEmptyByDefault(t *testing.T) {
	// Background / non-HTTP contexts have no request_id.
	assert.Equal(t, "", contextkeys.GetRequestID(context.Background()))
}
