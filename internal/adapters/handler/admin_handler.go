package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"gorm.io/gorm"
)

type entityMeta struct {
	Table   string
	Columns []string
}

var entities = map[string]entityMeta{
	"users": {
		Table:   "\"user\"",
		Columns: []string{"id", "name", "email", "\"is_admin\"", "\"emailVerified\"", "image", "\"createdAt\"", "\"updatedAt\""},
	},
	"worker-profiles": {
		Table:   "worker_profiles",
		Columns: []string{"id", "user_id", "profession", "business_name", "bio", "phone", "city", "service_radius_km", "address", "hourly_rate", "minimum_charge", "free_estimate", "years_experience", "certifications", "has_insurance", "languages", "emergency_service", "website", "social_links", "created_at", "updated_at"},
	},
	"client-profiles": {
		Table:   "client_profiles",
		Columns: []string{"id", "user_id", "full_name", "phone", "city", "address", "bio", "preferred_contact", "property_type", "notes", "created_at", "updated_at"},
	},
	"conversations": {
		Table:   "conversations",
		Columns: []string{"id", "user_id", "type", "metadata", "created_at", "updated_at"},
	},
	"messages": {
		Table:   "messages",
		Columns: []string{"id", "conversation_id", "role", "content", "created_at"},
	},
}

type AdminHandler struct {
	db *gorm.DB
}

func NewAdminHandler(db *gorm.DB) *AdminHandler {
	return &AdminHandler{db: db}
}

func (h *AdminHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/")
	parts := strings.SplitN(path, "/", 2)

	entitySlug := parts[0]
	meta, ok := entities[entitySlug]
	if !ok {
		slog.Warn("admin: unknown entity", "slug", entitySlug)
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown entity: %s", entitySlug))
		return
	}

	if len(parts) == 2 && parts[1] != "" {
		id := parts[1]
		switch r.Method {
		case http.MethodGet:
			h.getRow(w, meta, id)
		case http.MethodPut:
			h.updateRow(w, r, meta, id)
		case http.MethodDelete:
			h.deleteRow(w, meta, id)
		default:
			slog.Warn("admin: method not allowed on entity", "slug", entitySlug, "method", r.Method, "id", id)
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.listRows(w, r, meta)
	default:
		slog.Warn("admin: method not allowed on listing", "slug", entitySlug, "method", r.Method)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
