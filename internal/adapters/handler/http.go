package handler

import (
	"encoding/json"
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

// promptDTO is a flat JSON view of the domain entity (avoids exposing DB tags).
type promptDTO struct {
	ID        uint   `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Category  string `json:"category"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func toDTO(p *core.Prompt) promptDTO {
	return promptDTO{
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

	switch r.Method {
	case http.MethodGet:
		if idStr != "" && idStr != "prompts" {
			id, err := strconv.ParseUint(idStr, 10, 64)
			if err != nil {
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
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}
		h.update(w, r, uint(id))

	case http.MethodDelete:
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}
		h.delete(w, uint(id))

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (h *PromptHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	prompt, err := h.svc.Create(req.Title, req.Content, req.Category)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(toDTO(prompt))
}

func (h *PromptHandler) getByID(w http.ResponseWriter, id uint) {
	prompt, err := h.svc.GetByID(id)
	if err != nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(toDTO(prompt))
}

func (h *PromptHandler) list(w http.ResponseWriter) {
	prompts, err := h.svc.List()
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if prompts == nil {
		prompts = []core.Prompt{}
	}
	dtos := make([]promptDTO, len(prompts))
	for i, p := range prompts {
		dtos[i] = toDTO(&p)
	}
	json.NewEncoder(w).Encode(dtos)
}

func (h *PromptHandler) update(w http.ResponseWriter, r *http.Request, id uint) {
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	prompt, err := h.svc.Update(id, req.Title, req.Content, req.Category)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(toDTO(prompt))
}

func (h *PromptHandler) delete(w http.ResponseWriter, id uint) {
	if err := h.svc.Delete(id); err != nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
