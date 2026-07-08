package handler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// fakeLLMHealth implements llmHealthChecker for testing
type fakeLLMHealth struct {
	err error
}

func (f *fakeLLMHealth) Health(_ context.Context) error { return f.err }

// --- fake sql driver for unit tests ---

type fakeDriver struct{}

func (d fakeDriver) Open(name string) (driver.Conn, error) { return fakeConn{}, nil }

type fakeConn struct{}

func (c fakeConn) Prepare(query string) (driver.Stmt, error) { return nil, nil }
func (c fakeConn) Close() error                              { return nil }
func (c fakeConn) Begin() (driver.Tx, error)                 { return nil, nil }

func init() {
	sql.Register("fake", fakeDriver{})
}

// openFakeGorm returns a *gorm.DB backed by the fake sql driver.
// PingContext on this driver always succeeds.
func openFakeGorm(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := sql.Open("fake", "fake:test")
	require.NoError(t, err)
	// Directly set ConnPool so gorm.DB.DB() returns the sql.DB without panicking.
	return &gorm.DB{Config: &gorm.Config{ConnPool: db}}
}

// closedPingGorm returns a *gorm.DB whose underlying *sql.DB is already
// closed, so any PingContext call returns sql.ErrConnDone. Used to drive
// the /health P1-3 error-scrubbing path: with Postgres unreachable the
// handler must (a) flip Postgres="down", (b) NOT leak the underlying
// error message into the response body. The closed-ConnPool trick is
// safe here because health_handler's Postgres check is on sql.DB
// directly; it does not touch gorm.Statement (which would panic on a
// Dialector-less *gorm.DB).
func closedPingGorm(t *testing.T) *gorm.DB {
	t.Helper()
	sqlDB, err := sql.Open("fake", "fake:dead")
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	return &gorm.DB{Config: &gorm.Config{ConnPool: sqlDB}}
}

// --- tests ---

func TestNewHealthHandler(t *testing.T) {
	llm := &fakeLLMHealth{err: nil}
	h := NewHealthHandler(&gorm.DB{}, llm)
	assert.NotNil(t, h)
	assert.Equal(t, llm, h.llm)
}

func TestNewHealthHandlerNilDB(t *testing.T) {
	llm := &fakeLLMHealth{err: nil}
	h := NewHealthHandler(nil, llm)
	assert.NotNil(t, h)
	assert.Nil(t, h.db)
}

func TestLivezPostgresUp(t *testing.T) {
	gdb := openFakeGorm(t)
	llm := &fakeLLMHealth{err: nil}
	h := NewHealthHandler(gdb, llm)

	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	rec := httptest.NewRecorder()

	h.Livez(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp healthResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, "ok", resp.Postgres)
}

func TestLivezReturnsJSONContentType(t *testing.T) {
	gdb := openFakeGorm(t)
	llm := &fakeLLMHealth{err: nil}
	h := NewHealthHandler(gdb, llm)

	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	rec := httptest.NewRecorder()

	h.Livez(rec, req)

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

func TestLivezIgnoresHelperStatus(t *testing.T) {
	// Even when the helper is down, Livez should still only check postgres.
	gdb := openFakeGorm(t)
	llm := &fakeLLMHealth{err: assert.AnError}
	h := NewHealthHandler(gdb, llm)

	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	rec := httptest.NewRecorder()

	h.Livez(rec, req)

	var resp healthResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	// Livez never touches GRPCHelper — it should be empty (zero value).
	assert.Empty(t, resp.GRPCHelper)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestHealthScrubsErrorsFromBody regression-tests P1-3 (audit F7): the raw
// error.Help() or err.Error() text must NOT appear anywhere in the /health
// response body. Operators still see the full detail in slog.
func TestHealthScrubsErrorsFromBody(t *testing.T) {
	gdb := closedPingGorm(t)
	leak := "internal postgres connection string leaked: postgres://user:hunter2@db.local/sensitive"
	llm := &fakeLLMHealth{err: errors.New(leak)}
	h := NewHealthHandler(gdb, llm)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp healthResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	assert.Empty(t, resp.Details["postgres_err"],
		"P1-3 audit: postgres_err key must not appear in /health Details")
	assert.Empty(t, resp.Details["grpc_helper_err"],
		"P1-3 audit: grpc_helper_err key must not appear in /health Details")
	assert.Equal(t, "down", resp.Postgres,
		"P1-3 audit: when PingContext fails, Postgres must report down")
	assert.Equal(t, "down", resp.GRPCHelper)
	assert.Equal(t, "degraded", resp.Status)

	body := rec.Body.String()
	assert.NotContains(t, body, leak,
		"P1-3 audit: raw error string must NOT leak in /health response body")
	assert.NotContains(t, body, "hunter2",
		"P1-3 audit: sensitive substring must NOT leak in /health response body")
}

// TestHealthJSONShapeOnOk is a positive-path shape guard: a healthy system
// returns a body that contains only the status/postgres/grpc_helper fields
// (no Details map at all).
func TestHealthJSONShapeOnOk(t *testing.T) {
	gdb := openFakeGorm(t)
	llm := &fakeLLMHealth{err: nil}
	h := NewHealthHandler(gdb, llm)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	for _, key := range []string{"status", "postgres", "grpc_helper"} {
		assert.Contains(t, resp, key, "ok /health body must include %q", key)
	}
	// P1-3: omit Details entirely when nothing is degraded so we
	// surface a clean shape (`omitempty`).
	if d, ok := resp["details"]; ok {
		assert.Empty(t, d, "on ok path, details must be empty/absent")
	}
	assert.False(t, strings.Contains(body, `"details":{`),
		"on ok path, details object must be omitted, not empty")
}
