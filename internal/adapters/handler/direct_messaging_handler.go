package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/adapters/ratelimit"
	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
)

// DirectMessagingHandler handles all direct-messaging REST + SSE endpoints.
type DirectMessagingHandler struct {
	dm      ports.DirectMessageRepository
	profs   ports.ProfileRepository
	broker  ports.Broker
	limiter *ratelimit.RateLimiter
}

func NewDirectMessagingHandler(
	dm ports.DirectMessageRepository,
	profs ports.ProfileRepository,
	broker ports.Broker,
	limiter *ratelimit.RateLimiter,
) *DirectMessagingHandler {
	return &DirectMessagingHandler{dm: dm, profs: profs, broker: broker, limiter: limiter}
}

// ── Route dispatch ───────────────────────────────────────────────────────────

func (h *DirectMessagingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID := contextkeys.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	path := r.URL.Path

	switch {
	// GET /api/v1/workers/{workerProfileID}/contact
	case strings.HasPrefix(path, "/api/v1/workers/") && strings.HasSuffix(path, "/contact") && r.Method == http.MethodGet:
		workerProfileID := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/workers/"), "/contact")
		h.getOrCreateContact(w, r, userID, workerProfileID)

	// GET /api/v1/direct-messages/stream (SSE)
	case path == "/api/v1/direct-messages/stream" && r.Method == http.MethodGet:
		if h.broker == nil {
			writeError(w, http.StatusNotImplemented, "sse broker not available")
			return
		}
		h.streamSSE(w, r, userID)

	// GET /api/v1/direct-messages/since  (polling fallback)
	case path == "/api/v1/direct-messages/since" && r.Method == http.MethodGet:
		h.pollSince(w, r, userID)

	// GET /api/v1/direct-messages  (list inbox)
	case path == "/api/v1/direct-messages" && r.Method == http.MethodGet:
		h.listConversations(w, r, userID)

	// POST /api/v1/direct-messages/{convID}/messages
	case strings.HasPrefix(path, "/api/v1/direct-messages/") && strings.HasSuffix(path, "/messages") && r.Method == http.MethodPost:
		convID := extractSegment(path, "/api/v1/direct-messages/", "/messages")
		h.sendMessage(w, r, userID, convID)

	// GET /api/v1/direct-messages/{convID}/messages
	case strings.HasPrefix(path, "/api/v1/direct-messages/") && strings.HasSuffix(path, "/messages") && r.Method == http.MethodGet:
		convID := extractSegment(path, "/api/v1/direct-messages/", "/messages")
		h.getMessages(w, r, userID, convID)

	// PATCH /api/v1/direct-messages/{convID}/read
	case strings.HasPrefix(path, "/api/v1/direct-messages/") && strings.HasSuffix(path, "/read") && r.Method == http.MethodPatch:
		convID := extractSegment(path, "/api/v1/direct-messages/", "/read")
		h.markRead(w, r, userID, convID)

	// POST /api/v1/direct-messages/{convID}/archive
	case strings.HasPrefix(path, "/api/v1/direct-messages/") && strings.HasSuffix(path, "/archive") && r.Method == http.MethodPost:
		convID := extractSegment(path, "/api/v1/direct-messages/", "/archive")
		h.archive(w, r, userID, convID)

	// POST /api/v1/direct-messages/{convID}/block
	case strings.HasPrefix(path, "/api/v1/direct-messages/") && strings.HasSuffix(path, "/block") && r.Method == http.MethodPost:
		convID := extractSegment(path, "/api/v1/direct-messages/", "/block")
		h.block(w, r, userID, convID)

	// POST /api/v1/direct-messages/{convID}/report
	case strings.HasPrefix(path, "/api/v1/direct-messages/") && strings.HasSuffix(path, "/report") && r.Method == http.MethodPost:
		convID := extractSegment(path, "/api/v1/direct-messages/", "/report")
		h.report(w, r, userID, convID)

	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

// extractSegment extracts a path segment between two known strings.
// e.g. "/api/v1/direct-messages/abc-123/messages" → "abc-123"
func extractSegment(path, prefix, suffix string) string {
	s := strings.TrimPrefix(path, prefix)
	s = strings.TrimSuffix(s, suffix)
	return s
}

// ── Endpoints ────────────────────────────────────────────────────────────────

// GET /api/v1/workers/:workerProfileID/contact
func (h *DirectMessagingHandler) getOrCreateContact(
	w http.ResponseWriter, r *http.Request, userID, workerProfileID string,
) {
	if workerProfileID == "" {
		writeError(w, http.StatusBadRequest, "worker_profile_id required")
		return
	}

	// Load worker profile
	wp, err := h.dm.GetWorkerByProfileID(r.Context(), workerProfileID)
	if err != nil {
		slog.Error("dm: load worker", "worker_profile_id", workerProfileID, "error", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if wp == nil {
		writeError(w, http.StatusNotFound, "worker_not_found")
		return
	}

	// Prevent self-messaging
	if wp.UserID == userID {
		writeError(w, http.StatusBadRequest, "cannot_message_self")
		return
	}

	conv, created, err := h.dm.GetOrCreateConversation(r.Context(), userID, workerProfileID)
	if err != nil {
		slog.Error("dm: get-or-create conversation", "error", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	IncrDMSent("contact")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"conversation_id": conv.ID,
		"worker": map[string]interface{}{
			"id":            wp.ID,
			"user_id":       wp.UserID,
			"profession":    wp.Profession,
			"business_name": wp.BusinessName,
			"city":          wp.City,
		},
		"created": created,
	})
}

// GET /api/v1/direct-messages  (list inbox)
func (h *DirectMessagingHandler) listConversations(
	w http.ResponseWriter, r *http.Request, userID string,
) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "active"
	}
	limit := parseIntParam(r, "limit", 20)
	if limit < 1 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	// Determine role: check if user is a worker (has worker profile) or client
	role := core.SenderRoleClient
	if wp, err := h.profs.GetWorkerProfile(r.Context(), userID); err == nil && wp != nil {
		role = core.SenderRoleWorker
	}

	convs, err := h.dm.ListConversations(r.Context(), userID, role, status, limit, nil)
	if err != nil {
		slog.Error("dm: list conversations", "user_id", userID, "error", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	items := make([]map[string]interface{}, 0, len(convs))
	for _, c := range convs {
		items = append(items, h.conversationItem(r.Context(), c, userID, role))
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"conversations": items,
	})
}

// GET /api/v1/direct-messages/:convID/messages
func (h *DirectMessagingHandler) getMessages(
	w http.ResponseWriter, r *http.Request, userID, convID string,
) {
	if convID == "" {
		writeError(w, http.StatusBadRequest, "conversation_id required")
		return
	}

	// Verify participant
	ok, role, err := h.dm.IsParticipant(r.Context(), convID, userID)
	if err != nil {
		slog.Error("dm: check participant", "conv_id", convID, "error", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if !ok {
		writeError(w, http.StatusForbidden, "not_participant")
		return
	}

	limit := parseIntParam(r, "limit", 50)
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	before := r.URL.Query().Get("before")

	msgs, err := h.dm.GetMessages(r.Context(), convID, limit, before)
	if err != nil {
		slog.Error("dm: get messages", "conv_id", convID, "error", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	// Mark messages from the other party as read
	h.dm.MarkRead(r.Context(), convID, role)

	// Reverse to chronological order for display
	result := make([]core.DirectMessage, len(msgs))
	for i, m := range msgs {
		result[len(msgs)-1-i] = m
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": result,
		"has_more": len(result) >= limit,
	})
}

// POST /api/v1/direct-messages/:convID/messages
func (h *DirectMessagingHandler) sendMessage(
	w http.ResponseWriter, r *http.Request, userID, convID string,
) {
	if convID == "" {
		writeError(w, http.StatusBadRequest, "conversation_id required")
		return
	}

	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(req.Body) == 0 || len(req.Body) > core.MaxDirectMessageLength {
		writeError(w, http.StatusBadRequest, "body must be 1-4000 characters")
		return
	}

	// Rate limit: 30 messages per minute per user
	if h.limiter != nil && !h.limiter.Allow(userID+":send") {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}

	// Verify participant
	ok, role, err := h.dm.IsParticipant(r.Context(), convID, userID)
	if err != nil {
		slog.Error("dm: check participant", "conv_id", convID, "error", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if !ok {
		writeError(w, http.StatusForbidden, "not_participant")
		return
	}

	// Check if blocked
	conv, err := h.dm.GetConversation(r.Context(), convID)
	if err != nil || conv == nil {
		writeError(w, http.StatusNotFound, "conversation_not_found")
		return
	}
	if conv.IsBlocked() {
		writeError(w, http.StatusForbidden, "conversation blocked")
		return
	}

	msg := &core.DirectMessage{
		ConversationID: convID,
		SenderID:       userID,
		SenderRole:     role,
		Body:           req.Body,
	}
	if err := h.dm.SendMessage(r.Context(), msg); err != nil {
		slog.Error("dm: send message", "conv_id", convID, "error", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	IncrDMSent(role)

	// Push SSE event to participants (async — don't block the HTTP response)
	go h.pushSSE(conv, ports.Event{
		Type: "message",
		Payload: map[string]interface{}{
			"id":              msg.ID,
			"conversation_id": msg.ConversationID,
			"sender_id":       msg.SenderID,
			"sender_role":     msg.SenderRole,
			"body":            msg.Body,
			"created_at":      msg.CreatedAt.Format(time.RFC3339),
		},
	})

	slog.Info("dm: message sent",
		"conv_id", convID,
		"sender_role", role,
		"body_len", len(req.Body),
	)

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":              msg.ID,
		"conversation_id": msg.ConversationID,
		"sender_role":     msg.SenderRole,
		"body":            msg.Body,
		"created_at":      msg.CreatedAt,
	})
}

// PATCH /api/v1/direct-messages/:convID/read
func (h *DirectMessagingHandler) markRead(
	w http.ResponseWriter, r *http.Request, userID, convID string,
) {
	if convID == "" {
		writeError(w, http.StatusBadRequest, "conversation_id required")
		return
	}

	ok, role, err := h.dm.IsParticipant(r.Context(), convID, userID)
	if err != nil || !ok {
		writeError(w, http.StatusForbidden, "not_participant")
		return
	}

	count, err := h.dm.MarkRead(r.Context(), convID, role)
	if err != nil {
		slog.Error("dm: mark read", "conv_id", convID, "error", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	// Push SSE read receipt to participants (async)
	conv, err := h.dm.GetConversation(r.Context(), convID)
	if err == nil && conv != nil {
		go h.pushSSE(conv, ports.Event{
			Type: "read",
			Payload: map[string]interface{}{
				"conversation_id": convID,
				"read_by":         userID,
			},
		})
	}

	slog.Debug("dm: marked read", "conv_id", convID, "count", count)
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/direct-messages/:convID/archive
func (h *DirectMessagingHandler) archive(
	w http.ResponseWriter, r *http.Request, userID, convID string,
) {
	if convID == "" {
		writeError(w, http.StatusBadRequest, "conversation_id required")
		return
	}

	ok, role, err := h.dm.IsParticipant(r.Context(), convID, userID)
	if err != nil || !ok {
		writeError(w, http.StatusForbidden, "not_participant")
		return
	}

	if err := h.dm.ArchiveConversation(r.Context(), convID, userID, role); err != nil {
		slog.Error("dm: archive", "conv_id", convID, "error", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	slog.Info("dm: conversation archived", "conv_id", convID, "role", role)
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/direct-messages/:convID/block
func (h *DirectMessagingHandler) block(
	w http.ResponseWriter, r *http.Request, userID, convID string,
) {
	if convID == "" {
		writeError(w, http.StatusBadRequest, "conversation_id required")
		return
	}

	ok, _, err := h.dm.IsParticipant(r.Context(), convID, userID)
	if err != nil || !ok {
		writeError(w, http.StatusForbidden, "not_participant")
		return
	}

	if err := h.dm.BlockConversation(r.Context(), convID); err != nil {
		slog.Error("dm: block", "conv_id", convID, "error", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	slog.Info("dm: conversation blocked", "conv_id", convID)
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/direct-messages/{convID}/report
func (h *DirectMessagingHandler) report(
	w http.ResponseWriter, r *http.Request, userID, convID string,
) {
	if convID == "" {
		writeError(w, http.StatusBadRequest, "conversation_id required")
		return
	}

	ok, _, err := h.dm.IsParticipant(r.Context(), convID, userID)
	if err != nil || !ok {
		writeError(w, http.StatusForbidden, "not_participant")
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	slog.Warn("dm: conversation reported",
		"conv_id", convID,
		"reported_by", userID,
		"reason", req.Reason,
	)

	w.WriteHeader(http.StatusNoContent)
}

// GET /api/v1/direct-messages/since  (polling fallback)
func (h *DirectMessagingHandler) pollSince(
	w http.ResponseWriter, r *http.Request, userID string,
) {
	tsStr := r.URL.Query().Get("ts")
	if tsStr == "" {
		writeError(w, http.StatusBadRequest, "ts query parameter required")
		return
	}
	since, err := time.Parse(time.RFC3339, tsStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ts format (use ISO 8601)")
		return
	}

	msgs, err := h.dm.PollSince(r.Context(), userID, since)
	if err != nil {
		slog.Error("dm: poll since", "user_id", userID, "error", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	if msgs == nil {
		msgs = []core.DirectMessage{}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"messages":    msgs,
		"server_time": time.Now().UTC().Format(time.RFC3339),
	})
}

// ── SSE /stream endpoint ────────────────────────────────────────────────────

func (h *DirectMessagingHandler) streamSSE(w http.ResponseWriter, r *http.Request, userID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	ch, err := h.broker.Subscribe(ctx, userID)
	if err != nil {
		slog.Error("sse: subscribe failed", "user_id", userID, "error", err)
		writeError(w, http.StatusInternalServerError, "subscribe failed")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	slog.Info("sse: connection opened", "user_id", userID)

	// Heartbeat ticker — keeps connection alive for proxies (Traefik/nginx idle timeout ~60s)
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("sse: connection closed", "user_id", userID)
			return

		case <-heartbeat.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()

		case event, ok := <-ch:
			if !ok {
				slog.Info("sse: channel closed", "user_id", userID)
				return
			}
			data, err := json.Marshal(event.Payload)
			if err != nil {
				slog.Warn("sse: marshal event", "error", err)
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
			flusher.Flush()
		}
	}
}

// pushSSE broadcasts an event to all participants of a conversation.
func (h *DirectMessagingHandler) pushSSE(conv *core.DirectConversation, event ports.Event) {
	if h.broker == nil || conv == nil {
		return
	}
	// Notify the client
	_ = h.broker.Publish(conv.ClientID, event)
	// Notify the worker (resolve worker_profiles.id → user_id)
	wp, err := h.dm.GetWorkerByProfileID(context.Background(), conv.WorkerProfileID)
	if err == nil && wp != nil {
		_ = h.broker.Publish(wp.UserID, event)
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// conversationItem builds the inbox item for a conversation.
func (h *DirectMessagingHandler) conversationItem(
	ctx context.Context, c core.DirectConversation, userID, role string,
) map[string]interface{} {
	// Determine who the "other party" is
	otherRole := "client"
	otherName := ""
	otherID := ""

	if role == core.SenderRoleClient {
		otherRole = "worker"
		// Load worker profile for display
		wp, err := h.dm.GetWorkerByProfileID(ctx, c.WorkerProfileID)
		if err == nil && wp != nil {
			otherName = wp.BusinessName
			if otherName == "" {
				otherName = wp.Profession
			}
			otherID = wp.UserID
		}
	} else {
		otherID = c.ClientID
		otherName = c.ClientID // workers see client ID for now; profile display deferred
	}

	// Unread count for this user
	unread := c.ClientUnreadCount
	if role == core.SenderRoleWorker {
		unread = c.WorkerUnreadCount
	}

	item := map[string]interface{}{
		"id": c.ID,
		"other_party": map[string]interface{}{
			"id":   otherID,
			"name": otherName,
			"role": otherRole,
		},
		"unread_count": unread,
		"status":       c.Status,
	}

	if c.LastMessageAt != nil {
		item["last_message"] = map[string]interface{}{
			"preview": c.LastMessagePreview,
			"at":      c.LastMessageAt.Format(time.RFC3339),
		}
	}

	return item
}
