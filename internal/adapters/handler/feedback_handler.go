package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/HelpingPeopleNow/backend/internal/adapters/ratelimit"
	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
)

const maxFeedbackMessageLength = 2000

// FeedbackHandler handles user-submitted feedback.
type FeedbackHandler struct {
	repo        ports.FeedbackRepository
	notifier    ports.Notifier
	rateLimiter *ratelimit.RateLimiter
}

func NewFeedbackHandler(repo ports.FeedbackRepository, notifier ports.Notifier, rateLimiter *ratelimit.RateLimiter) *FeedbackHandler {
	return &FeedbackHandler{repo: repo, notifier: notifier, rateLimiter: rateLimiter}
}

type feedbackRequest struct {
	Message  string `json:"message"`
	PageURL  string `json:"page_url"`
	Category string `json:"category"`
}

// Submit handles POST /api/v1/feedback — any authenticated user.
func (h *FeedbackHandler) Submit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)

	var req feedbackRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if req.Message == "" || len(req.Message) > maxFeedbackMessageLength {
		slog.Warn("feedback: bad request", "error", "message must be 1–2000 chars", "len", len(req.Message))
		writeError(w, http.StatusBadRequest, "message must be 1–2000 chars")
		return
	}
	if req.PageURL == "" || len(req.PageURL) > 2048 {
		slog.Warn("feedback: bad request", "error", "page_url must be 1–2048 chars", "len", len(req.PageURL))
		writeError(w, http.StatusBadRequest, "page_url must be 1–2048 chars")
		return
	}
	if req.Category == "" {
		req.Category = "general"
	}
	if !core.ValidCategories[req.Category] {
		slog.Warn("feedback: bad request", "error", "invalid category", "category", req.Category)
		writeError(w, http.StatusBadRequest, "invalid category")
		return
	}

	userID := contextkeys.GetUserID(r.Context())

	// Apply rate limiting per user (when logged in) or per IP (when anonymous).
	if h.rateLimiter != nil {
		limitKey := userID
		if limitKey == "" {
			limitKey = clientIP(r)
		}
		if !h.rateLimiter.Allow(limitKey) {
			slog.Warn("feedback: rate limit hit", "key", limitKey)
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
	}

	fb := &core.Feedback{
		UserID:   userID,
		PageURL:  req.PageURL,
		Message:  req.Message,
		Category: req.Category,
		Status:   "open",
	}

	if err := h.repo.Create(fb); err != nil {
		slog.Error("feedback: create failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save feedback")
		return
	}
	slog.Info("feedback: submit ok", "user_id", userID, "category", req.Category, "id", fb.ID)

	// Resolve user email for the Telegram notification only when logged in.
	if userID != "" {
		if email, err := h.repo.GetUserEmail(userID); err == nil && email != "" {
			fb.Email = email
		}
	}

	// Fire-and-forget Telegram notification. Non-blocking: if it
	// fails, the feedback is still saved.
	if h.notifier != nil {
		go func() {
			if err := h.notifier.SendFeedbackAlert(fb); err != nil {
				slog.Warn("feedback: telegram notification failed", "error", err)
			}
		}()
	}

	writeJSON(w, http.StatusCreated, fb)
}

// clientIP returns the best-effort client IP for rate limiting.
// It prefers the X-Forwarded-For header (set by Traefik) and falls back
// to the connection's RemoteAddr. Only the first address in the header
// is used to keep the key stable.
func clientIP(r *http.Request) string {
	fwd := r.Header.Get("X-Forwarded-For")
	if fwd != "" {
		if idx := strings.Index(fwd, ","); idx != -1 {
			fwd = strings.TrimSpace(fwd[:idx])
		}
		if fwd != "" {
			return fwd
		}
	}
	return r.RemoteAddr
}
