package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"gorm.io/gorm"
)

type AuthMiddleware struct {
	authServiceURL string
	db             *gorm.DB
	// secret is the Better Auth shared secret. The DB-fallback resolve path
	// requires it to verify the cookie's HMAC signature before honoring a
	// session token (P2-3 audit / F8). Empty secret means DB-fallback is
	// disabled — log loudly so an operator notices.
	secret string
}

// NewAuthMiddleware wires the auth middleware.
//
// secret is used to verify the HMAC on `value.signature` session cookies
// when the auth service is unreachable. Pass the same BETTER_AUTH_SECRET
// value the auth service signs with (production must set this).
func NewAuthMiddleware(authServiceURL string, db *gorm.DB, secret string) *AuthMiddleware {
	return &AuthMiddleware{authServiceURL: authServiceURL, db: db, secret: secret}
}

func (m *AuthMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := m.resolve(r)
		if userID == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		ctx := contextkeys.SetUserID(r.Context(), userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *AuthMiddleware) resolve(r *http.Request) string {
	if id := m.resolveViaAuthService(r); id != "" {
		return id
	}
	return m.resolveViaDB(r)
}

func (m *AuthMiddleware) resolveViaAuthService(r *http.Request) string {
	if m.authServiceURL == "" {
		return ""
	}
	authReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, m.authServiceURL+"/api/auth/user-id", nil)
	if err != nil {
		slog.Debug("auth: failed to create auth request", "error", err)
		return ""
	}
	addSessionCookie(authReq, r)
	if !hasSessionCookie(authReq) {
		return ""
	}

	client := &http.Client{Timeout: 3 * time.Second}
	authResp, err := client.Do(authReq)
	if err != nil {
		slog.Debug("auth: auth service unreachable", "error", err)
		return ""
	}
	defer authResp.Body.Close()

	if authResp.StatusCode != http.StatusOK {
		slog.Debug("auth: non-OK status", "status", authResp.StatusCode)
		return ""
	}

	var result struct {
		UserID string `json:"userId"`
	}
	// P2-4 (audit): reject unknown fields from the auth-service response.
	authDec := json.NewDecoder(authResp.Body)
	authDec.DisallowUnknownFields()
	if err := authDec.Decode(&result); err != nil {
		slog.Debug("auth: decode response failed", "error", err)
		return ""
	}
	return result.UserID
}

// DB-fallback auth path.
//
// Better Auth signs the session cookie as `<value>.<signature>` where the
// signature is an HMAC-SHA256 of `value` under BETTER_AUTH_SECRET. Before
// this audit (F8 / P2-3), the backend dropped the signature entirely
// (`rawSessionToken` returned only the `value` portion) and the DB
// lookup matched the unsigned token. That meant a leaked cookie whose
// signature had been stripped could still resolve to a session.
//
// We now:
//  1. Fail-closed if `secret` is unset — log loudly so operators notice.
//  2. Split value+signature on the first ".". Tokens without a signature
//     segment are rejected.
//  3. Verify the signature against HMAC-SHA256(value, secret). We accept
//     hex, base64.RawURL (no padding), or base64.StdEncoding (with
//     padding) so we don't depend on Better Auth choosing exactly one
//     encoding in the future — constant-time byte comparison on the
//     decoded HMAC.
//  4. Only then do the DB lookup with the unsigned token.
func (m *AuthMiddleware) resolveViaDB(r *http.Request) string {
	if m.db == nil {
		return ""
	}
	if m.secret == "" {
		slog.Warn("auth: DB-fallback requested but BETTER_AUTH_SECRET is unset; rejecting (F8 / P2-3)")
		return ""
	}
	cookie, ok := sessionCookie(r)
	if !ok {
		return ""
	}
	value, signature, ok := splitSessionCookie(cookie.Value)
	if !ok || value == "" || signature == "" {
		slog.Debug("auth: cookie missing value or signature segment; rejecting")
		return ""
	}
	if !verifySessionHMAC(value, signature, m.secret) {
		slog.Debug("auth: cookie HMAC verification failed; rejecting")
		return ""
	}
	type dbSession struct {
		UserID string `gorm:"column:userId"`
	}
	var s dbSession
	err := m.db.Table(`"session"`).Where("token = ? AND \"expiresAt\" > NOW()", value).First(&s).Error
	if err != nil {
		return ""
	}
	return s.UserID
}

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

func hasSessionCookie(r *http.Request) bool {
	_, ok := sessionCookie(r)
	return ok
}

// rawSessionToken returns the unsigned token portion of a Better Auth
// session cookie (`<value>.<signature>`). Kept for backwards compatibility
// with call sites that only need the value; new code should prefer
// splitSessionCookie to also have the signature.
func rawSessionToken(cookie *http.Cookie) string {
	if cookie == nil || cookie.Value == "" {
		return ""
	}
	return strings.SplitN(cookie.Value, ".", 2)[0]
}

// splitSessionCookie parses a `value.signature` cookie value.
func splitSessionCookie(raw string) (value, signature string, ok bool) {
	if raw == "" {
		return "", "", false
	}
	parts := strings.SplitN(raw, ".", 2)
	switch len(parts) {
	case 2:
		return parts[0], parts[1], true
	default:
		return parts[0], "", true
	}
}

// verifySessionHMAC checks an HMAC-SHA256 signature against one of the
// common encodings Better Auth or a future library might emit. Returns
// true on a constant-time byte match in any of {hex, base64.RawURL,
// base64.StdEncoding}.
func verifySessionHMAC(value, signature, secret string) bool {
	if value == "" || signature == "" || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(value))
	expected := mac.Sum(nil)

	if dec, err := hex.DecodeString(signature); err == nil && subtle.ConstantTimeCompare(dec, expected) == 1 {
		return true
	}
	if dec, err := base64.RawURLEncoding.DecodeString(signature); err == nil && subtle.ConstantTimeCompare(dec, expected) == 1 {
		return true
	}
	if dec, err := base64.StdEncoding.DecodeString(signature); err == nil && subtle.ConstantTimeCompare(dec, expected) == 1 {
		return true
	}
	return false
}
