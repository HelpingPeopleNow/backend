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
//	GET /api/v1/client/profile  →  returns the authenticated user's client profile
//	PUT /api/v1/client/profile  →  creates or updates the client profile
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
	case http.MethodPut:
		h.put(w, r, userID)
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
		// No profile yet — return empty
		json.NewEncoder(w).Encode(map[string]interface{}{
			"user_id": userID,
		})
		return
	}
	json.NewEncoder(w).Encode(toClientDTO(&cp))
}

func (h *ClientHandler) put(w http.ResponseWriter, r *http.Request, userID string) {
	var dto core.ClientProfileDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		slog.Warn("client: invalid JSON", "error", err)
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	cp := &core.ClientProfile{
		UserID:   userID,
		FullName: dto.FullName,
		Phone:    dto.Phone,
		City:     dto.City,
		Address:  dto.Address,
		Bio:      dto.Bio,
	}

	// Upsert
	var existing core.ClientProfile
	err := h.db.Where("user_id = ?", userID).First(&existing).Error
	if err == nil {
		cp.ID = existing.ID
		cp.CreatedAt = existing.CreatedAt
		err = h.db.Save(cp).Error
	} else {
		err = h.db.Create(cp).Error
	}
	if err != nil {
		slog.Error("client: save failed", "error", err)
		http.Error(w, `{"error":"save failed"}`, http.StatusInternalServerError)
		return
	}

	slog.Info("client: profile saved", "user_id", userID, "full_name", cp.FullName)

	h.db.Where("user_id = ?", userID).First(&cp)
	json.NewEncoder(w).Encode(toClientDTO(cp))
}

func toClientDTO(cp *core.ClientProfile) *core.ClientProfileDTO {
	return &core.ClientProfileDTO{
		ID:        cp.ID,
		UserID:    cp.UserID,
		FullName:  cp.FullName,
		Phone:     cp.Phone,
		City:      cp.City,
		Address:   cp.Address,
		Bio:       cp.Bio,
		CreatedAt: cp.CreatedAt,
		UpdatedAt: cp.UpdatedAt,
	}
}
