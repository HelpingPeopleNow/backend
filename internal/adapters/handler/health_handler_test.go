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

type fakeLLM struct {
	healthErr error
}

func (f *fakeLLM) Health(ctx context.Context) error { return f.healthErr }

// openTestDB opens a GORM connection to a non-existent DB so db.DB().PingContext fails.
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "host=localhost port=5433 dbname=nonexistent sslmode=disable"
	}
	db, err := gorm.Open(postgres.Open(dbURL), &gorm.Config{})
	if err != nil {
		t.Skipf("skipping: cannot open test DB: %v", err)
	}
	return db
}

func TestHealthBothHealthy(t *testing.T) {
	t.Skip("integration: requires PostgreSQL service container")
}

func TestHealthLLMDown(t *testing.T) {
	db := openTestDB(t)
	llm := &fakeLLM{healthErr: errors.New("helper unreachable")}
	h := NewHealthHandler(db, llm)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var resp healthResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "degraded", resp.Status)
	assert.Equal(t, "down", resp.GRPCHelper)
	assert.Contains(t, resp.Details["grpc_helper_err"], "helper unreachable")
}

func TestHealthBothDown(t *testing.T) {
	db := openTestDB(t)
	llm := &fakeLLM{healthErr: errors.New("helper down")}
	h := NewHealthHandler(db, llm)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var resp healthResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "degraded", resp.Status)
	assert.Equal(t, "down", resp.GRPCHelper)
	assert.Equal(t, "down", resp.Postgres)
}

func TestHealthLLMHealthyPGDown(t *testing.T) {
	db := openTestDB(t)
	llm := &fakeLLM{healthErr: nil}
	h := NewHealthHandler(db, llm)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var resp healthResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "degraded", resp.Status)
	assert.Equal(t, "ok", resp.GRPCHelper)
	assert.Equal(t, "down", resp.Postgres)
}

func TestHealthContentType(t *testing.T) {
	db := openTestDB(t)
	llm := &fakeLLM{healthErr: nil}
	h := NewHealthHandler(db, llm)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}
