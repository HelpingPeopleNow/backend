package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"gorm.io/gorm"
)

// ClientHandler serves the client profile (one per user).
//
//	GET /api/v1/client/profile     →  returns the authenticated user's client profile
//	DELETE /api/v1/client/profile  →  clears the authenticated user's client profile
type ClientHandler struct {
	db *gorm.DB
}

func NewClientHandler(db *gorm.DB) *ClientHandler {
	return &ClientHandler{db: db}
}

func (h *ClientHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	userID := extractUserIDFromRequest(r, h.db)
	if userID == "" {
		slog.Warn("client: no user session")
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.get(w, userID)
	case http.MethodDelete:
		h.delete(w, userID)
	case http.MethodOptions:
		w.WriteHeader(http.StatusOK)
	default:
		slog.Warn("client: invalid method", "method", r.Method)
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (h *ClientHandler) get(w http.ResponseWriter, userID string) {
	var cp core.ClientProfile
	err := h.db.Where("user_id = ?", userID).First(&cp).Error
	if err != nil {
		if err != gorm.ErrRecordNotFound {
			slog.Error("client: failed to load profile", "user_id", userID, "error", err)
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		// No profile yet — return empty
		json.NewEncoder(w).Encode(map[string]interface{}{
			"user_id": userID,
		})
		return
	}
	json.NewEncoder(w).Encode(toClientDTO(&cp))
}

func (h *ClientHandler) delete(w http.ResponseWriter, userID string) {
	if err := h.db.Where("user_id = ?", userID).Delete(&core.ClientProfile{}).Error; err != nil {
		slog.Error("client: delete failed", "error", err)
		http.Error(w, `{"error":"delete failed"}`, http.StatusInternalServerError)
		return
	}
	slog.Info("client: profile deleted", "user_id", userID)
	w.WriteHeader(http.StatusNoContent)
}

func toClientDTO(cp *core.ClientProfile) *core.ClientProfileDTO {
	return &core.ClientProfileDTO{
		ID:               cp.ID,
		UserID:           cp.UserID,
		FullName:         cp.FullName,
		Phone:            cp.Phone,
		City:             cp.City,
		Address:          cp.Address,
		Bio:              cp.Bio,
		PreferredContact: cp.PreferredContact,
		PropertyType:     cp.PropertyType,
		Notes:            cp.Notes,
		CreatedAt:        cp.CreatedAt,
		UpdatedAt:        cp.UpdatedAt,
	}
}
