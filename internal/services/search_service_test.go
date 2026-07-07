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

// ── normalizeProfession ─────────────────────────────────────────────

func TestNormalizeProfessionVariants(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"electricista", "electrician"},
		{"Electricista", "electrician"},
		{"electrician", "electrician"},
		{"fontanero", "plumber"},
		{"plomero", "plumber"},
		{"plumber", "plumber"},
		{"limpieza", "cleaner"},
		{"cleaner", "cleaner"},
		{"manitas", "handyman"},
		{"handyman", "handyman"},
		{"carpintero", "carpintero"},
		{"carpenter", "carpintero"},
		{"pintor", "painter"},
		{"painter", "painter"},
		{"jardinero", "landscaper"},
		{"landscaper", "landscaper"},
		{"tejador", "roofer"},
		{"roofer", "roofer"},
		{"hvac", "hvac technician"},
		{"unknown_trade", "unknown_trade"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeProfession(tc.input)
			assert.Equal(t, tc.expected, got)
		})
	}
}

// ── buildWorkerSummaries ────────────────────────────────────────────

func TestBuildWorkerSummariesBasic(t *testing.T) {
	workers := []core.WorkerProfile{
		{
			ID:         "w1",
			Profession: "plumber",
			City:       "Madrid",
			HourlyRate: 25.0,
		},
	}
	summaries := buildWorkerSummaries(workers, "need a plumber")
	assert.Contains(t, summaries, "plumber")
	assert.Contains(t, summaries, "Madrid")
}

func TestBuildWorkerSummariesEmpty(t *testing.T) {
	summaries := buildWorkerSummaries(nil, "need a plumber")
	assert.Contains(t, summaries, "No workers matched")
	assert.Contains(t, summaries, "need a plumber")
}

func TestBuildWorkerSummariesWithDetails(t *testing.T) {
	workers := []core.WorkerProfile{
		{
			ID:               "w1",
			Profession:       "plumber",
			City:             "Madrid",
			Phone:            "+34600123456",
			Bio:              "10 years experience",
			HasInsurance:     true,
			EmergencyService: true,
			FreeEstimate:     true,
			Certifications:   `["GAS SAFE"]`,
		},
	}
	summaries := buildWorkerSummaries(workers, "need a plumber in Madrid")
	assert.Contains(t, summaries, "plumber")
	assert.Contains(t, summaries, "Madrid")
	assert.Contains(t, summaries, "phone: +34600123456")
	assert.Contains(t, summaries, "bio: 10 years experience")
	assert.Contains(t, summaries, "insured")
	assert.Contains(t, summaries, "emergency service")
	assert.Contains(t, summaries, "free estimates")
	assert.Contains(t, summaries, "certifications: GAS SAFE")
}

// ── searchFiltersFromJSON ───────────────────────────────────────────

func TestSearchFiltersFromJSON(t *testing.T) {
	filters := searchFiltersFromJSON([]byte(`{"profession":"plumber","city":"Madrid"}`))
	assert.Equal(t, "plumber", filters.Profession)
	assert.Equal(t, "Madrid", filters.City)
}

func TestSearchFiltersFromJSONEmpty(t *testing.T) {
	filters := searchFiltersFromJSON([]byte(`{}`))
	assert.Equal(t, "", filters.Profession)
}

func TestSearchFiltersFromJSONInvalid(t *testing.T) {
	filters := searchFiltersFromJSON([]byte(`not json`))
	assert.Equal(t, "", filters.Profession)
}

// ── sha256Hex (services-level) ──────────────────────────────────────

func TestServicesSha256Hex(t *testing.T) {
	h := sha256Hex("hello")
	assert.Len(t, h, 64)
}

// ── Search — two-pass and conversational ────────────────────────────

func TestSearchTwoPassWithFilters(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "[SEARCH]{\"profession\":\"plumber\",\"city\":\"Madrid\"}[/SEARCH]"}
	chatRepo := &testingutil.MockChatRepo{ReturnID: "s1"}
	svc := NewSearchService(llm, &testingutil.MockProfiles{}, chatRepo, &testingutil.MockPrompts{})

	result, err := svc.Search(t.Context(), "user-1", "need a plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	// Two-pass: SearchResult has Workers, not DetectedFields
	assert.Equal(t, "s1", result.ConversationID)
}

func TestSearchConversationalNoFilters(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "Hello! What kind of help do you need?"}
	chatRepo := &testingutil.MockChatRepo{ReturnID: "s2"}
	svc := NewSearchService(llm, &testingutil.MockProfiles{}, chatRepo, &testingutil.MockPrompts{})

	result, err := svc.Search(t.Context(), "user-1", "hi", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	assert.Nil(t, result.Workers)
	assert.Equal(t, "s2", result.ConversationID)
}

func TestSearchLLMError(t *testing.T) {
	llm := &testingutil.MockLLM{AskErr: assert.AnError}
	svc := NewSearchService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	_, err := svc.Search(t.Context(), "user-1", "plumber", nil, "", "", "", nil, nil)
	assert.Error(t, err)
}

