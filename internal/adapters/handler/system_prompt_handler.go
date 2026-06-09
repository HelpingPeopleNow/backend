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
	db                  *gorm.DB
	onUpdate            func(string) // helper prompt content refresh callback
	onProviderUpdate    func(string) // llm provider refresh callback
	onWorkerProfileUpd  func(string) // worker profile prompt refresh callback
}

func NewSystemPromptHandler(db *gorm.DB, onUpdate ...func(string)) *SystemPromptHandler {
	h := &SystemPromptHandler{db: db}
	if len(onUpdate) > 0 && onUpdate[0] != nil {
		h.onUpdate = onUpdate[0]
	}
	if len(onUpdate) > 1 && onUpdate[1] != nil {
		h.onProviderUpdate = onUpdate[1]
	}
	if len(onUpdate) > 2 && onUpdate[2] != nil {
		h.onWorkerProfileUpd = onUpdate[2]
	}
	return h
}

type systemPromptsDTO struct {
	HelperPrompt        string `json:"helper_prompt"`
	WorkerProfilePrompt string `json:"worker_profile_prompt"`
	LLMProvider         string `json:"llm_provider"`
	UpdatedAt           string `json:"updated_at"`
}

func toSystemDTO(sp *core.SystemPrompt) systemPromptsDTO {
	return systemPromptsDTO{
		HelperPrompt:        sp.HelperPrompt,
		WorkerProfilePrompt: sp.WorkerProfilePrompt,
		LLMProvider:         sp.LLMProvider,
		UpdatedAt:           sp.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
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
	err := h.db.First(&sp, 1).Error
	if err != nil {
		slog.Warn("system-prompt: row 1 not found, returning defaults", "error", err)
		json.NewEncoder(w).Encode(systemPromptsDTO{})
		return
	}
	slog.Info("system-prompt: loaded", "helper_prompt_len", len(sp.HelperPrompt))
	json.NewEncoder(w).Encode(toSystemDTO(&sp))
}

func (h *SystemPromptHandler) update(w http.ResponseWriter, r *http.Request, col string) {
	validCols := map[string]string{
		"helper":         "helper_prompt",
		"worker_profile": "worker_profile_prompt",
		"provider":       "llm_provider",
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
	err := h.db.Exec(
		`INSERT INTO system_prompts (id, `+columnName+`, updated_at)
		 VALUES (1, $1, NOW())
		 ON CONFLICT (id) DO UPDATE SET `+columnName+` = EXCLUDED.`+columnName+`, updated_at = NOW()`,
		req.Content,
	).Error
	if err != nil {
		slog.Error("system-prompt: update failed", "error", err)
		http.Error(w, `{"error":"update failed: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	var sp core.SystemPrompt
	h.db.First(&sp, 1)
	slog.Info("system-prompt: updated", "col", columnName)

	// Refresh the appropriate backend cache
	if columnName == "helper_prompt" && h.onUpdate != nil {
		h.onUpdate(req.Content)
		slog.Info("system-prompt: backend cache refreshed")
	}
	if columnName == "llm_provider" && h.onProviderUpdate != nil {
		h.onProviderUpdate(req.Content)
		slog.Info("system-prompt: backend provider cache refreshed", "provider", req.Content)
	}
	if columnName == "worker_profile_prompt" && h.onWorkerProfileUpd != nil {
		h.onWorkerProfileUpd(req.Content)
		slog.Info("system-prompt: worker profile prompt cache refreshed")
	}

	json.NewEncoder(w).Encode(toSystemDTO(&sp))
}
