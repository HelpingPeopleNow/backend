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

	if sqlDB, err := h.db.DB(); err != nil {
		resp.Postgres = "down"
		resp.Details["postgres_err"] = err.Error()
		slog.Error("health: postgres unavailable", "error", err)
	} else if err := sqlDB.PingContext(ctx); err != nil {
		resp.Postgres = "down"
		resp.Details["postgres_err"] = err.Error()
		slog.Error("health: postgres ping failed", "error", err)
	}

	if err := h.llm.Health(ctx); err != nil {
		resp.GRPCHelper = "down"
		resp.Details["grpc_helper_err"] = err.Error()
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
