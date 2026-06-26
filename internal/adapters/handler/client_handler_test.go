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
	"github.com/stretchr/testify/require"
)

func newClientHandler(cp *core.ClientProfile) *ClientHandler {
	return NewClientHandler(&testingutil.MockProfiles{ClientProfile: cp})
}

func TestClientHandlerGetProfile(t *testing.T) {
	cp := &core.ClientProfile{UserID: "user-1", FullName: "Jane", City: "Madrid"}
	h := newClientHandler(cp)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/client/profile", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "Jane", resp["full_name"])
}

func TestClientHandlerGetNoAuth(t *testing.T) {
	h := newClientHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/client/profile", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestClientHandlerGetNilProfile(t *testing.T) {
	h := newClientHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/client/profile", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-2")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "user_id")
}

func TestClientHandlerDelete(t *testing.T) {
	h := newClientHandler(nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/client/profile", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestClientHandlerMethodNotAllowed(t *testing.T) {
	h := newClientHandler(nil)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/client/profile", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
