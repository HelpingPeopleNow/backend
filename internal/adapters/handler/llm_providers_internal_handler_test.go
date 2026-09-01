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

func TestLLMProvidersInternalHandlerReturnsAdminProviders(t *testing.T) {
	h := NewLLMProvidersInternalHandler(&testingutil.MockPrompts{
		SP: &core.SystemPrompt{LLMProvider: "groq,openrouter,ollama"},
	})

	req := httptest.NewRequest(http.MethodGet, "/internal/llm-providers", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp llmProvidersResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, []string{"groq", "openrouter", "ollama"}, resp.Providers)
}

func TestLLMProvidersInternalHandlerEmptyMeansAutoChain(t *testing.T) {
	h := NewLLMProvidersInternalHandler(&testingutil.MockPrompts{
		SP: &core.SystemPrompt{LLMProvider: ""},
	})

	req := httptest.NewRequest(http.MethodGet, "/internal/llm-providers", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp llmProvidersResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Empty(t, resp.Providers)
}

func TestLLMProvidersInternalHandlerMethodNotAllowed(t *testing.T) {
	h := NewLLMProvidersInternalHandler(&testingutil.MockPrompts{})

	req := httptest.NewRequest(http.MethodPost, "/internal/llm-providers", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
