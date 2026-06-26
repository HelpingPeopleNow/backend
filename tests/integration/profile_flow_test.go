//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/adapters/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Profile flow integration tests ──────────────────────────────────────
// Worker profile upsert via chat, GET, DELETE. Same for client.

func TestWorkerProfileUpsertViaChat(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	llm := newFakeLLM(`[FIELDS]
{"profession":"Electrician","city":"Barcelona","hourly_rate":55.0,"bio":"Licensed electrician","phone":"+34600000010"}
[/FIELDS]`)
	mux := buildIntegrationMux(t, db, llm)

	// 1. Upsert via chat
	body, _ := json.Marshal(map[string]interface{}{
		"mode":    "worker_intake",
		"message": "I am an electrician in Barcelona",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fakeAuth("user-wp1")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 2. GET profile
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/worker/profile", nil)
	w2 := httptest.NewRecorder()
	fakeAuth("user-wp1")(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)

	var profile map[string]interface{}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &profile))
	assert.Equal(t, "Electrician", profile["profession"])
	assert.Equal(t, "Barcelona", profile["city"])
	assert.Equal(t, 55.0, profile["hourly_rate"])
}

func TestWorkerProfileUpdateViaChat(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	profileRepo := repository.NewGormProfileRepository(db)

	// Seed initial profile
	require.NoError(t, profileRepo.UpsertWorkerProfile(t.Context(), "user-wp2", map[string]interface{}{
		"profession": "Plumber",
		"city":       "Madrid",
	}))

	// Update via chat with new fields
	llm := newFakeLLM(`[FIELDS]
{"hourly_rate":60.0,"city":"Valencia"}
[/FIELDS]`)
	mux := buildIntegrationMux(t, db, llm)

	body, _ := json.Marshal(map[string]interface{}{
		"mode":    "worker_intake",
		"message": "I raised my rates and moved",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fakeAuth("user-wp2")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Verify merge — old fields preserved, new fields applied
	wp, err := profileRepo.GetWorkerProfile(t.Context(), "user-wp2")
	require.NoError(t, err)
	require.NotNil(t, wp)
	assert.Equal(t, "Plumber", wp.Profession) // preserved
	assert.Equal(t, "Valencia", wp.City)       // updated
	assert.Equal(t, 60.0, wp.HourlyRate)       // new
}

func TestWorkerProfileDeleteViaHTTP(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	llm := newFakeLLM(`[FIELDS]
{"profession":"Carpenter","city":"Sevilla"}
[/FIELDS]`)
	mux := buildIntegrationMux(t, db, llm)

	// Create via chat
	body, _ := json.Marshal(map[string]interface{}{
		"mode":    "worker_intake",
		"message": "I am a carpenter",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fakeAuth("user-wp3")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Verify exists
	profileRepo := repository.NewGormProfileRepository(db)
	wp, err := profileRepo.GetWorkerProfile(t.Context(), "user-wp3")
	require.NoError(t, err)
	require.NotNil(t, wp)

	// DELETE profile
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/worker/profile", nil)
	w2 := httptest.NewRecorder()
	fakeAuth("user-wp3")(mux).ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusNoContent, w2.Code)

	// Verify gone
	wp2, err := profileRepo.GetWorkerProfile(t.Context(), "user-wp3")
	require.NoError(t, err)
	assert.Nil(t, wp2)
}

func TestClientProfileUpsertViaChat(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	llm := newFakeLLM(`[FIELDS]
{"full_name":"Maria Garcia","city":"Madrid","phone":"+34600000020","property_type":"apartment"}
[/FIELDS]`)
	mux := buildIntegrationMux(t, db, llm)

	// 1. Upsert via chat
	body, _ := json.Marshal(map[string]interface{}{
		"mode":    "client_intake",
		"message": "I am Maria Garcia looking for a plumber in Madrid",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fakeAuth("user-cp1")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 2. GET profile
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/client/profile", nil)
	w2 := httptest.NewRecorder()
	fakeAuth("user-cp1")(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)

	var profile map[string]interface{}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &profile))
	assert.Equal(t, "Maria Garcia", profile["full_name"])
	assert.Equal(t, "Madrid", profile["city"])
}

func TestClientProfileDeleteViaHTTP(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	llm := newFakeLLM(`[FIELDS]
{"full_name":"Delete Me"}
[/FIELDS]`)
	mux := buildIntegrationMux(t, db, llm)

	// Create via chat
	body, _ := json.Marshal(map[string]interface{}{
		"mode":    "client_intake",
		"message": "I need help",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fakeAuth("user-cp2")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// DELETE
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/client/profile", nil)
	w2 := httptest.NewRecorder()
	fakeAuth("user-cp2")(mux).ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusNoContent, w2.Code)

	// Verify gone
	profileRepo := repository.NewGormProfileRepository(db)
	cp, err := profileRepo.GetClientProfile(t.Context(), "user-cp2")
	require.NoError(t, err)
	assert.Nil(t, cp)
}

func TestWorkerProfileGetNonExistentViaHTTP(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/worker/profile", nil)
	w := httptest.NewRecorder()
	fakeAuth("user-nonexist")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Returns minimal JSON with just user_id when no profile exists
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "user-nonexist", resp["user_id"])
	assert.Nil(t, resp["profession"])
}
