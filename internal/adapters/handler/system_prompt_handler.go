package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
)

type SystemPromptHandler struct {
	prompts ports.SystemPromptRepository
}

func NewSystemPromptHandler(prompts ports.SystemPromptRepository) *SystemPromptHandler {
	return &SystemPromptHandler{prompts: prompts}
}

type systemPromptsDTO struct {
	WorkerProfilePrompt          string `json:"worker_profile_prompt"`
	ClientProfilePrompt          string `json:"client_profile_prompt"`
	FindTraderSearchPrompt       string `json:"find_trader_search_prompt"`
	FindTraderPresentationPrompt string `json:"find_trader_presentation_prompt"`
	LLMProvider                  string `json:"llm_provider"`
	UpdatedAt                    string `json:"updated_at"`
}

func toSystemDTO(sp *core.SystemPrompt) systemPromptsDTO {
	return systemPromptsDTO{
		WorkerProfilePrompt:          sp.WorkerProfilePrompt,
		ClientProfilePrompt:          sp.ClientProfilePrompt,
		FindTraderSearchPrompt:       sp.FindTraderSearchPrompt,
		FindTraderPresentationPrompt: sp.FindTraderPresentationPrompt,
		LLMProvider:                  sp.LLMProvider,
		UpdatedAt:                    sp.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

var validColumns = map[string]string{
	"worker_profile":           "worker_profile_prompt",
	"client_profile":           "client_profile_prompt",
	"find_trader_search":       "find_trader_search_prompt",
	"find_trader_presentation": "find_trader_presentation_prompt",
	"provider":                 "llm_provider",
}

type updateSystemReq struct {
	Content string `json:"content"`
}

func (h *SystemPromptHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	slog.Info("system-prompt: request", "method", r.Method, "path", r.URL.Path)
	w.Header().Set("Content-Type", "application/json")

	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	var col string
	if len(parts) >= 5 && parts[4] != "" {
		col = parts[4]
	}

	switch r.Method {
	case http.MethodGet:
		sp, err := h.prompts.Get(r.Context())
		if err != nil {
			slog.Error("system-prompt: load failed", "error", err)
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		writeJSON(w, http.StatusOK, toSystemDTO(sp))

	case http.MethodPut:
		if col == "" {
			writeError(w, http.StatusBadRequest, "column name required, e.g. /api/v1/system-prompts/helper")
			return
		}
		columnName, ok := validColumns[col]
		if !ok {
			writeError(w, http.StatusBadRequest, "unknown column: "+col)
			return
		}

		var req updateSystemReq
		// P2-4 (audit): reject unknown fields. The system prompts admin
		// UI sends exactly one field (`content`); anything else is a
		// probe or client bug and we surface it as 400.
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if col != "provider" && req.Content == "" {
			writeError(w, http.StatusBadRequest, "content cannot be empty")
			return
		}

		sp, err := h.prompts.Update(r.Context(), columnName, req.Content)
		if err != nil {
			slog.Error("system-prompt: update failed", "col", columnName, "error", err)
			writeError(w, http.StatusInternalServerError, "update failed: "+err.Error())
			return
		}
		slog.Info("system-prompt: updated", "col", columnName)
		writeJSON(w, http.StatusOK, toSystemDTO(sp))

	case http.MethodOptions:
		w.WriteHeader(http.StatusOK)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
