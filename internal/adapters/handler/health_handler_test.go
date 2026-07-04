package handler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
