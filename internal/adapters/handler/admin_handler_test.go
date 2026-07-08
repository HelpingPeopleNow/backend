package handler

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminHandlerUnknownEntity(t *testing.T) {
	h := NewAdminHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/bogus", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Contains(t, resp["error"], "unknown entity")
}

func TestAdminHandlerMethodNotAllowedList(t *testing.T) {
	h := NewAdminHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestAdminHandlerMethodNotAllowedWithID(t *testing.T) {
	h := NewAdminHandler(nil)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/users/1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestAdminHandlerCleanCol(t *testing.T) {
	assert.Equal(t, "id", cleanCol(`"id"`))
	assert.Equal(t, "name", cleanCol("name"))
}

func TestAdminHandlerScanRow(t *testing.T) {
	meta := entityMeta{
		Table:   "test",
		Columns: []string{"id", "name"},
	}
	vals := []interface{}{[]byte("1"), "hello"}
	row := scanRow(meta, vals)
	assert.Equal(t, "1", row["id"])
	assert.Equal(t, "hello", row["name"])
}

func TestAdminHandlerScanRowNonBytes(t *testing.T) {
	meta := entityMeta{
		Table:   "test",
		Columns: []string{"count"},
	}
	vals := []interface{}{float64(42)}
	row := scanRow(meta, vals)
	assert.Equal(t, float64(42), row["count"])
}

func TestAdminHandlerUpdateInvalidJSON(t *testing.T) {
	h := NewAdminHandler(nil)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.True(t, rec.Code >= 400)
}

func TestAdminHandlerUpdateEmptyFields(t *testing.T) {
	h := NewAdminHandler(nil)
	body := `{"id": "1"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAdminHandlerDeleteUnknownEntity(t *testing.T) {
	h := NewAdminHandler(nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/bogus/1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAdminHandlerUpdateInvalidJSONBody(t *testing.T) {
	h := NewAdminHandler(nil)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/1", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAdminHandlerEntitySlugs(t *testing.T) {
	expected := []string{"users", "worker-profiles", "client-profiles", "conversations", "messages"}
	for _, slug := range expected {
		_, ok := entities[slug]
		assert.True(t, ok, "missing entity: %s", slug)
	}
}

func TestAdminHandlerPATCHMethodNotAllowed(t *testing.T) {
	h := NewAdminHandler(nil)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/worker-profiles/wp-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestAdminHandlerEmptyPath(t *testing.T) {
	h := NewAdminHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAdminHandlerScanRowMultipleByteCols(t *testing.T) {
	meta := entityMeta{
		Table:   "test",
		Columns: []string{"id", "name", "email"},
	}
	vals := []interface{}{[]byte("42"), []byte("Alice"), []byte("alice@test.com")}
	row := scanRow(meta, vals)
	assert.Equal(t, "42", row["id"])
	assert.Equal(t, "Alice", row["name"])
	assert.Equal(t, "alice@test.com", row["email"])
}

// TestAdminHandlerScrubsErrorsInSource is a static-source guard for the
// P1-3 (audit F7) error-scrubbing fix in admin_table.go. Rather than
// spinning up a Postgres fixture to drive the listRows/updateRow/deleteRow
// 500 paths (which need a real Dialector — gorm v1.25's Statement.QuoteTo
// panics on a Dialector-less *gorm.DB), this asserts that the response-
// body strings the audit demands really do appear in the source file and
// that the pre-audit "fmt.Sprintf-formatted" leak patterns really do not
// appear. Combined with the runtime TestHandleLLMErrorGeneric and
// TestHealthScrubsErrorsFromBody (sister paths), this is sufficient to
// fail a regression if someone re-introduces the leak pattern.
//
// TODO(audit-P3): replace with a real Dialector-backed integration test
// in tests/integration once a fixtures setup exists.
//
// //go:embed is resolved at compile time relative to the test source
// file, so this guard is cwd-immune (vscode-go, Bazel, delve all behave
// the same as `go test ./...`).
//
//go:embed admin_table.go
var adminTableSrc string

func TestAdminHandlerScrubsErrorsInSource(t *testing.T) {
	// Sanity: file must not be empty. Without this, a refactor that strips
	// the file down could vacuously pass mustContain and mustNotContain.
	require.Greater(t, len(adminTableSrc), 200,
		"P1-3 source guard: embedded admin_table.go is suspiciously short "+
			"(%d bytes) — the audit's scrubbing clauses are likely missing",
		len(adminTableSrc))
	src := adminTableSrc

	mustContain := []string{
		`"internal query failed"`,
		`"internal update failed"`,
		`"internal delete failed"`,
	}
	mustNotContain := []string{
		`fmt.Sprintf("query failed: %s"`,
		`fmt.Sprintf("update failed: %s"`,
		`fmt.Sprintf("delete failed: %s"`,
	}

	for _, want := range mustContain {
		assert.Contains(t, src, want,
			"P1-3 source guard: admin_table.go must contain %q", want)
	}
	for _, bad := range mustNotContain {
		assert.NotContains(t, src, bad,
			"P1-3 source guard: admin_table.go must NOT contain the leak pattern %q", bad)
	}
}
