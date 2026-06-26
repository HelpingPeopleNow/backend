package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// fakeLLMHealth implements llmHealthChecker for testing
type fakeLLMHealth struct {
	err error
}

func (f *fakeLLMHealth) Health(_ context.Context) error { return f.err }

// setupTestDB creates a GORM DB via postgres driver pointing at localhost.
// The DB() call succeeds (returns *sql.DB) but actual queries will fail,
// which is fine for health check tests that only need PingContext.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping health tests that need PostgreSQL")
	}
	db, err := gorm.Open(postgres.Open("host=localhost port=5432 dbname=test user=test password=test sslmode=disable"), &gorm.Config{})
	require.NoError(t, err)
	return db
}

func TestHealthHandlerAllOK(t *testing.T) {
	db := setupTestDB(t)
	h := NewHealthHandler(db, &fakeLLMHealth{err: nil})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Postgres may be "down" if no real DB, but LLM should be "ok"
	var resp healthResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "ok", resp.GRPCHelper)
	// Status depends on whether PG ping succeeds
	if resp.Postgres == "ok" {
		assert.Equal(t, "ok", resp.Status)
	} else {
		assert.Equal(t, "degraded", resp.Status)
	}
}

func TestHealthHandlerLLMDegraded(t *testing.T) {
	db := setupTestDB(t)
	h := NewHealthHandler(db, &fakeLLMHealth{err: errors.New("grpc down")})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var resp healthResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "degraded", resp.Status)
	assert.Equal(t, "down", resp.GRPCHelper)
	assert.Contains(t, resp.Details, "grpc_helper_err")
}

func TestHealthHandlerBothDown(t *testing.T) {
	db := setupTestDB(t)
	// LLM fails, PG likely fails too (no real DB)
	h := NewHealthHandler(db, &fakeLLMHealth{err: errors.New("grpc down")})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp healthResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "degraded", resp.Status)
	assert.Equal(t, "down", resp.GRPCHelper)
}

func TestHealthHandlerContentType(t *testing.T) {
	db := setupTestDB(t)
	h := NewHealthHandler(db, &fakeLLMHealth{err: nil})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}
