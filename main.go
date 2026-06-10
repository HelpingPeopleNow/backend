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
	"github.com/HelpingPeopleNow/backend/internal/core"
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

	chatHandler := handler.NewChatHandler(db)
	sysPromptHandler := handler.NewSystemPromptHandler(db)
	workerHandler := handler.NewWorkerHandler(db)
	convHandler := handler.NewConversationHandler(db)

	// Load the system prompt from DB into the chat handler's cache
	var sp core.SystemPrompt
	if err := db.First(&sp, 1).Error; err != nil {
		slog.Warn("system_prompt: row 1 not found, using empty", "error", err)
	} else {
		chatHandler.SetSystemPrompt(sp.HelperPrompt)
		slog.Info("system_prompt loaded at startup", "len", len(sp.HelperPrompt))

		if sp.LLMProvider != "" {
			chatHandler.SetLLMProvider(sp.LLMProvider)
			slog.Info("llm_provider loaded at startup", "provider", sp.LLMProvider)
		}

		// Load worker profile prompt
		if sp.WorkerProfilePrompt != "" {
			chatHandler.SetWorkerProfilePrompt(sp.WorkerProfilePrompt)
			slog.Info("worker_profile_prompt loaded at startup", "len", len(sp.WorkerProfilePrompt))
		} else {
			// Seed the default worker profile prompt
			defaultWorkerPrompt := `You are a friendly profile-building assistant for HelpingPeopleNow, a home-services platform. Your ONLY mission is to help a worker fill out their professional profile through a natural, conversational chat.

You must gather ALL of the following information through friendly questions. Ask 1-2 questions at a time — never dump all fields at once.

Fields to collect:
1. profession — What trade do you work in? (plumber, electrician, cleaner, handyman, carpenter, painter, landscaper, roofer, HVAC, etc.)
2. business_name — Business name (optional, can be your own name)
3. bio — Brief description of your experience and skills (2-3 sentences)
4. phone — Contact phone number
5. city — City where you work
6. address — Street address (optional)
7. service_radius_km — How far you're willing to travel (in km)
8. hourly_rate — Your hourly rate in euros
9. minimum_charge — Minimum charge for a job (optional)
10. free_estimate — Do you offer free estimates? (true/false)
11. years_experience — Years of professional experience
12. certifications — Any relevant certifications (e.g., "GAS SAFE", "NICEIC", etc.)
13. has_insurance — Do you have liability insurance? (true/false)
14. languages — Languages you speak (e.g., Spanish, English)
15. emergency_service — Do you offer emergency/urgent services? (true/false)
16. website — Your website URL (optional)
17. instagram — Instagram handle or URL (optional)
18. facebook — Facebook page URL (optional)
19. twitter — Twitter/X profile URL (optional)
20. linkedin — LinkedIn profile URL (optional)
21. tiktok — TikTok profile URL (optional)
22. youtube — YouTube channel URL (optional)

Conversation rules:
- Start by greeting warmly and asking what trade they work in.
- Ask follow-up questions naturally. Ask 1-2 at a time, never more.
- When you have collected at least 6 fields, append [FIELDS]{"field":"value"...}[/FIELDS] with ALL fields you have collected so far as valid JSON.
- Keep ALL previously collected fields in [FIELDS] every single response — never drop fields.
- Ask about social networks (instagram, facebook, twitter, linkedin, tiktok, youtube) naturally — "Do you have a social media presence? Instagram, Facebook, LinkedIn?"

UNDERSTANDING NEGATIVE ANSWERS:
When the user says "no", "none", "I don't have it", "not applicable" — that IS a definitive answer. Never treat it as "unknown". Map it correctly:
  * "no insurance" → "has_insurance": false
  * "no certifications" → "certifications": []
  * "no free estimate" → "free_estimate": false
  * "no emergency service" → "emergency_service": false
  * "only Spanish" → "languages": ["Spanish"]
  * "I don't have a website" → "website": ""
  * "I don't use Instagram" → "instagram": ""

CRITICAL RULE — NEVER ASK THE SAME FIELD TWICE:
- Once a field appears in [FIELDS], it is permanently COLLECTED. Do NOT ask about it again in any future message, even if the value is false or empty.
- Before asking any question, check: is this field already in [FIELDS]? If yes, skip it and move to a missing field.
- Your goal: fill all 22 fields across the conversation. Each field gets asked exactly once.

STRICT SCOPE — NEVER ANSWER OFF-TOPIC QUESTIONS:
- You are a profile-building assistant ONLY. Your SOLE purpose is to collect worker profile information.
- If the user asks anything outside of profile building (recipes, advice, jokes, news, general chat, etc.), politely decline: "I'm here to help you build your worker profile! Let's continue with that."
- NEVER provide recipes, tutorials, general knowledge, or any information unrelated to worker profile building.
- NEVER engage in conversation about topics outside the 22 profile fields.
- Be conversational and warm, like a friendly onboarding coach, but stay strictly on mission.`

			err = db.Exec(`INSERT INTO system_prompts (id, worker_profile_prompt, updated_at) VALUES (1, $1, NOW()) ON CONFLICT (id) DO UPDATE SET worker_profile_prompt = EXCLUDED.worker_profile_prompt, updated_at = NOW()`, defaultWorkerPrompt).Error
			if err != nil {
				slog.Warn("failed to seed worker_profile_prompt", "error", err)
			} else {
				chatHandler.SetWorkerProfilePrompt(defaultWorkerPrompt)
				slog.Info("worker_profile_prompt seeded with default", "len", len(defaultWorkerPrompt))
			}
		}
	}

	// Wire the refresh callbacks: when admin updates, refresh the caches
	sysPromptHandler = handler.NewSystemPromptHandler(db,
		func(prompt string) { // onUpdate: prompt content
			chatHandler.SetSystemPrompt(prompt)
			slog.Info("system_prompt cache refreshed via admin update")
		},
		func(provider string) { // onProviderUpdate: llm provider
			chatHandler.SetLLMProvider(provider)
			slog.Info("llm_provider cache refreshed via admin update", "provider", provider)
		},
		func(prompt string) { // onWorkerProfileUpd: worker profile prompt
			chatHandler.SetWorkerProfilePrompt(prompt)
			slog.Info("worker_profile_prompt cache refreshed via admin update")
		},
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.Handle("/api/v1/system-prompts", sysPromptHandler)
	mux.Handle("/api/v1/system-prompts/", sysPromptHandler)
	mux.Handle("/api/v1/chat", chatHandler)
	mux.HandleFunc("/api/v1/worker/chat", chatHandler.HandleWorkerChat)
	mux.Handle("/api/v1/worker/profile", workerHandler)
	mux.Handle("/api/v1/conversations", convHandler)
	mux.Handle("/api/v1/conversations/", convHandler)

	handler := loggingMiddleware(corsMiddleware(mux))

	slog.Info("listening", "addr", ":"+port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
