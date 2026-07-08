package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"gorm.io/gorm"
)

type HealthHandler struct {
	db  *gorm.DB
	llm llmHealthChecker
}

type llmHealthChecker interface {
	Health(ctx context.Context) error
}

func NewHealthHandler(db *gorm.DB, llm llmHealthChecker) *HealthHandler {
	return &HealthHandler{db: db, llm: llm}
}

type healthResponse struct {
	Status     string            `json:"status"`
	Postgres   string            `json:"postgres"`
	GRPCHelper string            `json:"grpc_helper"`
	Details    map[string]string `json:"details,omitempty"`
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp := healthResponse{
		Postgres:   "ok",
		GRPCHelper: "ok",
		Details:    make(map[string]string),
	}

	// P1-3 (audit F7): scrub raw error strings from the response body so
	// /health doesn't leak internal error detail to unauthenticated probes.
	// The underlying error is still logged via slog for operators.
	if sqlDB, err := h.db.DB(); err != nil {
		resp.Postgres = "down"
		slog.Error("health: postgres unavailable", "error", err)
	} else if err := sqlDB.PingContext(ctx); err != nil {
		resp.Postgres = "down"
		slog.Error("health: postgres ping failed", "error", err)
	}

	if err := h.llm.Health(ctx); err != nil {
		resp.GRPCHelper = "down"
		slog.Error("health: helper gRPC degraded", "error", err)
	}

	if resp.Postgres == "ok" && resp.GRPCHelper == "ok" {
		resp.Status = "ok"
	} else {
		resp.Status = "degraded"
		slog.Warn("health: system degraded", "postgres", resp.Postgres, "grpc_helper", resp.GRPCHelper)
	}

	SetHealthStatus("postgres", resp.Postgres == "ok")
	SetHealthStatus("grpc_helper", resp.GRPCHelper == "ok")

	statusCode := http.StatusOK
	if resp.Status == "degraded" {
		statusCode = http.StatusServiceUnavailable
	}

	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(resp)
}

// Livez is a lightweight liveness probe that only checks Postgres.
// It ignores the helper gRPC service so Docker healthchecks don't kill
// the backend container when the helper is temporarily down.
func (h *HealthHandler) Livez(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	postgres := "ok"
	if sqlDB, err := h.db.DB(); err != nil {
		postgres = "down"
		slog.Error("livez: postgres unavailable", "error", err)
	} else if err := sqlDB.PingContext(ctx); err != nil {
		postgres = "down"
		slog.Error("livez: postgres ping failed", "error", err)
	}

	SetHealthStatus("postgres", postgres == "ok")

	if postgres == "ok" {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok", Postgres: "ok"})
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(healthResponse{Status: "down", Postgres: postgres})
	}
}
