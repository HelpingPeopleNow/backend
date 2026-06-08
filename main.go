package main

import (
	"log"
	"net/http"
	"os"

	"github.com/HelpingPeopleNow/backend/database"
	"github.com/HelpingPeopleNow/backend/internal/adapters/handler"
	"github.com/HelpingPeopleNow/backend/internal/adapters/repository"
	"github.com/HelpingPeopleNow/backend/internal/service"
)

// corsMiddleware adds CORS headers for cross-port requests from the frontend.
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
	// ── Database ──────────────────────────────────────────
	db, err := database.Connect()
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	log.Println("Connected to PostgreSQL via GORM")

	// ── Hexagonal wiring ───────────────────────────────────
	// Outbound adapter → port implementation → use case → inbound adapter
	promptRepo := repository.NewGormPromptRepository(db) // adapter implementing port
	promptSvc := service.NewPromptService(promptRepo)     // use case depends on port
	promptH := handler.NewPromptHandler(promptSvc)        // inbound adapter
	sysPromptH := handler.NewSystemPromptHandler(db)      // direct GORM handler for column-based prompts
	chatH := handler.NewChatHandler()                       // proxy to helper service

	// ── Router ─────────────────────────────────────────────
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.Handle("/api/v1/prompt-helpers", promptH)
	mux.Handle("/api/v1/prompt-helpers/", promptH)
	mux.Handle("/api/v1/prompts", promptH)
	mux.Handle("/api/v1/prompts/", promptH)
	mux.Handle("/api/v1/system-prompts", sysPromptH)
	mux.Handle("/api/v1/system-prompts/", sysPromptH)
	mux.Handle("/api/v1/chat", chatH)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	log.Printf("Starting backend on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, corsMiddleware(mux)))
}
