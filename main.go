package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/HelpingPeopleNow/backend/database"
	"github.com/HelpingPeopleNow/backend/internal/adapters/handler"
	"github.com/HelpingPeopleNow/backend/internal/adapters/repository"
	"github.com/HelpingPeopleNow/backend/internal/service"
)

// contextKey is used for storing values in request context to avoid collisions.
type contextKey string

const sessionKey contextKey = "session"

// GetSession retrieves the session info stored in the request context by authMiddleware.
// Returns nil if no session info is present.
func GetSession(ctx context.Context) map[string]interface{} {
	v := ctx.Value(sessionKey)
	if v == nil {
		return nil
	}
	session, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	return session
}

// authMiddleware validates the better-auth-session cookie via the auth service.
// It skips validation for public endpoints (GET /health, GET /api/v1/hello)
// and stores session/user info in the request context on success.
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Public endpoints — skip session validation
		if r.Method == http.MethodGet && (r.URL.Path == "/health" || r.URL.Path == "/api/v1/hello") {
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie("better-auth-session")
		if err != nil {
			slog.Warn("auth: missing session cookie", "path", r.URL.Path)
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		// Build request to auth service, forwarding the session cookie
		authReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "http://auth:8083/api/auth/get-session", nil)
		if err != nil {
			slog.Error("auth: failed to create request", "error", err)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		authReq.AddCookie(cookie)

		client := &http.Client{Timeout: 5 * time.Second}
		authResp, err := client.Do(authReq)
		if err != nil {
			slog.Error("auth: session validation request failed", "error", err)
			http.Error(w, `{"error":"auth service unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		defer authResp.Body.Close()

		if authResp.StatusCode != http.StatusOK {
			slog.Warn("auth: invalid session", "status", authResp.StatusCode, "path", r.URL.Path)
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		// Parse session info from the auth service response
		var sessionInfo map[string]interface{}
		if err := json.NewDecoder(authResp.Body).Decode(&sessionInfo); err != nil {
			slog.Error("auth: failed to decode session response", "error", err)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		slog.Info("auth: session validated", "path", r.URL.Path)

		// Store session info in request context and continue
		ctx := context.WithValue(r.Context(), sessionKey, sessionInfo)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		slog.Info("request started",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		)
		next.ServeHTTP(w, r)
		slog.Info("request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	slog.Info("starting backend", "port", port)

	db, err := database.Connect()
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	slog.Info("database connected")

	repo := repository.NewGormPromptRepository(db)
	svc := service.NewPromptService(repo)
	promptHandler := handler.NewPromptHandler(svc)
	chatHandler := handler.NewChatHandler()
	sysPromptHandler := handler.NewSystemPromptHandler(db)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.Handle("/api/v1/prompt-helpers", authMiddleware(promptHandler))
	mux.Handle("/api/v1/prompt-helpers/", authMiddleware(promptHandler))
	mux.Handle("/api/v1/prompts", promptHandler)
	mux.Handle("/api/v1/prompts/", promptHandler)
	mux.Handle("/api/v1/system-prompts", sysPromptHandler)
	mux.Handle("/api/v1/system-prompts/", sysPromptHandler)
	mux.Handle("/api/v1/chat", chatHandler)

	handler := loggingMiddleware(corsMiddleware(mux))

	slog.Info("listening", "addr", ":"+port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
