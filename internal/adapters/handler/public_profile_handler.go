package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
)

type PublicProfileHandler struct {
	profiles ports.ProfileRepository
}

func NewPublicProfileHandler(profiles ports.ProfileRepository) *PublicProfileHandler {
	return &PublicProfileHandler{profiles: profiles}
}

// ServeHTTP handles GET /api/v1/workers/public/{slug}.
// Slug is extracted via strings.TrimPrefix — no chi dependency.
func (h *PublicProfileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	slog.Info("public-profile: request", "method", r.Method, "path", r.URL.Path)
	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/workers/public/")
	slug = strings.TrimSuffix(slug, "/")
	if slug == "" || !core.ValidateSlug(slug) {
		http.Error(w, `{"error":"invalid slug"}`, http.StatusBadRequest)
		return
	}

	wp, err := h.profiles.FindBySlug(r.Context(), slug)
	if err != nil {
		slog.Error("public profile: find by slug", "slug", slug, "error", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	if wp == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	dto := core.WorkerProfileToPublicDTO(wp)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dto)
}

// LatestProfiles handles GET /api/v1/workers/public/latest?limit=6.
func (h *PublicProfileHandler) LatestProfiles(w http.ResponseWriter, r *http.Request) {
	limit := 6
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 20 {
			limit = parsed
		}
	}

	workers, err := h.profiles.FindLatestWithSlug(r.Context(), limit)
	if err != nil {
		slog.Error("public profile: latest profiles", "error", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	dtos := make([]core.WorkerPublicDTO, len(workers))
	for i, w := range workers {
		dtos[i] = core.WorkerProfileToPublicDTO(&w)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dtos)
}
