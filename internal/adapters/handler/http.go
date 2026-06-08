package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/service"
)

// PromptHandler is the inbound adapter — translates HTTP into use-case calls.
type PromptHandler struct {
	svc *service.PromptService
}

func NewPromptHandler(svc *service.PromptService) *PromptHandler {
	return &PromptHandler{svc: svc}
}

type createRequest struct {
	Title    string `json:"title"`
	Content  string `json:"content"`
	Category string `json:"category"`
}

type updateRequest struct {
	Title    string `json:"title,omitempty"`
	Content  string `json:"content,omitempty"`
	Category string `json:"category,omitempty"`
}

type promptHelperDTO struct {
	ID        uint   `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Category  string `json:"category"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func toDTO(p *core.PromptHelper) promptHelperDTO {
	return promptHelperDTO{
		ID:        p.ID,
		Title:     p.Title,
		Content:   p.Content,
		Category:  p.Category,
		CreatedAt: p.CreatedAt.Format(time.RFC3339),
		UpdatedAt: p.UpdatedAt.Format(time.RFC3339),
	}
}

func (h *PromptHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	idStr := ""
	if len(parts) > 0 {
		idStr = parts[len(parts)-1]
	}

	slog.Info("prompt-helpers request", "method", r.Method, "path", r.URL.Path)

	switch r.Method {
	case http.MethodGet:
		if idStr != "" && idStr != "prompt-helpers" && idStr != "prompts" {
			id, err := strconv.ParseUint(idStr, 10, 64)
			if err != nil {
				slog.Warn("prompt-helpers: invalid ID", "idStr", idStr)
				http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
				return
			}
			h.getByID(w, uint(id))
		} else {
			h.list(w)
		}

	case http.MethodPost:
		h.create(w, r)

	case http.MethodPatch:
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			slog.Warn("prompt-helpers: invalid ID for update", "idStr", idStr)
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}
		h.update(w, r, uint(id))

	case http.MethodDelete:
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			slog.Warn("prompt-helpers: invalid ID for delete", "idStr", idStr)
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}
		h.delete(w, uint(id))

	default:
		slog.Warn("prompt-helpers: invalid method", "method", r.Method)
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (h *PromptHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Warn("prompt-helpers: invalid create JSON", "error", err)
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	slog.Info("prompt-helpers: creating", "title", req.Title, "category", req.Category)
	prompt, err := h.svc.Create(req.Title, req.Content, req.Category)
	if err != nil {
		slog.Error("prompt-helpers: create failed", "error", err)
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	slog.Info("prompt-helpers: created", "id", prompt.ID)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(toDTO(prompt))
}

func (h *PromptHandler) getByID(w http.ResponseWriter, id uint) {
	slog.Info("prompt-helpers: getting by ID", "id", id)
	prompt, err := h.svc.GetByID(id)
	if err != nil {
		slog.Warn("prompt-helpers: not found", "id", id)
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(toDTO(prompt))
}

func (h *PromptHandler) list(w http.ResponseWriter) {
	slog.Info("prompt-helpers: listing all")
	prompts, err := h.svc.List()
	if err != nil {
		slog.Error("prompt-helpers: list failed", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if prompts == nil {
		prompts = []core.PromptHelper{}
	}
	dtos := make([]promptHelperDTO, len(prompts))
	for i, p := range prompts {
		dtos[i] = toDTO(&p)
	}
	slog.Info("prompt-helpers: listed", "count", len(dtos))
	json.NewEncoder(w).Encode(dtos)
}

func (h *PromptHandler) update(w http.ResponseWriter, r *http.Request, id uint) {
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Warn("prompt-helpers: invalid update JSON", "error", err)
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	slog.Info("prompt-helpers: updating", "id", id)
	prompt, err := h.svc.Update(id, req.Title, req.Content, req.Category)
	if err != nil {
		slog.Error("prompt-helpers: update failed", "id", id, "error", err)
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	slog.Info("prompt-helpers: updated", "id", id)
	json.NewEncoder(w).Encode(toDTO(prompt))
}

func (h *PromptHandler) delete(w http.ResponseWriter, id uint) {
	slog.Info("prompt-helpers: deleting", "id", id)
	if err := h.svc.Delete(id); err != nil {
		slog.Error("prompt-helpers: delete failed", "id", id, "error", err)
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	slog.Info("prompt-helpers: deleted", "id", id)
	w.WriteHeader(http.StatusNoContent)
}
