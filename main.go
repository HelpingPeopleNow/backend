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

	// ── Router ─────────────────────────────────────────────
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/api/v1/hello", helloHandler)
	mux.Handle("/api/v1/prompt-helpers", promptH)
	mux.Handle("/api/v1/prompt-helpers/", promptH)
	mux.Handle("/api/v1/prompts", promptH)
	mux.Handle("/api/v1/prompts/", promptH)
	mux.Handle("/api/v1/system-prompts", sysPromptH)
	mux.Handle("/api/v1/system-prompts/", sysPromptH)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	log.Printf("Starting backend on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
