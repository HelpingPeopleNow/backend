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

// ── Admin CRUD integration tests ────────────────────────────────────────
// Admin CRUD for 5 entities × list/get/update/delete.
// The admin handler operates directly on GORM tables. We test against
// real PG to verify the SQL queries work.

func TestAdminListWorkerProfiles(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	// Seed data
	profileRepo := repository.NewGormProfileRepository(db)
	require.NoError(t, profileRepo.UpsertWorkerProfile(t.Context(), "admin-w1", map[string]interface{}{
		"profession": "Plumber",
		"city":       "Madrid",
	}))

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/worker-profiles", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var rows []map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rows))
	require.GreaterOrEqual(t, len(rows), 1)
	// Find our seeded row
	found := false
	for _, row := range rows {
		if row["user_id"] == "admin-w1" {
			assert.Equal(t, "Plumber", row["profession"])
			found = true
			break
		}
	}
	assert.True(t, found, "seeded worker profile should appear in admin list")
}

func TestAdminGetWorkerProfile(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	profileRepo := repository.NewGormProfileRepository(db)
	require.NoError(t, profileRepo.UpsertWorkerProfile(t.Context(), "admin-w2", map[string]interface{}{
		"profession": "Electrician",
	}))

	// Get the profile ID
	wp, err := profileRepo.GetWorkerProfile(t.Context(), "admin-w2")
	require.NoError(t, err)
	require.NotNil(t, wp)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/worker-profiles/"+wp.ID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var row map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &row))
	assert.Equal(t, "Electrician", row["profession"])
	assert.Equal(t, "admin-w2", row["user_id"])
}

func TestAdminUpdateWorkerProfile(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	profileRepo := repository.NewGormProfileRepository(db)
	require.NoError(t, profileRepo.UpsertWorkerProfile(t.Context(), "admin-w3", map[string]interface{}{
		"profession": "Plumber",
	}))

	wp, err := profileRepo.GetWorkerProfile(t.Context(), "admin-w3")
	require.NoError(t, err)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	body, _ := json.Marshal(map[string]interface{}{
		"profession": "Master Plumber",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/worker-profiles/"+wp.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Verify update persisted
	wp2, err := profileRepo.GetWorkerProfile(t.Context(), "admin-w3")
	require.NoError(t, err)
	assert.Equal(t, "Master Plumber", wp2.Profession)
}

func TestAdminDeleteWorkerProfile(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	profileRepo := repository.NewGormProfileRepository(db)
	require.NoError(t, profileRepo.UpsertWorkerProfile(t.Context(), "admin-w4", map[string]interface{}{
		"profession": "To Delete",
	}))

	wp, err := profileRepo.GetWorkerProfile(t.Context(), "admin-w4")
	require.NoError(t, err)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/worker-profiles/"+wp.ID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Verify deleted
	wp2, err := profileRepo.GetWorkerProfile(t.Context(), "admin-w4")
	require.NoError(t, err)
	assert.Nil(t, wp2)
}

func TestAdminListClientProfiles(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	profileRepo := repository.NewGormProfileRepository(db)
	require.NoError(t, profileRepo.UpsertClientProfile(t.Context(), "admin-c1", map[string]interface{}{
		"full_name": "Admin Test Client",
	}))

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/client-profiles", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var rows []map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rows))
	assert.GreaterOrEqual(t, len(rows), 1)
}

func TestAdminListConversations(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	chatRepo := repository.NewGormChatRepository(db)
	_, err := chatRepo.SaveMessages(t.Context(), "admin-conv-user", "worker", "hello", "hi", "", nil, nil)
	require.NoError(t, err)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/conversations", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var rows []map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rows))
	assert.GreaterOrEqual(t, len(rows), 1)
}

func TestAdminGetConversation(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	chatRepo := repository.NewGormChatRepository(db)
	convID, err := chatRepo.SaveMessages(t.Context(), "admin-conv-get", "worker", "test", "reply", "", nil, nil)
	require.NoError(t, err)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/conversations/"+convID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var row map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &row))
	assert.Equal(t, convID, row["id"])
}

func TestAdminListSystemPrompts(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system-prompts", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var rows []map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rows))
	assert.GreaterOrEqual(t, len(rows), 1)
}

func TestAdminGetSystemPrompt(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	// Seed
	promptRepo := repository.NewGormSystemPromptRepository(db)
	sp, err := promptRepo.Get(t.Context())
	require.NoError(t, err)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system-prompts/1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var row map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &row))
	assert.Equal(t, float64(sp.ID), row["id"])
}

func TestAdminGetNonexistent(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/worker-profiles/00000000-0000-0000-0000-000000000000", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAdminUnknownEntity(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/bogus-entity", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAdminUpdateNotFound(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	body, _ := json.Marshal(map[string]interface{}{
		"profession": "Ghost",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/worker-profiles/00000000-0000-0000-0000-000000000000", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAdminDeleteNotFound(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/worker-profiles/00000000-0000-0000-0000-000000000000", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
