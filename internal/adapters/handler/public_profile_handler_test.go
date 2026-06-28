package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/testingutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicProfileHandlerServeHTTP_Found(t *testing.T) {
	wp := &core.WorkerProfile{
		ID:           "w1",
		Slug:         "acme-plumbing",
		Profession:   "Plumber",
		BusinessName: "Acme Plumbing",
		City:         "Madrid",
	}
	h := NewPublicProfileHandler(&testingutil.MockProfiles{WorkerProfile: wp})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/public/acme-plumbing", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var dto core.WorkerPublicDTO
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&dto))
	assert.Equal(t, "acme-plumbing", dto.Slug)
	assert.Equal(t, "Acme Plumbing", dto.BusinessName)
	assert.Equal(t, "w1", dto.ID)
}

func TestPublicProfileHandlerServeHTTP_NotFound(t *testing.T) {
	h := NewPublicProfileHandler(&testingutil.MockProfiles{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/public/nonexistent", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "not found", resp["error"])
}

func TestPublicProfileHandlerServeHTTP_InvalidSlug(t *testing.T) {
	h := NewPublicProfileHandler(&testingutil.MockProfiles{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/public/INVALID+SLUG%21%21%21", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPublicProfileHandlerServeHTTP_EmptySlug(t *testing.T) {
	h := NewPublicProfileHandler(&testingutil.MockProfiles{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/public/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPublicProfileHandlerServeHTTP_PrivateFieldsStripped(t *testing.T) {
	wp := &core.WorkerProfile{
		ID:           "w1",
		Slug:         "acme-plumbing",
		UserID:       "user-1",
		Profession:   "Plumber",
		BusinessName: "Acme Plumbing",
		Phone:        "+34600123456",
		Address:      "123 Main St",
		City:         "Madrid",
	}
	h := NewPublicProfileHandler(&testingutil.MockProfiles{WorkerProfile: wp})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/public/acme-plumbing", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var raw map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&raw))
	assert.Equal(t, "w1", raw["id"])
	assert.Equal(t, "acme-plumbing", raw["slug"])
	// These private fields must not be present in the public DTO
	_, hasPhone := raw["phone"]
	_, hasAddress := raw["address"]
	_, hasUserID := raw["user_id"]
	assert.False(t, hasPhone, "phone should not be in public DTO")
	assert.False(t, hasAddress, "address should not be in public DTO")
	assert.False(t, hasUserID, "user_id should not be in public DTO")
}

func TestPublicProfileHandlerLatestProfiles_DefaultLimit(t *testing.T) {
	wp := &core.WorkerProfile{
		ID:           "w1",
		Slug:         "acme-plumbing",
		Profession:   "Plumber",
		BusinessName: "Acme Plumbing",
	}
	h := NewPublicProfileHandler(&testingutil.MockProfiles{WorkerProfile: wp})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/public/latest", nil)
	rec := httptest.NewRecorder()
	h.LatestProfiles(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var dtos []core.WorkerPublicDTO
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&dtos))
	require.Len(t, dtos, 1)
	assert.Equal(t, "acme-plumbing", dtos[0].Slug)
}

func TestPublicProfileHandlerLatestProfiles_CustomLimit(t *testing.T) {
	wp := &core.WorkerProfile{
		ID:           "w1",
		Slug:         "acme-plumbing",
		Profession:   "Plumber",
		BusinessName: "Acme Plumbing",
	}
	h := NewPublicProfileHandler(&testingutil.MockProfiles{WorkerProfile: wp})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/public/latest?limit=3", nil)
	rec := httptest.NewRecorder()
	h.LatestProfiles(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestPublicProfileHandlerLatestProfiles_NoSlugReturnsEmpty(t *testing.T) {
	wp := &core.WorkerProfile{
		ID:           "w1",
		Profession:   "Plumber",
		BusinessName: "Acme Plumbing",
		// Slug is empty — should not be returned
	}
	h := NewPublicProfileHandler(&testingutil.MockProfiles{WorkerProfile: wp})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/public/latest", nil)
	rec := httptest.NewRecorder()
	h.LatestProfiles(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var dtos []core.WorkerPublicDTO
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&dtos))
	assert.Empty(t, dtos)
}
