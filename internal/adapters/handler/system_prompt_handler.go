package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"gorm.io/gorm"
)

// SystemPromptHandler serves the column-based system_prompts singleton table.
//   GET  /api/v1/system-prompts          → returns the helper prompt + llm provider
//   PUT  /api/v1/system-prompts/helper   → updates the helper prompt
//   PUT  /api/v1/system-prompts/provider → updates the llm provider
type SystemPromptHandler struct {
	db                            *gorm.DB
	onProviderUpdate              func(string)
	onWorkerProfileUpd            func(string)
	onClientProfileUpd            func(string)
	onFindTraderSearchUpd         func(string)
	onFindTraderPresentationUpd   func(string)
}

func NewSystemPromptHandler(db *gorm.DB, onUpdate ...func(string)) *SystemPromptHandler {
	h := &SystemPromptHandler{db: db}
	if len(onUpdate) > 0 && onUpdate[0] != nil {
		h.onProviderUpdate = onUpdate[0]
	}
	if len(onUpdate) > 1 && onUpdate[1] != nil {
		h.onWorkerProfileUpd = onUpdate[1]
	}
	if len(onUpdate) > 2 && onUpdate[2] != nil {
		h.onClientProfileUpd = onUpdate[2]
	}
	if len(onUpdate) > 3 && onUpdate[3] != nil {
		h.onFindTraderSearchUpd = onUpdate[3]
	}
	if len(onUpdate) > 4 && onUpdate[4] != nil {
		h.onFindTraderPresentationUpd = onUpdate[4]
	}
	return h
}

func (h *SystemPromptHandler) SetOnProviderUpdate(fn func(string)) {
	h.onProviderUpdate = fn
}

func (h *SystemPromptHandler) SetOnWorkerProfileUpdate(fn func(string)) {
	h.onWorkerProfileUpd = fn
}

func (h *SystemPromptHandler) SetOnClientProfileUpdate(fn func(string)) {
	h.onClientProfileUpd = fn
}

func (h *SystemPromptHandler) SetOnFindTraderSearchUpdate(fn func(string)) {
	h.onFindTraderSearchUpd = fn
}

func (h *SystemPromptHandler) SetOnFindTraderPresentationUpdate(fn func(string)) {
	h.onFindTraderPresentationUpd = fn
}

type systemPromptsDTO struct {
	WorkerProfilePrompt         string `json:"worker_profile_prompt"`
	ClientProfilePrompt         string `json:"client_profile_prompt"`
	FindTraderSearchPrompt      string `json:"find_trader_search_prompt"`
	FindTraderPresentationPrompt string `json:"find_trader_presentation_prompt"`
	LLMProvider                 string `json:"llm_provider"`
	UpdatedAt                   string `json:"updated_at"`
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

type updateSystemReq struct {
	Content string `json:"content"`
}

func (h *SystemPromptHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	var col string
	if len(parts) >= 5 && parts[4] != "" {
		col = parts[4]
	}

	slog.Info("system-prompt request", "method", r.Method, "col", col)

	switch r.Method {
	case http.MethodGet:
		h.get(w)

	case http.MethodPut:
		if col == "" {
			slog.Warn("system-prompt: missing column name")
			http.Error(w, `{"error":"column name required, e.g. /api/v1/system-prompts/helper"}`, http.StatusBadRequest)
			return
		}
		h.update(w, r, col)

	case http.MethodOptions:
		w.WriteHeader(http.StatusOK)

	default:
		slog.Warn("system-prompt: invalid method", "method", r.Method)
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (h *SystemPromptHandler) get(w http.ResponseWriter) {
	var sp core.SystemPrompt
	if err := h.db.FirstOrCreate(&sp).Error; err != nil {
		slog.Error("system-prompt: FirstOrCreate failed", "error", err)
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}
	slog.Info("system-prompt: loaded", "worker_profile_prompt_len", len(sp.WorkerProfilePrompt))
	json.NewEncoder(w).Encode(toSystemDTO(&sp))
}

func (h *SystemPromptHandler) update(w http.ResponseWriter, r *http.Request, col string) {
	validCols := map[string]string{
		"worker_profile":       "worker_profile_prompt",
		"client_profile":       "client_profile_prompt",
		"find_trader_search":   "find_trader_search_prompt",
		"find_trader_presentation": "find_trader_presentation_prompt",
		"provider":             "llm_provider",
	}
	columnName, ok := validCols[col]
	if !ok {
		slog.Warn("system-prompt: unknown column", "col", col)
		http.Error(w, `{"error":"unknown column: `+col+`"}`, http.StatusBadRequest)
		return
	}

	var req updateSystemReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Warn("system-prompt: invalid JSON", "error", err)
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if col != "provider" && req.Content == "" {
		slog.Warn("system-prompt: empty content")
		http.Error(w, `{"error":"content cannot be empty"}`, http.StatusBadRequest)
		return
	}

	slog.Info("system-prompt: updating", "col", columnName, "content_len", len(req.Content))

	// Ensure the singleton row exists, then update
	var sp core.SystemPrompt
	h.db.FirstOrCreate(&sp)

	// Update the specific column
 updates := map[string]interface{}{columnName: req.Content, "updated_at": gorm.Expr("NOW()")}
	if err := h.db.Model(&sp).Updates(updates).Error; err != nil {
		slog.Error("system-prompt: update failed", "error", err)
		http.Error(w, `{"error":"update failed: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	h.db.First(&sp)
	slog.Info("system-prompt: updated", "col", columnName)

	// Refresh the appropriate backend cache
	if columnName == "llm_provider" && h.onProviderUpdate != nil {
		h.onProviderUpdate(req.Content)
		slog.Info("system-prompt: backend provider cache refreshed", "provider", req.Content)
	}
	if columnName == "worker_profile_prompt" && h.onWorkerProfileUpd != nil {
		h.onWorkerProfileUpd(req.Content)
		slog.Info("system-prompt: worker profile prompt cache refreshed")
	}
	if columnName == "client_profile_prompt" && h.onClientProfileUpd != nil {
		h.onClientProfileUpd(req.Content)
		slog.Info("system-prompt: client profile prompt cache refreshed")
	}
	if columnName == "find_trader_search_prompt" && h.onFindTraderSearchUpd != nil {
		h.onFindTraderSearchUpd(req.Content)
		slog.Info("system-prompt: find-trader search prompt cache refreshed")
	}
	if columnName == "find_trader_presentation_prompt" && h.onFindTraderPresentationUpd != nil {
		h.onFindTraderPresentationUpd(req.Content)
		slog.Info("system-prompt: find-trader presentation prompt cache refreshed")
	}

	json.NewEncoder(w).Encode(toSystemDTO(&sp))
}
