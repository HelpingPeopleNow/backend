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
//   GET  /api/v1/system-prompts          → returns the helper prompt
//   PUT  /api/v1/system-prompts/{name}   → updates the helper prompt
type SystemPromptHandler struct {
	db *gorm.DB
}

func NewSystemPromptHandler(db *gorm.DB) *SystemPromptHandler {
	return &SystemPromptHandler{db: db}
}

type systemPromptsDTO struct {
	HelperPrompt string `json:"helper_prompt"`
	UpdatedAt    string `json:"updated_at"`
}

func toSystemDTO(sp *core.SystemPrompt) systemPromptsDTO {
	return systemPromptsDTO{
		HelperPrompt: sp.HelperPrompt,
		UpdatedAt:    sp.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
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
		"helper": "helper_prompt",
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
	if req.Content == "" {
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
	slog.Info("system-prompt: updated")
	json.NewEncoder(w).Encode(toSystemDTO(&sp))
}
