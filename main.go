package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/HelpingPeopleNow/backend/database"
	"github.com/HelpingPeopleNow/backend/internal/adapters/handler"
	"github.com/HelpingPeopleNow/backend/internal/adapters/repository"
	"github.com/HelpingPeopleNow/backend/internal/service"
)

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
	mux.Handle("/api/v1/prompt-helpers", promptHandler)
	mux.Handle("/api/v1/prompt-helpers/", promptHandler)
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
