package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
	"github.com/HelpingPeopleNow/backend/internal/services"
)

const (
	searchRateLimit  = 10       // P1-1 (audit): max chat calls per minute per user across all modes (F2)
	maxSearchInput   = 2 * 1024 // 2 KB cap on search message length (F10)
	maxBodyBytes     = 64 << 10 // P1-1 (audit): 64 KiB cap on request body so a giant payload can't OOM the process or balloon LLM token cost (F4)
	maxMessageLength = 8000     // P1-1 (audit): defense-in-depth per-message char cap
)

type ChatHandler struct {
	intakeService     *services.IntakeService
	searchService     *services.SearchService
	prompts           ports.SystemPromptRepository
	searchRateLimiter SearchRateLimiter
}

// SearchRateLimiter is the interface for per-user search rate limiting (F2).
type SearchRateLimiter interface {
	Allow(key string) bool
}

func NewChatHandler(
	intakeService *services.IntakeService,
	searchService *services.SearchService,
	prompts ports.SystemPromptRepository,
	searchRateLimiter SearchRateLimiter,
) *ChatHandler {
	return &ChatHandler{
		intakeService:     intakeService,
		searchService:     searchService,
		prompts:           prompts,
		searchRateLimiter: searchRateLimiter,
	}
}

type chatRequest struct {
	Mode           string        `json:"mode"`
	Message        string        `json:"message"`
	History        []chatMessage `json:"history,omitempty"`
	ConversationID string        `json:"conversation_id,omitempty"`
	Lang           string        `json:"lang,omitempty"`
	Latitude       *float64      `json:"latitude,omitempty"`
	Longitude      *float64      `json:"longitude,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (h *ChatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// P1-1 (audit): cap inbound body so a giant payload can't OOM the
	// process or balloon LLM token cost (F4). Distinguish *http.MaxBytesError
	// from JSON parse errors so clients get an actionable status code.
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	var req chatRequest
	// P2-4 (audit): reject unknown JSON fields. Frontend ChatRequest type
	// in frontend/src/services/chat.ts sends exactly the 7 fields below —
	// any extra field is a client bug or a probe payload and we surface
	// it as 400 rather than silently ignoring it.
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message cannot be empty")
		return
	}

	// P1-1 (audit): defense-in-depth per-message char cap.
	if len(req.Message) > maxMessageLength {
		writeError(w, http.StatusBadRequest, "message too long")
		return
	}

	mode := req.Mode
	if mode == "" {
		mode = "worker_intake"
	}
	if mode != "worker_intake" && mode != "client_intake" && mode != "search" {
		writeError(w, http.StatusBadRequest, "invalid mode, must be worker_intake, client_intake, or search")
		return
	}

	userID := contextkeys.GetUserID(r.Context())

	// P1-1 (audit): per-user rate limit covers ALL chat modes (F4 cost
	// blowout). Search-mode truncation in the case branch is a separate
	// 2 KB input cap on the message content, not a rate-limit gate.
	if h.searchRateLimiter != nil && userID != "" && !h.searchRateLimiter.Allow(userID) {
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded, try again in 1 minute")
		return
	}

	slog.Info("chat request", "mode", mode, "msg_len", len(req.Message), "history_len", len(req.History), "conv_id", req.ConversationID)
	IncrChatRequests(mode)

	provider := ""
	if sp, err := h.prompts.Get(r.Context()); err == nil {
		provider = sp.LLMProvider
	}
	history := convertHistory(req.History)

	// P3-1 (audit / F2): observe wall-clock from mode dispatch start to
	// write so the chat_llm_duration_seconds histogram (already rendered
	// by metrics_handler.go) is populated. `defer` covers every return
	// path including LLMError 503s — we want latency distribution across
	// successes AND failures so failures don't hide in the long tail.
	// Search mode measures both Pass 1 + Pass 2 wall-clock; that's the
	// user-visible latency and the right thing to alert on.
	//
	// The deferred closure takes provider+mode as explicit args (snapshot
	// at registration time) so a future edit that reassigns either inside
	// the switch cannot leak a post-assignment value into the histogram.
	llmStart := time.Now()
	defer func(providerSnap, modeSnap string, start time.Time) {
		ObserveChatLLMDuration(providerSnap, modeSnap, time.Since(start).Seconds())
	}(provider, mode, llmStart)

	switch mode {
	case "worker_intake":
		result, err := h.intakeService.ProcessIntake(r.Context(), userID, services.IntakeModeWorker, req.Message, history, provider, req.Lang, req.ConversationID, req.Latitude, req.Longitude)
		if err != nil {
			handleLLMError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"answer":          result.Answer,
			"detected_fields": result.FieldsRaw,
			"conversation_id": result.ConversationID,
		})

	case "client_intake":
		result, err := h.intakeService.ProcessIntake(r.Context(), userID, services.IntakeModeClient, req.Message, history, provider, req.Lang, req.ConversationID, req.Latitude, req.Longitude)
		if err != nil {
			handleLLMError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"answer":          result.Answer,
			"detected_fields": result.FieldsRaw,
			"conversation_id": result.ConversationID,
		})

	case "search":
		// F10: cap search input length at 2 KB (after JSON-decode truncation).
		if len(req.Message) > maxSearchInput {
			req.Message = req.Message[:maxSearchInput]
			slog.Warn("search input truncated", "user_id", userID, "original_len", len(req.Message), "capped_to", maxSearchInput)
		}
		// Per-user rate limit is enforced upstream in ServeHTTP so it
		// covers intake + search uniformly (P1-1 audit fix).
		result, err := h.searchService.Search(r.Context(), userID, req.Message, history, provider, req.Lang, req.ConversationID, req.Latitude, req.Longitude)
		if err != nil {
			handleLLMError(w, err)
			return
		}
		// VECTOR_SEARCH_PLAN §12.3 / Idea C — wire the orphaned vector
		// metrics here in the handler layer (services → adapters/handler
		// would be a cycle; handlers → services is the allowed direction).
		// Branch is post-fact (repo-reported), so the counter reflects
		// what actually happened — not the intent.
		IncrVectorSearch(result.Branch)
		if result.Branch == "vector" && result.TopScore > 0 {
			ObserveVectorScore(result.TopScore)
		}
		// F4: increment embed_failures_total when embed failure caused ILIKE fallback
		if result.Branch == "ilike_embed_failed" {
			IncrEmbedFailures()
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"answer":          result.Answer,
			"workers":         core.FindTraderCards(result.Workers),
			"conversation_id": result.ConversationID,
		})
	}
}
