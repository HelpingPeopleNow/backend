package handler

import (
	"net/http"
	"strings"
)

const (
	canonicalSessionCookieName = "better-auth.session_token"
	secureSessionCookieName    = "__Secure-better-auth.session_token"
)

var sessionCookieNames = []string{
	secureSessionCookieName,
	canonicalSessionCookieName,
}

func sessionCookie(r *http.Request) (*http.Cookie, bool) {
	for _, name := range sessionCookieNames {
		cookie, err := r.Cookie(name)
		if err == nil && cookie.Value != "" {
			return cookie, true
		}
	}
	return nil, false
}

func addSessionCookie(dst *http.Request, src *http.Request) bool {
	cookie, ok := sessionCookie(src)
	if !ok {
		return false
	}
	dst.AddCookie(cookie)
	return true
}

func rawSessionToken(cookie *http.Cookie) string {
	if cookie == nil || cookie.Value == "" {
		return ""
	}
	return strings.SplitN(cookie.Value, ".", 2)[0]
}
