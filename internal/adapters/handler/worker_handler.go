package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/HelpingPeopleNow/backend/internal/ports"
)

type WorkerHandler struct {
	profiles ports.ProfileRepository
}

func NewWorkerHandler(profiles ports.ProfileRepository) *WorkerHandler {
	return &WorkerHandler{profiles: profiles}
}

func (h *WorkerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	userID := contextkeys.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		wp, err := h.profiles.GetWorkerProfile(r.Context(), userID)
		if err != nil {
			slog.Error("worker: load profile", "user_id", userID, "error", err)
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if wp == nil {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"user_id": userID})
			return
		}
		_ = json.NewEncoder(w).Encode(wp.ToDTO())

	case http.MethodDelete:
		if err := h.profiles.DeleteWorkerProfile(r.Context(), userID); err != nil {
			slog.Error("worker: delete profile", "user_id", userID, "error", err)
			writeError(w, http.StatusInternalServerError, "delete failed")
			return
		}
		slog.Info("worker: profile deleted", "user_id", userID)
		w.WriteHeader(http.StatusNoContent)

	case http.MethodOptions:
		w.WriteHeader(http.StatusOK)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
