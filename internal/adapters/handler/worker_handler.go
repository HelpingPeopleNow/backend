package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"gorm.io/gorm"
)

// WorkerHandler serves the worker profile (one per user).
//
//	GET /api/v1/worker/profile     →  returns the authenticated user's worker profile
//	DELETE /api/v1/worker/profile  →  clears the authenticated user's worker profile
type WorkerHandler struct {
	db *gorm.DB
}

func NewWorkerHandler(db *gorm.DB) *WorkerHandler {
	return &WorkerHandler{db: db}
}

func (h *WorkerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract user ID from the session cookie via auth service
	userID := extractUserIDFromRequest(r, h.db)
	if userID == "" {
		slog.Warn("worker: no user session")
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
		slog.Warn("worker: invalid method", "method", r.Method)
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (h *WorkerHandler) get(w http.ResponseWriter, userID string) {
	var wp core.WorkerProfile
	err := h.db.Where("user_id = ?", userID).First(&wp).Error
	if err != nil {
		// No profile yet — return empty, not 404 (frontend shows the form)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"user_id": userID,
		})
		return
	}
	json.NewEncoder(w).Encode(toWorkerDTO(&wp))
}

func (h *WorkerHandler) delete(w http.ResponseWriter, userID string) {
	if err := h.db.Where("user_id = ?", userID).Delete(&core.WorkerProfile{}).Error; err != nil {
		slog.Error("worker: delete failed", "error", err)
		http.Error(w, `{"error":"delete failed"}`, http.StatusInternalServerError)
		return
	}
	slog.Info("worker: profile deleted", "user_id", userID)
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func toWorkerDTO(wp *core.WorkerProfile) *core.WorkerProfileDTO {
	var certs, langs []string
	json.Unmarshal([]byte(wp.Certifications), &certs)
	json.Unmarshal([]byte(wp.Languages), &langs)
	var social []core.SocialLink
	json.Unmarshal([]byte(wp.SocialLinks), &social)
	if certs == nil {
		certs = []string{}
	}
	if langs == nil {
		langs = []string{}
	}
	if social == nil {
		social = []core.SocialLink{}
	}
	return &core.WorkerProfileDTO{
		ID:               wp.ID,
		UserID:           wp.UserID,
		Profession:       wp.Profession,
		BusinessName:     wp.BusinessName,
		Bio:              wp.Bio,
		Phone:            wp.Phone,
		City:             wp.City,
		ServiceRadiusKm:  wp.ServiceRadiusKm,
		Address:          wp.Address,
		HourlyRate:       wp.HourlyRate,
		MinimumCharge:    wp.MinimumCharge,
		FreeEstimate:     wp.FreeEstimate,
		YearsExperience:  wp.YearsExperience,
		Certifications:   certs,
		HasInsurance:     wp.HasInsurance,
		Languages:        langs,
		EmergencyService: wp.EmergencyService,
		Website:          wp.Website,
		SocialLinks:      social,
		CreatedAt:        wp.CreatedAt,
		UpdatedAt:        wp.UpdatedAt,
	}
}

// extractUserIDFromRequest resolves the user ID from the session cookie.
// Tries the auth service's user-id endpoint first, then falls back to
// a direct DB query using the raw session token.
func extractUserIDFromRequest(r *http.Request, db *gorm.DB) string {
	// Tries via auth service first
	authReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "http://auth:8083/api/auth/user-id", nil)
	if err == nil {
		addSessionCookie(authReq, r)
		client := &http.Client{Timeout: 3 * time.Second}
		authResp, err := client.Do(authReq)
		if err == nil {
			defer authResp.Body.Close()
			if authResp.StatusCode == http.StatusOK {
				var result struct {
					UserID string `json:"userId"`
				}
				if err := json.NewDecoder(authResp.Body).Decode(&result); err == nil && result.UserID != "" {
					return result.UserID
				}
			}
		}
	}

	// Fallback: parse the cookie directly and query the session table
	cookie, ok := sessionCookie(r)
	if !ok {
		return ""
	}
	token := rawSessionToken(cookie)
	if token == "" {
		return ""
	}
	type dbSession struct {
		UserID string `gorm:"column:userId"`
	}
	var s dbSession
	err = db.Table("\"session\"").Where("token = ? AND \"expiresAt\" > NOW()", token).First(&s).Error
	if err != nil {
		return ""
	}
	return s.UserID
}
