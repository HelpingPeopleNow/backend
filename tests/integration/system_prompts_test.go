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

// ── System prompts integration tests ────────────────────────────────────
// GET /api/v1/system-prompts (any user), PUT /api/v1/system-prompts/ (admin only).

func TestSystemPromptsGET(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	// GET returns the system prompts (auto-created defaults)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system-prompts", nil)
	w := httptest.NewRecorder()
	fakeAuth("user-sp-get")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp, "worker_profile_prompt")
	assert.Contains(t, resp, "client_profile_prompt")
	assert.Contains(t, resp, "find_trader_search_prompt")
	assert.Contains(t, resp, "find_trader_presentation_prompt")
	assert.Contains(t, resp, "llm_provider")
}

func TestSystemPromptsPUT(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	// Seed the row so it exists
	promptRepo := repository.NewGormSystemPromptRepository(db)
	_, err := promptRepo.Get(t.Context())
	require.NoError(t, err)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	// PUT to update worker_profile_prompt
	body, _ := json.Marshal(map[string]interface{}{
		"content": "You are a professional worker intake assistant for HelpingPeopleNow.",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/system-prompts/worker_profile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	// System prompts PUT is admin-protected; we test the handler directly
	// without the admin middleware since we're testing DB persistence, not ACL.
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "You are a professional worker intake assistant for HelpingPeopleNow.", resp["worker_profile_prompt"])

	// Verify persistence
	sp, err := promptRepo.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "You are a professional worker intake assistant for HelpingPeopleNow.", sp.WorkerProfilePrompt)
}

func TestSystemPromptsPUTInvalidColumn(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	body, _ := json.Marshal(map[string]interface{}{
		"content": "test",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/system-prompts/nonexistent_column", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSystemPromptsPUTEmptyContent(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	body, _ := json.Marshal(map[string]interface{}{
		"content": "",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/system-prompts/worker_profile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSystemPromptsPUTProvider(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	// PUT to update provider — empty content is allowed for provider
	body, _ := json.Marshal(map[string]interface{}{
		"content": "openai",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/system-prompts/provider", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "openai", resp["llm_provider"])
}
