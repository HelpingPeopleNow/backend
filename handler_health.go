package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/adapters/handler"
	"gorm.io/gorm"
)

// healthResponse is the shape of the /health JSON output.
type healthResponse struct {
	Status     string            `json:"status"`
	Postgres   string            `json:"postgres"`
	GRPCHelper string            `json:"grpc_helper"`
	Details    map[string]string `json:"details,omitempty"`
}

// newHealthHandler returns an HTTP handler that checks PostgreSQL and the
// helper gRPC service health endpoint before reporting status.
func newHealthHandler(db *gorm.DB) http.HandlerFunc {
	helperHealthURL := os.Getenv("HELPER_HEALTH_URL")
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		resp := healthResponse{
			Postgres:   "ok",
			GRPCHelper: "ok",
			Details:    make(map[string]string),
		}

		// --- PostgreSQL check ---
		sqlDB, err := db.DB()
		if err != nil {
			resp.Postgres = "down"
			resp.Details["postgres_err"] = err.Error()
			slog.Error("health: postgres unavailable", "error", err)
		} else if err := sqlDB.PingContext(ctx); err != nil {
			resp.Postgres = "down"
			resp.Details["postgres_err"] = err.Error()
			slog.Error("health: postgres ping failed", "error", err)
		}

		// --- Helper gRPC health check (HTTP probe on :8084) ---
		helperCtx, helperCancel := context.WithTimeout(ctx, 3*time.Second)
		defer helperCancel()
		req, err := http.NewRequestWithContext(helperCtx, http.MethodGet, helperHealthURL, nil)
		if err != nil {
			resp.GRPCHelper = "down"
			resp.Details["grpc_helper_err"] = err.Error()
			slog.Error("health: failed to create helper request", "error", err)
		} else {
			hc := &http.Client{Timeout: 3 * time.Second}
			hresp, err := hc.Do(req)
			if err != nil {
				resp.GRPCHelper = "down"
				resp.Details["grpc_helper_err"] = err.Error()
				slog.Error("health: helper gRPC unreachable", "error", err)
			} else {
				hresp.Body.Close()
				if hresp.StatusCode != http.StatusOK {
					resp.GRPCHelper = "down"
					resp.Details["grpc_helper_err"] = http.StatusText(hresp.StatusCode)
					slog.Warn("health: helper gRPC degraded", "status", hresp.StatusCode)
				}
			}
		}

		// --- Overall status ---
		if resp.Postgres == "ok" && resp.GRPCHelper == "ok" {
			resp.Status = "ok"
		} else {
			resp.Status = "degraded"
			slog.Warn("health: system degraded", "postgres", resp.Postgres, "grpc_helper", resp.GRPCHelper)
		}

		// --- Record Prometheus health gauges ---
		handler.SetHealthStatus("postgres", resp.Postgres == "ok")
		handler.SetHealthStatus("grpc_helper", resp.GRPCHelper == "ok")

		statusCode := http.StatusOK
		if resp.Status == "degraded" {
			statusCode = http.StatusServiceUnavailable
		}

		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(resp)
	}
}
