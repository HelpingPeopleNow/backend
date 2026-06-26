package handler

import (
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
	assert.Equal(t, "1", row["id"]) // []byte → string conversion
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
	// nil body → read error
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// Should fail with "read body failed" or "invalid JSON"
	assert.True(t, rec.Code >= 400)
}

func TestAdminHandlerUpdateEmptyFields(t *testing.T) {
	h := NewAdminHandler(nil)
	body := `{"id": "1"}` // id is filtered out, so no valid fields
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

// Verify entity meta map has all expected keys
func TestAdminHandlerEntitySlugs(t *testing.T) {
	expected := []string{"users", "worker-profiles", "client-profiles", "conversations", "messages"}
	for _, slug := range expected {
		_, ok := entities[slug]
		assert.True(t, ok, "missing entity: %s", slug)
	}
}
