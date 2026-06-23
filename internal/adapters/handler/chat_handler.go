package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
	"github.com/HelpingPeopleNow/backend/internal/services"
)

type ChatHandler struct {
	intakeService *services.IntakeService
	searchService *services.SearchService
	prompts       ports.SystemPromptRepository
}

func NewChatHandler(
	intakeService *services.IntakeService,
	searchService *services.SearchService,
	prompts ports.SystemPromptRepository,
) *ChatHandler {
	return &ChatHandler{
		intakeService: intakeService,
		searchService: searchService,
		prompts:       prompts,
	}
}

type chatRequest struct {
	Mode           string        `json:"mode"`
	Message        string        `json:"message"`
	History        []chatMessage `json:"history,omitempty"`
	ConversationID string        `json:"conversation_id,omitempty"`
	Lang           string        `json:"lang,omitempty"`
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

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message cannot be empty")
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

	slog.Info("chat request", "mode", mode, "msg_len", len(req.Message), "history_len", len(req.History), "conv_id", req.ConversationID)
	IncrChatRequests(mode)

	userID := contextkeys.GetUserID(r.Context())
	provider := ""
	if sp, err := h.prompts.Get(r.Context()); err == nil {
		provider = sp.LLMProvider
	}
	history := convertHistory(req.History)

	switch mode {
	case "worker_intake":
		result, err := h.intakeService.ProcessIntake(r.Context(), userID, services.IntakeModeWorker, req.Message, history, provider, req.Lang, req.ConversationID)
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
		result, err := h.intakeService.ProcessIntake(r.Context(), userID, services.IntakeModeClient, req.Message, history, provider, req.Lang, req.ConversationID)
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
		result, err := h.searchService.Search(r.Context(), userID, req.Message, history, provider, req.Lang, req.ConversationID)
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
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"answer":          result.Answer,
			"workers":         core.FindTraderCards(result.Workers),
			"conversation_id": result.ConversationID,
		})
	}
}