func TestSearchPromptLoadError(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "test"}
	prompts := &testingutil.MockPrompts{GetErr: assert.AnError}
	svc := NewSearchService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, prompts)

	_, err := svc.Search(t.Context(), "user-1", "plumber", nil, "", "", "", nil, nil)
	assert.Error(t, err)
}

// ── GPS injection into filters (lines 141-147) ────────────────────

func TestSearchGPSInjectionFromRequest(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: `[SEARCH]{"profession":"plumber","city":"Madrid"}[/SEARCH]`}
	svc := NewSearchService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	lat, lng := 40.4168, -3.7038
	result, err := svc.Search(t.Context(), "user-1", "need a plumber", nil, "", "", "", &lat, &lng)
	require.NoError(t, err)
	require.NotNil(t, result)
	// Two-pass flow executed with GPS coords injected
	assert.Equal(t, "ilike", result.Branch)
}

func TestSearchGPSInjectionFromClientProfile(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: `[SEARCH]{"profession":"plumber","city":"Madrid"}[/SEARCH]`}
	clat, clng := 40.4168, -3.7038
	profiles := &testingutil.MockProfiles{
		ClientProfile: &core.ClientProfile{
			City:      "Madrid",
			Latitude:  &clat,
			Longitude: &clng,
		},
	}
	svc := NewSearchService(llm, profiles, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	result, err := svc.Search(t.Context(), "user-1", "need a plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "ilike", result.Branch)
}

// ── Embed failure fallback (lines 169-172) ────────────────────────

func TestSearchEmbedFailureFallback(t *testing.T) {
	llm := &testingutil.MockLLM{
		Answer:  `[SEARCH]{"profession":"plumber","city":"Madrid"}[/SEARCH]`,
		EmbedFn: func(_ context.Context, _ string) ([]float32, error) { return nil, assert.AnError },
	}
	svc := NewSearchService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	result, err := svc.Search(t.Context(), "", "need a plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "ilike", result.Branch)
}

// ── FindWorkers error (lines 235-237) ─────────────────────────────

func TestSearchFindWorkersError(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: `[SEARCH]{"profession":"plumber","city":"Madrid"}[/SEARCH]`}
	profiles := &testingutil.MockProfiles{WorkersErr: assert.AnError}
	svc := NewSearchService(llm, profiles, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	_, err := svc.Search(t.Context(), "", "need a plumber", nil, "", "", "", nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "find workers")
}

// ── Pass 2 LLM error (lines 248-250) ─────────────────────────────

func TestSearchPass2LLMError(t *testing.T) {
	pass1Answer := `[SEARCH]{"profession":"plumber","city":"Madrid"}[/SEARCH]`
	llm2 := &pass2ErrorLLM{pass1Answer: pass1Answer}
	profiles := &testingutil.MockProfiles{
		Workers: []core.WorkerProfile{{ID: "w1", Profession: "plumber", City: "Madrid"}},
	}
	svc := NewSearchService(llm2, profiles, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	_, err := svc.Search(t.Context(), "user-1", "need a plumber", nil, "", "", "", nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pass 2")
}

// pass2ErrorLLM returns pass1 answer on first Ask, error on second.
type pass2ErrorLLM struct {
	pass1Answer string
	callCount   int
}

func (l *pass2ErrorLLM) Ask(_ context.Context, _, _ string, _ []ports.MessagePair, _ string) (*ports.LLMResponse, error) {
	l.callCount++
	if l.callCount == 1 {
		return &ports.LLMResponse{Answer: l.pass1Answer}, nil
	}
	return nil, assert.AnError
}
func (l *pass2ErrorLLM) Health(_ context.Context) error { return nil }
func (l *pass2ErrorLLM) Embed(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, 768), nil
}

// ── SaveMessages error on search path (lines 265-267) ─────────────

func TestSearchSaveConversationError(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: `[SEARCH]{"profession":"plumber","city":"Madrid"}[/SEARCH]`}
	chatRepo := &testingutil.MockChatRepo{SaveErr: assert.AnError}
	svc := NewSearchService(llm, &testingutil.MockProfiles{}, chatRepo, &testingutil.MockPrompts{})

	result, err := svc.Search(t.Context(), "user-1", "need a plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	// Save failed but search still returns result with empty ConversationID
	assert.Equal(t, "", result.ConversationID)
}

// ── buildWorkerSummaries with DistanceKm (lines 406-408) ──────────

func TestBuildWorkerSummariesWithDistanceKm(t *testing.T) {
	dist := 3.7
	workers := []core.WorkerProfile{
		{
			ID:         "w1",
			Profession: "plumber",
			City:       "Madrid",
			DistanceKm: &dist,
		},
	}
	summaries := buildWorkerSummaries(workers, "need a plumber")
	assert.Contains(t, summaries, "distance: 3.7 km")
}
