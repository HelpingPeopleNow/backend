package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/testingutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSystemPromptHandler(sp *core.SystemPrompt) *SystemPromptHandler {
	return NewSystemPromptHandler(&testingutil.MockPrompts{SP: sp})
}

func TestSystemPromptHandlerGet(t *testing.T) {
	sp := &core.SystemPrompt{
		WorkerProfilePrompt: "WP",
		LLMProvider:         "ollama",
	}
	h := newSystemPromptHandler(sp)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system-prompts", nil)
	ctx := contextkeys.SetUserID(req.Context(), "admin-1")
	ctx = contextkeys.SetIsAdmin(ctx, true)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "WP", resp["worker_profile_prompt"])
	assert.Equal(t, "ollama", resp["llm_provider"])
}

func TestSystemPromptHandlerPutValidColumn(t *testing.T) {
	h := newSystemPromptHandler(&core.SystemPrompt{})

	// Use valid column name from validColumns map
	req := httptest.NewRequest(http.MethodPut, "/api/v1/system-prompts/worker_profile", strings.NewReader(`{"content":"new prompt"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := contextkeys.SetUserID(req.Context(), "admin-1")
	ctx = contextkeys.SetIsAdmin(ctx, true)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestSystemPromptHandlerPutInvalidColumn(t *testing.T) {
	h := newSystemPromptHandler(&core.SystemPrompt{})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/system-prompts/invalid_column", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := contextkeys.SetUserID(req.Context(), "admin-1")
	ctx = contextkeys.SetIsAdmin(ctx, true)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSystemPromptHandlerMethodNotAllowed(t *testing.T) {
	h := newSystemPromptHandler(&core.SystemPrompt{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system-prompts", nil)
	ctx := contextkeys.SetUserID(req.Context(), "admin-1")
	ctx = contextkeys.SetIsAdmin(ctx, true)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}