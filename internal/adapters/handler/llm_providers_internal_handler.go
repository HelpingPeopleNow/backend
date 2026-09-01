package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/HelpingPeopleNow/backend/internal/ports"
)

// LLMProvidersInternalHandler exposes the admin-selected LLM provider list
// for the helper deep-probe loop. Intended for Docker-network access only.
type LLMProvidersInternalHandler struct {
	Prompts ports.SystemPromptRepository
}

type llmProvidersResponse struct {
	Providers []string `json:"providers"`
}

func NewLLMProvidersInternalHandler(prompts ports.SystemPromptRepository) *LLMProvidersInternalHandler {
	return &LLMProvidersInternalHandler{Prompts: prompts}
}

func (h *LLMProvidersInternalHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sp, err := h.Prompts.Get(r.Context())
	if err != nil {
		slog.Warn("internal llm-providers: load failed", "error", err)
		http.Error(w, "failed to load providers", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(llmProvidersResponse{Providers: sp.ParsedProviders()})
}
