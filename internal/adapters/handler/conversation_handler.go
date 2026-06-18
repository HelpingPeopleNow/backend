package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"gorm.io/gorm"
	"log/slog"
)

// ConversationHandler handles listing and fetching saved conversations.
type ConversationHandler struct {
	db *gorm.DB
}

// conversationListItem is the abbreviated conversation shown in listings.
type conversationListItem struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// conversationDetail is the full conversation including messages.
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

// NewConversationHandler creates a new ConversationHandler.
func NewConversationHandler(db *gorm.DB) *ConversationHandler {
	return &ConversationHandler{db: db}
}

// ServeHTTP dispatches GET requests for listing or fetching conversations.
// Routes:
//
//	GET /api/v1/conversations          — list (filtered by ?type=, ?limit=, ?offset=)
//	GET /api/v1/conversations/{id}     — get one by ID
func (h *ConversationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract conversation ID from path suffix: /api/v1/conversations/{id}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/conversations")
	path = strings.TrimPrefix(path, "/")
	convID := path

	if convID != "" {
		if r.Method != http.MethodGet {
			slog.Warn("conv-handler: invalid method for single conversation", "method", r.Method)
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		h.getOne(w, r, convID)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.list(w, r)
	default:
		slog.Warn("conv-handler: invalid method", "method", r.Method)
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// list returns a user's conversations, optionally filtered by type.
func (h *ConversationHandler) list(w http.ResponseWriter, r *http.Request) {
	userID := resolveUserIDFromSession(r, h.db)
	if userID == "" {
		slog.Warn("conv-handler: unauthorized list attempt")
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	convType := r.URL.Query().Get("type")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 20
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	offset := 0
	if offsetStr != "" {
		if v, err := strconv.Atoi(offsetStr); err == nil && v >= 0 {
			offset = v
		}
	}

	query := h.db.Model(&core.Conversation{}).Where("user_id = ?", userID)
	if convType != "" {
		query = query.Where("type = ?", convType)
	}

	var total int64
	query.Count(&total)

	var convs []core.Conversation
	if err := query.Order("updated_at DESC").Offset(offset).Limit(limit).Find(&convs).Error; err != nil {
		slog.Error("conv-handler: failed to list conversations", "error", err)
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
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

	json.NewEncoder(w).Encode(map[string]interface{}{
		"conversations": items,
		"total":         total,
		"limit":         limit,
		"offset":        offset,
	})
}

// getOne loads a conversation by ID and includes its messages from the messages table.
func (h *ConversationHandler) getOne(w http.ResponseWriter, r *http.Request, convID string) {
	userID := resolveUserIDFromSession(r, h.db)
	if userID == "" {
		slog.Warn("conv-handler: unauthorized getOne attempt", "convID", convID)
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var conv core.Conversation
	if err := h.db.Where("id = ? AND user_id = ?", convID, userID).First(&conv).Error; err != nil {
		slog.Warn("conv-handler: conversation not found", "convID", convID, "error", err)
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	// Load messages from the messages table
	var dbMessages []core.Message
	if err := h.db.Where("conversation_id = ?", convID).Order("created_at ASC").Find(&dbMessages).Error; err != nil {
		slog.Error("conv-handler: failed to load messages", "convID", convID, "error", err)
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}

	msgs := make([]msgItem, len(dbMessages))
	for i, m := range dbMessages {
		msgs[i] = msgItem{Role: m.Role, Content: m.Content}
	}

	detail := conversationDetail{
		ID:        conv.ID,
		Type:      conv.Type,
		Metadata:  conv.Metadata,
		Messages:  msgs,
		CreatedAt: conv.CreatedAt,
		UpdatedAt: conv.UpdatedAt,
	}

	json.NewEncoder(w).Encode(detail)
}

// resolveUserIDFromSession extracts the user ID from the better-auth session cookie.
func resolveUserIDFromSession(r *http.Request, db *gorm.DB) string {
	cookie, ok := sessionCookie(r)
	if !ok {
		slog.Debug("resolveUserIDFromSession: no session cookie")
		return ""
	}
	token := rawSessionToken(cookie)
	if token == "" {
		slog.Debug("resolveUserIDFromSession: empty session token")
		return ""
	}
	type dbSession struct {
		UserID string `gorm:"column:userId"`
	}
	var s dbSession
	err := db.Table("\"session\"").Where("token = ? AND \"expiresAt\" > NOW()", token).First(&s).Error
	if err != nil {
		slog.Debug("resolveUserIDFromSession: session not found", "error", err)
		return ""
	}
	return s.UserID
}
