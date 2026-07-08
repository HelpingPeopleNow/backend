package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// newUnreachableAdminDB constructs a *gorm.DB backed by a real Postgres
// Dialector but pointing at an unused port on loopback. The
// DisableAutomaticPing config option is essential — without it,
// gorm.Open ping-checks the connection eagerly, and the test fails on
// Open rather than on the query path we want to exercise.
//
// Once Open succeeds (no ping), every runtime query against this DB
// fails at the driver layer with a "connection refused" error —
// exactly the runtime-error path that admin_table.go's listRows /
// updateRow / deleteRow must scrub.
//
// We can't use the source-only `TestAdminHandlerScrubsErrorsInSource`
// (see admin_handler_test.go) as a substitute for a runtime test:
// source guards catch re-introduction of leak patterns, but they
// don't exercise the actual handler chain + dispatch. This runtime
// test confirms the 500 response body stays static.
func newUnreachableAdminDB(t *testing.T) *gorm.DB {
	t.Helper()
	// Port 1 is reserved (tcpmux); on loopback it's reliably closed.
	// `connect_timeout=1` bounds the dial duration so the test stays fast.
	dsn := "host=127.0.0.1 port=1 user=x password=x dbname=x sslmode=disable connect_timeout=1"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		DisableAutomaticPing: true,
	})
	require.NoError(t, err,
		"gorm.Open with DisableAutomaticPing must not eager-ping (P1-3 regression test)")
	return db
}

func TestAdminListRowsScrubsRuntimeError(t *testing.T) {
	h := NewAdminHandler(newUnreachableAdminDB(t))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code,
		"runtime dial error must surface as 500 (not panic)")
	body := rec.Body.String()
	assert.Contains(t, body, "internal query failed",
		"P1-3 body must say 'internal query failed' so callers can't infer DB state")
	for _, leak := range []string{
		"connection refused",
		"127.0.0.1",
		"connect_timeout",
		"dial tcp",
		"pgx", "postgres", "password",
	} {
		assert.NotContains(t, strings.ToLower(body), strings.ToLower(leak),
			"P1-3 body must NOT leak: %q", leak)
	}
}

func TestAdminUpdateRowsScrubsRuntimeError(t *testing.T) {
	h := NewAdminHandler(newUnreachableAdminDB(t))
	body := `{"name":"alice"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code,
		"runtime dial error must surface as 500 (not panic)")
	out := rec.Body.String()
	assert.Contains(t, out, "internal update failed",
		"P1-3 body must say 'internal update failed'")
	for _, leak := range []string{
		"connection refused", "127.0.0.1", "pgx", "postgres",
	} {
		assert.NotContains(t, strings.ToLower(out), strings.ToLower(leak),
			"P1-3 body must NOT leak: %q", leak)
	}
}

func TestAdminDeleteRowsScrubsRuntimeError(t *testing.T) {
	h := NewAdminHandler(newUnreachableAdminDB(t))
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code,
		"runtime dial error must surface as 500 (not panic)")
	out := rec.Body.String()
	assert.Contains(t, out, "internal delete failed",
		"P1-3 body must say 'internal delete failed'")
	for _, leak := range []string{
		"connection refused", "127.0.0.1", "pgx", "postgres",
	} {
		assert.NotContains(t, strings.ToLower(out), strings.ToLower(leak),
			"P1-3 body must NOT leak: %q", leak)
	}
}
