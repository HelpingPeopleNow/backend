package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/HelpingPeopleNow/backend/internal/ports"
)

type ConversationHandler struct {
	chats ports.ChatRepository
}

func NewConversationHandler(chats ports.ChatRepository) *ConversationHandler {
	return &ConversationHandler{chats: chats}
}

type conversationListItem struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type conversationDetail struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	Messages  []msgItem       `json:"messages"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type msgItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (h *ConversationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	userID := contextkeys.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/conversations")
	path = strings.TrimPrefix(path, "/")
	convID := path

	if convID != "" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		IncrConversation("get")
		h.getOne(w, r, userID, convID)
		return
	}

	switch r.Method {
	case http.MethodGet:
		IncrConversation("list")
		h.list(w, r, userID)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *ConversationHandler) list(w http.ResponseWriter, r *http.Request, userID string) {
	convType := r.URL.Query().Get("type")
	limit := parseIntParam(r, "limit", 20)
	offset := parseIntParam(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}

	convs, total, err := h.chats.ListConversations(r.Context(), userID, convType, limit, offset)
	if err != nil {
		slog.Error("conv-handler: list failed", "error", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	items := make([]conversationListItem, len(convs))
	for i, c := range convs {
		items[i] = conversationListItem{
			ID:        c.ID,
			Type:      c.Type,
			Metadata:  c.Metadata,
			CreatedAt: c.CreatedAt,
			UpdatedAt: c.UpdatedAt,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"conversations": items,
		"total":         total,
		"limit":         limit,
		"offset":        offset,
	})
}

func (h *ConversationHandler) getOne(w http.ResponseWriter, r *http.Request, userID, convID string) {
	conv, err := h.chats.GetConversation(r.Context(), userID, convID)
	if err != nil {
		slog.Error("conv-handler: getOne failed", "convID", convID, "error", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if conv == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	msgs, err := h.chats.GetMessages(r.Context(), convID)
	if err != nil {
		slog.Error("conv-handler: messages load failed", "convID", convID, "error", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	msgItems := make([]msgItem, len(msgs))
	for i, m := range msgs {
		msgItems[i] = msgItem{Role: m.Role, Content: m.Content}
	}

	writeJSON(w, http.StatusOK, conversationDetail{
		ID:        conv.ID,
		Type:      conv.Type,
		Metadata:  conv.Metadata,
		Messages:  msgItems,
		CreatedAt: conv.CreatedAt,
		UpdatedAt: conv.UpdatedAt,
	})
}
