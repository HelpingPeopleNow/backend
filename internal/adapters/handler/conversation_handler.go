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
	Title     string          `json:"title"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// NewConversationHandler creates a new ConversationHandler.
func NewConversationHandler(db *gorm.DB) *ConversationHandler {
	return &ConversationHandler{db: db}
}

// ServeHTTP dispatches GET requests for listing or fetching conversations.
// Routes:
//   GET /api/v1/conversations          — list (filtered by ?type=, ?limit=, ?offset=)
//   GET /api/v1/conversations/{id}     — get one by ID
func (h *ConversationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract conversation ID from path suffix: /api/v1/conversations/{id}
	// The path looks like /api/v1/conversations/<uuid>
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/conversations")
	path = strings.TrimPrefix(path, "/")
	convID := path

	if convID != "" {
		// It's a request for a specific conversation
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
// GET /api/v1/conversations?type=worker&limit=5&offset=0
func (h *ConversationHandler) list(w http.ResponseWriter, r *http.Request) {
	userID := resolveUserIDFromSession(r, h.db)
	if userID == "" {
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
			Title:     c.Title,
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

// getOne returns a single conversation by ID (must belong to the requesting user).
func (h *ConversationHandler) getOne(w http.ResponseWriter, r *http.Request, convID string) {
	userID := resolveUserIDFromSession(r, h.db)
	if userID == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var conv core.Conversation
	if err := h.db.Where("id = ? AND user_id = ?", convID, userID).First(&conv).Error; err != nil {
		slog.Warn("conv-handler: conversation not found", "convID", convID, "error", err)
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(conv)
}

// resolveUserIDFromSession extracts the user ID from the better-auth session cookie.
func resolveUserIDFromSession(r *http.Request, db *gorm.DB) string {
	cookie, err := r.Cookie("better-auth-session")
	if err != nil {
		return ""
	}
	tokenParts := []byte(cookie.Value)
	// The cookie is "<token>.<encrypted_payload>" — split to get the raw token
	dotIdx := -1
	for i, b := range tokenParts {
		if b == '.' {
			dotIdx = i
			break
		}
	}
	if dotIdx < 0 {
		return ""
	}
	token := string(tokenParts[:dotIdx])
	if token == "" {
		return ""
	}
	type dbSession struct {
		UserID string `gorm:"column:userId"`
	}
	var s dbSession
	err = db.Table("\"session\"").Where("token = ? AND \"expiresAt\" > NOW()", token).First(&s).Error
	if err != nil {
		slog.Debug("resolveUserIDFromSession: session not found", "error", err)
		return ""
	}
	return s.UserID
}
