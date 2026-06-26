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

// ── Chat flow integration tests ─────────────────────────────────────────
// These test POST /api/v1/chat through the full handler → service →
// repository → real PG stack with a FakeLLM that returns canned [FIELDS]
// blocks.

const (
	// workerFieldsResponse is what the fake LLM returns for worker_intake mode.
	workerFieldsResponse = `Here is your worker profile:
[FIELDS]
{"profession":"Plumber","city":"Madrid","hourly_rate":45.0,"bio":"10 years fixing pipes","phone":"+34600000001"}
[/FIELDS]`

	// clientFieldsResponse is what the fake LLM returns for client_intake mode.
	clientFieldsResponse = `Here is your client profile:
[FIELDS]
{"full_name":"Alvaro Test","city":"Madrid","phone":"+34600000002","bio":"Need plumbing help"}
[/FIELDS]`

	// searchResponse is what the fake LLM returns for search mode (pass 1 — [SEARCH] block).
	searchResponse = `[SEARCH]
{"profession":"Plumber","city":"Madrid"}
[/SEARCH]`

	// searchPresentationResponse is the fake LLM return for search pass 2.
	searchPresentationResponse = `I found 1 plumber in Madrid who can help you.`
)

func TestChatWorkerIntake(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	llm := newFakeLLM(workerFieldsResponse)
	mux := buildIntegrationMux(t, db, llm)
	handler := fakeAuth("user-chat-w1")(mux)

	body, _ := json.Marshal(map[string]interface{}{
		"mode":    "worker_intake",
		"message": "I am a plumber in Madrid with 10 years experience",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)
	_ = handler // handler is mux wrapped with fakeAuth; use it below

	// Retry with auth
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	fakeAuth("user-chat-w1")(mux).ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusOK, w2.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["answer"])
	assert.NotEmpty(t, resp["conversation_id"])

	// Verify profile was upserted in the real DB
	profileRepo := repository.NewGormProfileRepository(db)
	wp, err := profileRepo.GetWorkerProfile(t.Context(), "user-chat-w1")
	require.NoError(t, err)
	require.NotNil(t, wp)
	assert.Equal(t, "Plumber", wp.Profession)
	assert.Equal(t, "Madrid", wp.City)
	assert.Equal(t, 45.0, wp.HourlyRate)
}

func TestChatClientIntake(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	llm := newFakeLLM(clientFieldsResponse)
	mux := buildIntegrationMux(t, db, llm)

	body, _ := json.Marshal(map[string]interface{}{
		"mode":    "client_intake",
		"message": "I need a plumber in Madrid",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	fakeAuth("user-chat-c1")(mux).ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["answer"])
	assert.NotEmpty(t, resp["conversation_id"])

	// Verify client profile was upserted
	profileRepo := repository.NewGormProfileRepository(db)
	cp, err := profileRepo.GetClientProfile(t.Context(), "user-chat-c1")
	require.NoError(t, err)
	require.NotNil(t, cp)
	assert.Equal(t, "Alvaro Test", cp.FullName)
	assert.Equal(t, "Madrid", cp.City)
}

func TestChatSearch(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	// For search, the LLM is called twice (two-pass): once for [SEARCH]
	// extraction, once for presentation. We use a two-answer fake.
	llm := newFakeLLM(searchResponse)
	// The search service calls LLM twice; the fake returns the same answer
	// both times. Pass 2 won't parse as [SEARCH] so it'll be treated as
	// presentation text.
	mux := buildIntegrationMux(t, db, llm)

	body, _ := json.Marshal(map[string]interface{}{
		"mode":    "search",
		"message": "I need a plumber in Madrid",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	fakeAuth("user-search-1")(mux).ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["answer"])
}

func TestChatInvalidMode(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	body, _ := json.Marshal(map[string]interface{}{
		"mode":    "invalid_mode",
		"message": "hello",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	fakeAuth("user-invalid")(mux).ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestChatEmptyMessage(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	body, _ := json.Marshal(map[string]interface{}{
		"mode":    "worker_intake",
		"message": "",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	fakeAuth("user-empty")(mux).ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestChatMethodNotAllowed(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat", nil)
	w := httptest.NewRecorder()

	fakeAuth("user-get")(mux).ServeHTTP(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
