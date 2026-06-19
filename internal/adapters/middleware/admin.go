package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
)

type AdminMiddleware struct {
	authServiceURL string
}

func NewAdminMiddleware(authServiceURL string) *AdminMiddleware {
	return &AdminMiddleware{authServiceURL: authServiceURL}
}

func (m *AdminMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.authServiceURL == "" {
			http.Error(w, `{"error":"admin auth not configured"}`, http.StatusInternalServerError)
			return
		}

		userID := contextkeys.GetUserID(r.Context())
		if userID == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		authReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, m.authServiceURL+"/api/auth/get-session", nil)
		if err != nil {
			slog.Error("admin: failed to create auth request", "error", err)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		addSessionCookie(authReq, r)

		client := &http.Client{Timeout: 5 * time.Second}
		authResp, err := client.Do(authReq)
		if err != nil {
			slog.Error("admin: auth service unavailable", "error", err)
			http.Error(w, `{"error":"auth service unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		defer authResp.Body.Close()

		if authResp.StatusCode != http.StatusOK {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		var sessionInfo map[string]interface{}
		if err := json.NewDecoder(authResp.Body).Decode(&sessionInfo); err != nil {
			slog.Error("admin: decode failed", "error", err)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		userObj, ok := sessionInfo["user"].(map[string]interface{})
		if !ok {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}

		isAdmin, _ := userObj["is_admin"].(bool)
		if !isAdmin {
			slog.Warn("admin: non-admin rejected", "path", r.URL.Path)
			http.Error(w, `{"error":"forbidden: admin access required"}`, http.StatusForbidden)
			return
		}

		ctx := contextkeys.SetIsAdmin(r.Context(), true)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
