package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/HelpingPeopleNow/backend/internal/ports"
)

type ClientHandler struct {
	profiles ports.ProfileRepository
}

func NewClientHandler(profiles ports.ProfileRepository) *ClientHandler {
	return &ClientHandler{profiles: profiles}
}

func (h *ClientHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	slog.Info("client: request", "method", r.Method, "path", r.URL.Path)
	w.Header().Set("Content-Type", "application/json")

	userID := contextkeys.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		cp, err := h.profiles.GetClientProfile(r.Context(), userID)
		if err != nil {
			slog.Error("client: load profile", "user_id", userID, "error", err)
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if cp == nil {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"user_id": userID})
			return
		}
		_ = json.NewEncoder(w).Encode(cp.ToDTO())

	case http.MethodDelete:
		if err := h.profiles.DeleteClientProfile(r.Context(), userID); err != nil {
			slog.Error("client: delete profile", "user_id", userID, "error", err)
			writeError(w, http.StatusInternalServerError, "delete failed")
			return
		}
		slog.Info("client: profile deleted", "user_id", userID)
		w.WriteHeader(http.StatusNoContent)

	case http.MethodOptions:
		w.WriteHeader(http.StatusOK)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
