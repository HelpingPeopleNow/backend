package handler

import (
	"net/http"
	"testing"
)

func TestSessionCookiePrefersCanonicalCookie(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: legacySessionCookieName, Value: "legacy.payload"})
	req.AddCookie(&http.Cookie{Name: canonicalSessionCookieName, Value: "canonical.payload"})

	cookie, ok := sessionCookie(req)
	if !ok {
		t.Fatal("expected a session cookie")
	}
	if cookie.Name != canonicalSessionCookieName {
		t.Fatalf("expected canonical cookie, got %q", cookie.Name)
	}
}

func TestSessionCookieFallsBackToLegacyCookie(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: legacySessionCookieName, Value: "legacy.payload"})

	cookie, ok := sessionCookie(req)
	if !ok {
		t.Fatal("expected a session cookie")
	}
	if cookie.Name != legacySessionCookieName {
		t.Fatalf("expected legacy cookie, got %q", cookie.Name)
	}
}

func TestRawSessionTokenSplitsAtFirstDot(t *testing.T) {
	cookie := &http.Cookie{Name: canonicalSessionCookieName, Value: "token.encrypted.payload"}
	if got := rawSessionToken(cookie); got != "token" {
		t.Fatalf("expected token, got %q", got)
	}
}
