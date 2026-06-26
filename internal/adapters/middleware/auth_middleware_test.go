package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAuthMiddlewareResolveViaAuthServiceEmpty(t *testing.T) {
	m := NewAuthMiddleware("", nil)
	assert.Equal(t, "", m.resolveViaAuthService(nil))
}

func TestAuthMiddlewareResolveViaDBNilDB(t *testing.T) {
	m := NewAuthMiddleware("http://auth", nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	assert.Equal(t, "", m.resolveViaDB(req))
}

func TestAuthMiddlewareResolve(t *testing.T) {
	m := NewAuthMiddleware("", nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	assert.Equal(t, "", m.resolve(req))
}

func TestAuthMiddlewareWrapReturnsUnauthorized(t *testing.T) {
	m := NewAuthMiddleware("", nil)
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ── sessionCookie ────────────────────────────────────────────────────

func TestSessionCookieSecureFirst(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: secureSessionCookieName, Value: "secure.token"})
	req.AddCookie(&http.Cookie{Name: canonicalSessionCookieName, Value: "canon.token"})

	cookie, ok := sessionCookie(req)
	assert.True(t, ok)
	assert.Equal(t, secureSessionCookieName, cookie.Name)
	assert.Equal(t, "secure.token", cookie.Value)
}

func TestSessionCookieFallsBackToCanonical(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: canonicalSessionCookieName, Value: "canonical.payload"})

	cookie, ok := sessionCookie(req)
	assert.True(t, ok)
	assert.Equal(t, canonicalSessionCookieName, cookie.Name)
}

func TestSessionCookieNotFound(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	_, ok := sessionCookie(req)
	assert.False(t, ok)
}

func TestSessionCookieEmptyValue(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: canonicalSessionCookieName, Value: ""})
	_, ok := sessionCookie(req)
	assert.False(t, ok)
}

// ── hasSessionCookie ─────────────────────────────────────────────────

func TestHasSessionCookieTrue(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: canonicalSessionCookieName, Value: "tok"})
	assert.True(t, hasSessionCookie(req))
}

func TestHasSessionCookieFalse(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	assert.False(t, hasSessionCookie(req))
}

// ── addSessionCookie ─────────────────────────────────────────────────

func TestAddSessionCookieCopiesCookie(t *testing.T) {
	src, _ := http.NewRequest(http.MethodGet, "/", nil)
	src.AddCookie(&http.Cookie{Name: canonicalSessionCookieName, Value: "tok.encrypted"})

	dst, _ := http.NewRequest(http.MethodGet, "/", nil)
	ok := addSessionCookie(dst, src)
	assert.True(t, ok)

	cookies := dst.Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == canonicalSessionCookieName && c.Value == "tok.encrypted" {
			found = true
		}
	}
	assert.True(t, found, "cookie should be copied to dst")
}

func TestAddSessionCookieNoCookie(t *testing.T) {
	src, _ := http.NewRequest(http.MethodGet, "/", nil)
	dst, _ := http.NewRequest(http.MethodGet, "/", nil)
	ok := addSessionCookie(dst, src)
	assert.False(t, ok)
}

// ── rawSessionToken ──────────────────────────────────────────────────

func TestRawSessionTokenSplitsAtFirstDot(t *testing.T) {
	cookie := &http.Cookie{Name: canonicalSessionCookieName, Value: "token.encrypted.payload"}
	assert.Equal(t, "token", rawSessionToken(cookie))
}

func TestRawSessionTokenNoDot(t *testing.T) {
	cookie := &http.Cookie{Name: canonicalSessionCookieName, Value: "tokenonly"}
	assert.Equal(t, "tokenonly", rawSessionToken(cookie))
}

func TestRawSessionTokenNil(t *testing.T) {
	assert.Equal(t, "", rawSessionToken(nil))
}

func TestRawSessionTokenEmpty(t *testing.T) {
	cookie := &http.Cookie{Name: canonicalSessionCookieName, Value: ""}
	assert.Equal(t, "", rawSessionToken(cookie))
}

// ── addSessionCookie with secure cookie ──────────────────────────────

func TestAddSessionCookieSecureCookie(t *testing.T) {
	src, _ := http.NewRequest(http.MethodGet, "/", nil)
	src.AddCookie(&http.Cookie{Name: secureSessionCookieName, Value: "secure.tok"})

	dst, _ := http.NewRequest(http.MethodGet, "/", nil)
	ok := addSessionCookie(dst, src)
	assert.True(t, ok)

	cookies := dst.Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == secureSessionCookieName && c.Value == "secure.tok" {
			found = true
		}
	}
	assert.True(t, found)
}
