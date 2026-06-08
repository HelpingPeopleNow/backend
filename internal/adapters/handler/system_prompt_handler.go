package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"gorm.io/gorm"
)

// SystemPromptHandler serves the column-based system_prompts singleton table.
//   GET  /api/v1/system-prompts          → returns all prompt columns
//   PUT  /api/v1/system-prompts/{name}   → updates the named column (helper, frontend, backend, …)
type SystemPromptHandler struct {
	db *gorm.DB
}

func NewSystemPromptHandler(db *gorm.DB) *SystemPromptHandler {
	return &SystemPromptHandler{db: db}
}

type systemPromptsDTO struct {
	HelperPrompt   string `json:"helper_prompt"`
	FrontendPrompt string `json:"frontend_prompt"`
	BackendPrompt  string `json:"backend_prompt"`
	UpdatedAt      string `json:"updated_at"`
}

func toSystemDTO(sp *core.SystemPrompt) systemPromptsDTO {
	return systemPromptsDTO{
		HelperPrompt:   sp.HelperPrompt,
		FrontendPrompt: sp.FrontendPrompt,
		BackendPrompt:  sp.BackendPrompt,
		UpdatedAt:      sp.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

type updateSystemReq struct {
	Content string `json:"content"`
}

func (h *SystemPromptHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract column name from the URL path
	// /api/v1/system-prompts         → no column (list)
	// /api/v1/system-prompts/helper  → column = "helper"
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	var col string
	if len(parts) >= 5 && parts[4] != "" {
		col = parts[4]
	}

	switch r.Method {
	case http.MethodGet:
		h.get(w)

	case http.MethodPut:
		if col == "" {
			http.Error(w, `{"error":"column name required, e.g. /api/v1/system-prompts/helper"}`, http.StatusBadRequest)
			return
		}
		h.update(w, r, col)

	case http.MethodOptions:
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (h *SystemPromptHandler) get(w http.ResponseWriter) {
	var sp core.SystemPrompt
	err := h.db.First(&sp, 1).Error
	if err != nil {
		// Row 1 doesn't exist yet — return empty defaults
		json.NewEncoder(w).Encode(systemPromptsDTO{})
		return
	}
	json.NewEncoder(w).Encode(toSystemDTO(&sp))
}

func (h *SystemPromptHandler) update(w http.ResponseWriter, r *http.Request, col string) {
	// Validate the column name is one we know about
	validCols := map[string]string{
		"helper":   "helper_prompt",
		"frontend": "frontend_prompt",
		"backend":  "backend_prompt",
	}
	columnName, ok := validCols[col]
	if !ok {
		http.Error(w, `{"error":"unknown column: `+col+`"}`, http.StatusBadRequest)
		return
	}

	var req updateSystemReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.Content == "" {
		http.Error(w, `{"error":"content cannot be empty"}`, http.StatusBadRequest)
		return
	}

	// Upsert row 1 and update the specific column
	err := h.db.Exec(
		`INSERT INTO system_prompts (id, `+columnName+`, updated_at)
		 VALUES (1, $1, NOW())
		 ON CONFLICT (id) DO UPDATE SET `+columnName+` = EXCLUDED.`+columnName+`, updated_at = NOW()`,
		req.Content,
	).Error
	if err != nil {
		http.Error(w, `{"error":"update failed: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	// Return the updated row
	var sp core.SystemPrompt
	h.db.First(&sp, 1)
	json.NewEncoder(w).Encode(toSystemDTO(&sp))
}
