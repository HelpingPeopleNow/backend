package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ── resolveViaAuthService via httptest server ───────────────────────

func TestResolveViaAuthServiceOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"userId": "user-123"})
	}))
	defer srv.Close()

	m := NewAuthMiddleware(srv.URL, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: canonicalSessionCookieName, Value: "tok.encrypted"})

	id := m.resolveViaAuthService(req)
	assert.Equal(t, "user-123", id)
}

func TestResolveViaAuthServiceUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	m := NewAuthMiddleware(srv.URL, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: canonicalSessionCookieName, Value: "tok.encrypted"})

	id := m.resolveViaAuthService(req)
	assert.Equal(t, "", id)
}

func TestResolveViaAuthServiceUnreachable(t *testing.T) {
	m := NewAuthMiddleware("http://127.0.0.1:1", nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: canonicalSessionCookieName, Value: "tok.encrypted"})

	id := m.resolveViaAuthService(req)
	assert.Equal(t, "", id)
}

func TestResolveViaAuthServiceMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{invalid json`))
	}))
	defer srv.Close()

	m := NewAuthMiddleware(srv.URL, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: canonicalSessionCookieName, Value: "tok.encrypted"})

	id := m.resolveViaAuthService(req)
	assert.Equal(t, "", id)
}

func TestResolveViaAuthServiceNoSessionCookie(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	m := NewAuthMiddleware(srv.URL, nil)
	// No cookie on request — should short-circuit before HTTP call
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	id := m.resolveViaAuthService(req)
	assert.Equal(t, "", id)
	assert.False(t, called, "auth service should not be called without a session cookie")
}

func TestResolveViaAuthServiceSecureCookie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"userId": "secure-user"})
	}))
	defer srv.Close()

	m := NewAuthMiddleware(srv.URL, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: secureSessionCookieName, Value: "secure.tok"})

	id := m.resolveViaAuthService(req)
	assert.Equal(t, "secure-user", id)
}

func TestResolveViaAuthServiceMissingFieldsJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"other": "value"})
	}))
	defer srv.Close()

	m := NewAuthMiddleware(srv.URL, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: canonicalSessionCookieName, Value: "tok.encrypted"})

	id := m.resolveViaAuthService(req)
	// JSON decodes successfully but userId is empty string
	assert.Equal(t, "", id)
}

func TestResolveViaAuthServiceServerInternalError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	m := NewAuthMiddleware(srv.URL, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: canonicalSessionCookieName, Value: "tok.encrypted"})

	id := m.resolveViaAuthService(req)
	assert.Equal(t, "", id)
}

// ── resolve end-to-end via httptest server ──────────────────────────

func TestResolveFallsBackToDB(t *testing.T) {
	// Auth service returns non-OK, DB is nil → should return ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	m := NewAuthMiddleware(srv.URL, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: canonicalSessionCookieName, Value: "tok.encrypted"})

	id := m.resolve(req)
	assert.Equal(t, "", id)
}

func TestResolveViaAuthServiceSucceedsDirectly(t *testing.T) {
	// Auth service returns user ID → resolve should return it without DB fallback
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"userId": "direct-user"})
	}))
	defer srv.Close()

	m := NewAuthMiddleware(srv.URL, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: canonicalSessionCookieName, Value: "tok.encrypted"})

	id := m.resolve(req)
	assert.Equal(t, "direct-user", id)
}
