package main

import (
	"log"
	"net/http"
	"os"

	"github.com/HelpingPeopleNow/backend/database"
	"github.com/HelpingPeopleNow/backend/internal/handler"
	"github.com/HelpingPeopleNow/backend/internal/repository"
	"github.com/HelpingPeopleNow/backend/internal/service"
)

func main() {
	// ── Database ──────────────────────────────────────────
	db, err := database.Connect()
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	log.Println("Connected to PostgreSQL via GORM")

	// ── DDD wiring: Repository → Service → Handler ────────
	promptRepo := repository.NewGormPromptRepository(db)
	promptSvc := service.NewPromptService(promptRepo)
	promptH := handler.NewPromptHandler(promptSvc)

	// ── Router ─────────────────────────────────────────────
	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Hello (legacy)
	mux.HandleFunc("/api/v1/hello", helloHandler)

	// Prompts CRUD
	mux.Handle("/api/v1/prompts", promptH)
	mux.Handle("/api/v1/prompts/", promptH)

	// ── Start ──────────────────────────────────────────────
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	log.Printf("Starting backend on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
