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

// deepProbeChecker is an optional capability (OBSERVABILITY_AUDIT_REPORT.md
// §3.2/§4 roadmap item 5): implemented by *llm.GRPCLLMService but checked via
// type assertion rather than added to llmHealthChecker/ports.LLMService so
// the many ports.LLMService test fakes across internal/services and
// internal/testingutil don't all need a new method.
type deepProbeChecker interface {
	DeepProbeStatus(ctx context.Context) (string, map[string]string, error)
}

func NewHealthHandler(db *gorm.DB, llm llmHealthChecker) *HealthHandler {
	return &HealthHandler{db: db, llm: llm}
}

type healthResponse struct {
	Status           string            `json:"status"`
	Postgres         string            `json:"postgres"`
	GRPCHelper       string            `json:"grpc_helper"`
	DeepProbe        string            `json:"deep_probe,omitempty"`
	DeepProbeResults map[string]string `json:"deep_probe_results,omitempty"`
	Details          map[string]string `json:"details,omitempty"`
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

	// OBSERVABILITY_AUDIT_REPORT.md §3.2/§4 roadmap item 5: surface the
	// helper's synthetic deep-probe result. Informational only — a deep
	// probe failure does NOT flip resp.Status, mirroring the helper's own
	// /health (the ChatLLMErrorsCritical / HelperDeepProbeFailed
	// Prometheus rules already page on this signal directly via the
	// helper_deep_probe_success gauge).
	if dp, ok := h.llm.(deepProbeChecker); ok {
		if status, results, err := dp.DeepProbeStatus(ctx); err != nil {
			slog.Warn("health: deep probe status unavailable", "error", err)
		} else {
			resp.DeepProbe = status
			resp.DeepProbeResults = results
		}
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
