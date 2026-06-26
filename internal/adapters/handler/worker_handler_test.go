package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/testingutil"
	"github.com/stretchr/testify/assert"
)

func newWorkerHandler(wp *core.WorkerProfile) *WorkerHandler {
	return NewWorkerHandler(&testingutil.MockProfiles{WorkerProfile: wp})
}

func TestWorkerHandlerGetProfile(t *testing.T) {
	wp := &core.WorkerProfile{UserID: "user-1", Profession: "Plumber", City: "Madrid"}
	h := newWorkerHandler(wp)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/worker/profile", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	assert.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "Plumber", resp["profession"])
}

func TestWorkerHandlerGetNoAuth(t *testing.T) {
	h := newWorkerHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/worker/profile", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestWorkerHandlerGetNilProfile(t *testing.T) {
	h := newWorkerHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/worker/profile", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	// Returns {user_id: "..."} when profile is nil
	var resp map[string]interface{}
	assert.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "user-1", resp["user_id"])
}

func TestWorkerHandlerDelete(t *testing.T) {
	h := newWorkerHandler(nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/worker/profile", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestWorkerHandlerOptions(t *testing.T) {
	h := newWorkerHandler(nil)
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/worker/profile", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestWorkerHandlerMethodNotAllowed(t *testing.T) {
	h := newWorkerHandler(nil)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/worker/profile", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
