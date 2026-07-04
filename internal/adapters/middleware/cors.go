package middleware

import (
	"net/http"
	"os"
	"strings"
)

// getAllowedOrigins reads the ALLOWED_ORIGINS env var and returns the parsed allowlist.
// Called at request time so tests can override via os.Setenv.
func getAllowedOrigins() []string {
	envOrigins := os.Getenv("ALLOWED_ORIGINS")
	if envOrigins == "" {
		return nil
	}
	var allowed []string
	for _, o := range strings.Split(envOrigins, ",") {
		origin := strings.TrimSpace(o)
		if origin != "" {
			allowed = append(allowed, origin)
		}
	}
	return allowed
}

// isAllowedOrigin checks if the given origin is in the allowlist.
func isAllowedOrigin(origin string) bool {
	for _, allowed := range getAllowedOrigins() {
		if allowed == origin {
			return true
		}
	}
	return false
}

// CORS returns a middleware that handles CORS with an allowlist.
// Only configured origins are allowed with credentials.
// For OPTIONS preflight requests, responds with 204 No Content.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Only set CORS headers if origin is in the allowlist
		if origin != "" && isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}

		// Always set these headers for allowed origins (they're safe)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}