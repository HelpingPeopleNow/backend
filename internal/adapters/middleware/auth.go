package middleware

import (
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
}

func NewAuthMiddleware(authServiceURL string, db *gorm.DB) *AuthMiddleware {
	return &AuthMiddleware{authServiceURL: authServiceURL, db: db}
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
	if err := json.NewDecoder(authResp.Body).Decode(&result); err != nil {
		slog.Debug("auth: decode response failed", "error", err)
		return ""
	}
	return result.UserID
}

func (m *AuthMiddleware) resolveViaDB(r *http.Request) string {
	if m.db == nil {
		return ""
	}
	cookie, ok := sessionCookie(r)
	if !ok {
		return ""
	}
	token := rawSessionToken(cookie)
	if token == "" {
		return ""
	}
	type dbSession struct {
		UserID string `gorm:"column:userId"`
	}
	var s dbSession
	err := m.db.Table("\"session\"").Where("token = ? AND \"expiresAt\" > NOW()", token).First(&s).Error
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

func rawSessionToken(cookie *http.Cookie) string {
	if cookie == nil || cookie.Value == "" {
		return ""
	}
	return strings.SplitN(cookie.Value, ".", 2)[0]
}
