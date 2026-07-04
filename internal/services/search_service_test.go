package services

import (
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/core"
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
