package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	metricspkg "github.com/HelpingPeopleNow/backend/internal/metrics"
)

// ReembedToggle provides a runtime kill-switch for the re-embedding pipeline
// (P2-2 audit remediation).
//
//	POST /api/v1/admin/reembed   {"enabled": true|false}   → flips the kill switch
//	GET  /api/v1/admin/reembed                               → returns current state
//
// Mounting (in main.go, under Admin.Wrap):
//
//	mux.Handle("/api/v1/admin/reembed", d.Admin.Wrap(handler.NewReembedToggleHandler(d.Intake)))
//
// The handler calls IntakeService.SetReembedEnabled and publishes the new
// gauge to metrics so Grafana reflects the live state immediately.
type ReembedToggleHandler struct {
	intake ReembedToggler
}

// ReembedToggler is the subset of IntakeService this handler needs.
// Extracted as an interface for testability.
type ReembedToggler interface {
	SetReembedEnabled(enabled bool)
	IsReembedEnabled() bool
}

type reembedState struct {
	Enabled bool `json:"enabled"`
}

func NewReembedToggleHandler(intake ReembedToggler) *ReembedToggleHandler {
	return &ReembedToggleHandler{intake: intake}
}

func (h *ReembedToggleHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		h.getState(w)
	case http.MethodPost:
		h.setState(w, r)
	default:
		slog.Warn("admin: reembed method not allowed", "method", r.Method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
	}
}

func (h *ReembedToggleHandler) getState(w http.ResponseWriter) {
	enabled := h.intake.IsReembedEnabled()
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(reembedState{Enabled: enabled})
}

func (h *ReembedToggleHandler) setState(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	// P2-4 (audit): reject unknown JSON fields.
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON body"})
		return
	}
	h.intake.SetReembedEnabled(req.Enabled)
	slog.Info("admin: reembed toggle", "enabled", req.Enabled)
	metricspkg.SetReembedEnabled(req.Enabled)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(reembedState{Enabled: req.Enabled})
}
