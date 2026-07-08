package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ── resolveViaAuthService via httptest server ─────────────────────────────────

func TestResolveViaAuthServiceOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"userId": "user-123"})
	}))
	defer srv.Close()

	// Empty secret disables DB-fallback (P2-3 adds secret as 3rd arg).
	m := NewAuthMiddleware(srv.URL, nil, "")
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

	m := NewAuthMiddleware(srv.URL, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: canonicalSessionCookieName, Value: "tok.encrypted"})

	id := m.resolveViaAuthService(req)
	assert.Equal(t, "", id)
}

func TestResolveViaAuthServiceUnreachable(t *testing.T) {
	m := NewAuthMiddleware("http://127.0.0.1:1", nil, "")
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

	m := NewAuthMiddleware(srv.URL, nil, "")
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

	m := NewAuthMiddleware(srv.URL, nil, "")
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

	m := NewAuthMiddleware(srv.URL, nil, "")
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

	m := NewAuthMiddleware(srv.URL, nil, "")
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

	m := NewAuthMiddleware(srv.URL, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: canonicalSessionCookieName, Value: "tok.encrypted"})

	id := m.resolveViaAuthService(req)
	assert.Equal(t, "", id)
}

func TestResolveViaAuthServiceRejectsUnknownFields(t *testing.T) {
	// P2-4 audit: extra fields in the auth-service response should be
	// rejected (DisallowUnknownFields). The struct only has `userId`.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"userId":"abc","injected":"evil"}`))
	}))
	defer srv.Close()

	m := NewAuthMiddleware(srv.URL, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: canonicalSessionCookieName, Value: "tok.encrypted"})

	id := m.resolveViaAuthService(req)
	assert.Equal(t, "", id, "DisallowUnknownFields must reject extra fields")
}

// ── resolve end-to-end via httptest server ──────────────────────────────────

func TestResolveFallsBackToDB(t *testing.T) {
	// Auth service returns non-OK, DB is nil → should return ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	m := NewAuthMiddleware(srv.URL, nil, "")
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

	m := NewAuthMiddleware(srv.URL, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: canonicalSessionCookieName, Value: "tok.encrypted"})

	id := m.resolve(req)
	assert.Equal(t, "direct-user", id)
}

// ── P2-3 (audit / F8) — DB-fallback Cookie HMAC Verification ──────────────

// These tests directly exercise verifySessionHMAC with deterministic
// inputs. resolveViaDB itself can't be tested without a real *gorm.DB	// fixture (audit-P3 in-flight; superseded by the runtime integration test
// in admin_handler_test.go at the time the audit closed),
// so we cover the matching logic here.

func TestVerifySessionHMACAcceptsHexSignature(t *testing.T) {
	secret := "test-secret-1234"
	value := "session-value-abc"
	// Compute the expected HMAC-SHA256 hex ourselves.
	good, sig := signCookieHex(t, value, secret)
	_ = good
	assert.True(t, verifySessionHMAC(value, sig, secret), "hex signature must verify")
}

func TestVerifySessionHMACAcceptsBase64RawURL(t *testing.T) {
	secret := "test-secret-1234"
	value := "session-value-abc"
	good, sig := signCookieBase64Raw(t, value, secret)
	_ = good
	assert.True(t, verifySessionHMAC(value, sig, secret), "base64url signature must verify")
}

func TestVerifySessionHMACAcceptsBase64Std(t *testing.T) {
	secret := "test-secret-1234"
	value := "session-value-abc"
	good, sig := signCookieBase64Std(t, value, secret)
	_ = good
	assert.True(t, verifySessionHMAC(value, sig, secret), "base64std signature must verify")
}

func TestVerifySessionHMACRejectsEmptySecret(t *testing.T) {
	// Even with a perfect signature, an empty secret must reject
	// (fail-closed to prevent dev-mode bypass).
	value := "value"
	_, sig := signCookieHex(t, value, "real-secret")
	assert.False(t, verifySessionHMAC(value, sig, ""), "empty secret must reject any signature (fail-closed)")
}

func TestVerifySessionHMACRejectsEmptyValue(t *testing.T) {
	assert.False(t, verifySessionHMAC("", "anysig", "secret"))
}

func TestVerifySessionHMACRejectsEmptySignature(t *testing.T) {
	assert.False(t, verifySessionHMAC("value", "", "secret"))
}

func TestVerifySessionHMACRejectsTamperedSignature(t *testing.T) {
	secret := "test-secret-1234"
	value := "session-value-abc"
	_, goodSig := signCookieHex(t, value, secret)
	// Flip a single byte in the hex signature.
	badSig := flipFirstHexByte(goodSig)
	assert.False(t, verifySessionHMAC(value, badSig, secret), "tampered signature must reject")
}

func TestVerifySessionHMACRejectsWrongSecret(t *testing.T) {
	value := "session-value-abc"
	// Signed with one secret, presented with a different one.
	_, sig := signCookieHex(t, value, "secret-A")
	assert.False(t, verifySessionHMAC(value, sig, "secret-B"), "wrong secret must reject")
}

func TestSplitSessionCookie(t *testing.T) {
	v, sig, ok := splitSessionCookie("tokenValue.tokenSignature")
	assert.True(t, ok)
	assert.Equal(t, "tokenValue", v)
	assert.Equal(t, "tokenSignature", sig)

	v, sig, ok = splitSessionCookie("valueNoSig")
	assert.True(t, ok)
	assert.Equal(t, "valueNoSig", v)
	assert.Equal(t, "", sig, "missing signature segment must yield empty signature")

	_, _, ok = splitSessionCookie("")
	assert.False(t, ok)
}

// rawSessionToken is still exported for legacy callers and must keep
// the original behaviour (return only the value segment).
func TestRawSessionTokenBackcompat(t *testing.T) {
	c := &http.Cookie{Value: "value.sig"}
	assert.Equal(t, "value", rawSessionToken(c))
	c = &http.Cookie{Value: "value-no-sig"}
	assert.Equal(t, "value-no-sig", rawSessionToken(c))
	c = &http.Cookie{Value: ""}
	assert.Equal(t, "", rawSessionToken(c))
	c = nil
	assert.Equal(t, "", rawSessionToken(c))
}
