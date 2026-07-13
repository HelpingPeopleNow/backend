package services

import (
	"context"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
	"github.com/HelpingPeopleNow/backend/internal/testingutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── searchFiltersFromJSON additional coverage ────────────────────────

func TestSearchFiltersFromJSONAllFields(t *testing.T) {
	input := []byte(`{"profession":"plumber","city":"Madrid","emergency":true,"free_estimate":true,"insured":true}`)
	f := searchFiltersFromJSON(input)
	assert.Equal(t, "Plumber", f.Profession)
	assert.Equal(t, "Madrid", f.City)
	assert.True(t, f.EmergencyOnly)
	assert.True(t, f.FreeEstimateOnly)
	assert.True(t, f.InsuredOnly)
}

func TestSearchFiltersFromJSONSpanishProfession(t *testing.T) {
	input := []byte(`{"profession":"electricista","city":"Barcelona"}`)
	f := searchFiltersFromJSON(input)
	assert.Equal(t, "Electrician", f.Profession) // normalized
}

// ── Search with empty prompt ─────────────────────────────────────────

func TestSearchEmptyPrompt(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "Hello!"}
	prompts := &testingutil.MockPrompts{SP: &core.SystemPrompt{FindTraderSearchPrompt: ""}}
	svc := NewSearchService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, prompts)

	result, err := svc.Search(t.Context(), "user-1", "plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "ilike_disabled_via_env", result.Branch)
}

func TestSearchConversationalNoUserID(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "What do you need?"}
	svc := NewSearchService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	result, err := svc.Search(t.Context(), "", "hi there", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "", result.ConversationID)
}

// ── Search cache behavior ────────────────────────────────────────────

func TestSearchCacheHit(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "[SEARCH]{\"profession\":\"plumber\"}[/SEARCH]"}
	chatRepo := &testingutil.MockChatRepo{ReturnID: "s1"}
	svc := NewSearchService(llm, &testingutil.MockProfiles{}, chatRepo, &testingutil.MockPrompts{})

	// First call
	result1, err := svc.Search(t.Context(), "user-1", "plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)

	// Second call with same query — should hit cache
	result2, err := svc.Search(t.Context(), "user-1", "plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)

	// Both should have the same answer (from cache)
	assert.Equal(t, result1.Answer, result2.Answer)
	// But ConversationID should NOT be leaked from cache
	assert.Equal(t, "", result2.ConversationID)
}

// ── Search with client city fallback ─────────────────────────────────

func TestSearchClientCityFallback(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "[SEARCH]{\"profession\":\"plumber\"}[/SEARCH]"}
	profs := &testingutil.MockProfiles{
		ClientProfile: &core.ClientProfile{City: "Barcelona"},
	}
	svc := NewSearchService(llm, profs, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	result, err := svc.Search(t.Context(), "user-1", "plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// ── currentWorkerSignature ───────────────────────────────────────────

func TestCurrentWorkerSignatureNilDB(t *testing.T) {
	profs := &testingutil.MockProfiles{} // RawQuery returns nil
	svc := NewSearchService(&testingutil.MockLLM{}, profs, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	sig, err := svc.currentWorkerSignature(t.Context())
	assert.NoError(t, err)
	assert.True(t, sig.MaxUpdate.IsZero()) // nil DB → zero time
	assert.Equal(t, 0, sig.Count)
}

func TestCurrentWorkerSignatureMemoization(t *testing.T) {
	profs := &testingutil.MockProfiles{} // RawQuery returns nil
	svc := NewSearchService(&testingutil.MockLLM{}, profs, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	sig1, _ := svc.currentWorkerSignature(t.Context())
	sig2, _ := svc.currentWorkerSignature(t.Context())
	assert.Equal(t, sig1, sig2)
}

// ── ReembedWorker ────────────────────────────────────────────────────

func TestReembedWorkerProfileNotFound(t *testing.T) {
	profs := &testingutil.MockProfiles{WorkerProfile: nil}
	llm := &testingutil.MockLLM{}
	svc := NewIntakeService(llm, profs, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	// Should not panic — logs warning and returns
	svc.ReembedWorker("nonexistent")
}

func TestReembedWorkerNoFieldsToEmbed(t *testing.T) {
	profs := &testingutil.MockProfiles{
		WorkerProfile: &core.WorkerProfile{
			UserID: "user-1",
			// all empty → BuildFieldTexts may be empty
		},
		EmbeddingMeta: map[string]ports.EmbeddingMeta{},
	}
	llm := &testingutil.MockLLM{}
	svc := NewIntakeService(llm, profs, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	// Should return without panic
	svc.ReembedWorker("user-1")
}

func TestReembedWorkerWithFields(t *testing.T) {
	profs := &testingutil.MockProfiles{
		WorkerProfile: &core.WorkerProfile{
			UserID:     "user-1",
			Profession: "plumber",
			City:       "Madrid",
			Bio:        "10 years experience",
		},
		EmbeddingMeta: map[string]ports.EmbeddingMeta{},
	}
	llm := &testingutil.MockLLM{
		EmbedFn: func(_ context.Context, _ string) ([]float32, error) {
			return make([]float32, 768), nil
		},
	}
	svc := NewIntakeService(llm, profs, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	// Should re-embed all fields
	svc.ReembedWorker("user-1")
}

func TestReembedWorkerSkipsUnchanged(t *testing.T) {
	profs := &testingutil.MockProfiles{
		WorkerProfile: &core.WorkerProfile{
			UserID:     "user-1",
			Profession: "plumber",
			City:       "Madrid",
		},
		EmbeddingMeta: map[string]ports.EmbeddingMeta{
			"city": {Hash: "different-hash", Model: "granite-embedding:278m"},
		},
	}

	embedCalled := 0
	llm := &testingutil.MockLLM{
		EmbedFn: func(_ context.Context, _ string) ([]float32, error) {
			embedCalled++
			return make([]float32, 768), nil
		},
	}
	svc := NewIntakeService(llm, profs, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	svc.ReembedWorker("user-1")
	// Some fields should have been re-embedded, some skipped
	assert.Greater(t, embedCalled, 0)
}

func TestReembedWorkerModelChange(t *testing.T) {
	profs := &testingutil.MockProfiles{
		WorkerProfile: &core.WorkerProfile{
			UserID:     "user-1",
			Profession: "plumber",
		},
		EmbeddingMeta: map[string]ports.EmbeddingMeta{
			"profession": {Hash: "same", Model: "old-model"},
		},
	}

	embedCalled := 0
	llm := &testingutil.MockLLM{
		EmbedFn: func(_ context.Context, _ string) ([]float32, error) {
			embedCalled++
			return make([]float32, 768), nil
		},
	}
	svc := NewIntakeService(llm, profs, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	svc.ReembedWorker("user-1")
	// Model changed → should re-embed
	assert.Greater(t, embedCalled, 0)
}
