package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
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
		{"electricista", "Electrician"},
		{"Electricista", "Electrician"},
		{"electrician", "Electrician"},
		{"fontanero", "Plumber"},
		{"plomero", "Plumber"},
		{"plumber", "Plumber"},
		{"limpieza", "Cleaner"},
		{"cleaner", "Cleaner"},
		{"manitas", "Handyman"},
		{"handyman", "Handyman"},
		{"carpintero", "Carpenter"},
		{"carpenter", "Carpenter"},
		{"pintor", "Painter"},
		{"painter", "Painter"},
		{"jardinero", "Landscaper"},
		{"landscaper", "Landscaper"},
		{"tejador", "Roofer"},
		{"roofer", "Roofer"},
		{"hvac", "HVAC Technician"},
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
	assert.Equal(t, "Plumber", filters.Profession)
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

// F5 parity test: normalizeProfession must return the same canonical English
// values as core.normalizeProfessionForEmbedding. Both must agree on every
// known key to prevent ILIKE mismatches between the embedding text and the
// search query.
func TestNormalizeProfessionParity(t *testing.T) {
	// Map of raw profession input → expected canonical English output.
	// Both normalizers MUST return these exact values (Title Case).
	expected := map[string]string{
		"plomero":      "Plumber",
		"plumber":      "Plumber",
		"fontanero":    "Plumber",
		"electricista": "Electrician",
		"electrician":  "Electrician",
		"carpintero":   "Carpenter",
		"carpenter":    "Carpenter",
		"pintor":       "Painter",
		"painter":      "Painter",
		"pintura":      "Painter",
		"jardinero":    "Landscaper",
		"landscaper":   "Landscaper",
		"gardener":     "Landscaper",
		"limpieza":     "Cleaner",
		"limpiador":    "Cleaner",
		"cleaner":      "Cleaner",
		"cleaning":     "Cleaner",
		"manitas":      "Handyman",
		"handyman":     "Handyman",
		"handy man":    "Handyman",
		"tejador":      "Roofer",
		"roofer":       "Roofer",
		"hvac":         "HVAC Technician",
	}
	for raw, want := range expected {
		got := normalizeProfession(raw)
		assert.Equal(t, want, got, "normalizeProfession(%q) = %q, want %q", raw, got, want)
	}
}

// F5 stricter regression: both normalizers (services + core) must return
// IDENTICAL strings — not just semantically equivalent values. This catches
// casing drift (e.g. "plumber" vs "Plumber") that a semantic-only test misses.
func TestNormalizeProfessionCasingParityWithEmbedding(t *testing.T) {
	inputs := []string{
		"plomero", "plumber", "fontanero",
		"electricista", "electrician",
		"carpintero", "carpenter",
		"pintor", "painter", "pintura",
		"jardinero", "landscaper", "gardener",
		"limpieza", "limpiador", "cleaner",
		"manitas", "handyman",
		"tejador", "roofer",
		"hvac",
	}
	for _, raw := range inputs {
		searchVal := normalizeProfession(raw)
		embedVal := core.NormalizeProfessionForEmbedding(raw)
		assert.Equal(t, embedVal, searchVal,
			"casing mismatch for %q: search=%q, embedding=%q", raw, searchVal, embedVal)
	}
}

// ── VECTOR_SEARCH_PLAN Phase 4: full-keyset parity ───────────────────
//
// Every alias in the shared map must canonicalize identically through
// both the search path and the embedding path.
func TestNormalizeProfessionFullKeysetParity(t *testing.T) {
	for alias := range core.ProfessionAliases {
		searchVal := normalizeProfession(alias)
		embedVal := core.NormalizeProfessionForEmbedding(alias)
		assert.Equal(t, embedVal, searchVal,
			"full-keyset parity mismatch for %q: search=%q, embedding=%q", alias, searchVal, embedVal)
	}
}

// ── VECTOR_SEARCH_PLAN Phase 1: GPS precedence ────────────────────────

func TestResolveSearchCoordsRequestOverridesProfile(t *testing.T) {
	reqLat, reqLng := 1.0, 2.0
	profLat, profLng := 3.0, 4.0
	lat, lng := resolveSearchCoords(&reqLat, &reqLng, &profLat, &profLng)
	require.NotNil(t, lat)
	require.NotNil(t, lng)
	assert.Equal(t, 1.0, *lat)
	assert.Equal(t, 2.0, *lng)
}

func TestResolveSearchCoordsFallsBackToProfile(t *testing.T) {
	profLat, profLng := 3.0, 4.0
	lat, lng := resolveSearchCoords(nil, nil, &profLat, &profLng)
	require.NotNil(t, lat)
	require.NotNil(t, lng)
	assert.Equal(t, 3.0, *lat)
	assert.Equal(t, 4.0, *lng)
}

func TestResolveSearchCoordsNilWhenAbsent(t *testing.T) {
	lat, lng := resolveSearchCoords(nil, nil, nil, nil)
	assert.Nil(t, lat)
	assert.Nil(t, lng)
}

// ── VECTOR_SEARCH_PLAN Wiring: MaxDistanceKm extraction ─────────────

func TestSearchFiltersFromJSONMaxDistanceKm(t *testing.T) {
	filters := searchFiltersFromJSON([]byte(`{"profession":"plumber","city":"Madrid","max_distance_km":10}`))
	assert.Equal(t, "Plumber", filters.Profession)
	assert.Equal(t, "Madrid", filters.City)
	require.NotNil(t, filters.MaxDistanceKm)
	assert.Equal(t, 10, *filters.MaxDistanceKm)
}

func TestSearchFiltersFromJSONMaxDistanceKmZeroIgnored(t *testing.T) {
	filters := searchFiltersFromJSON([]byte(`{"profession":"plumber","city":"Madrid","max_distance_km":0}`))
	assert.Nil(t, filters.MaxDistanceKm)
}

// ── Helper LLM mocks for new tests ──────────────────────────────────

// capturingLLM records every message passed to Ask and returns
// pass1Answer on the first call, pass2Answer on subsequent calls.
type capturingLLM struct {
	mu          sync.Mutex
	messages    []string
	callCount   int
	pass1Answer string
	pass2Answer string
	EmbedFn     func(ctx context.Context, text string) ([]float32, error)
}

func (l *capturingLLM) Ask(_ context.Context, _ string, message string, _ []ports.MessagePair, _ string) (*ports.LLMResponse, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, message)
	l.callCount++
	if l.callCount == 1 {
		return &ports.LLMResponse{Answer: l.pass1Answer}, nil
	}
	return &ports.LLMResponse{Answer: l.pass2Answer}, nil
}
func (l *capturingLLM) Health(_ context.Context) error { return nil }
func (l *capturingLLM) Embed(_ context.Context, text string) ([]float32, error) {
	if l.EmbedFn != nil {
		return l.EmbedFn(nil, text)
	}
	return make([]float32, 768), nil
}

// countingLLM is like capturingLLM but only tracks callCount (no message storage).
type countingLLM struct {
	mu          sync.Mutex
	callCount   int
	pass1Answer string
	pass2Answer string
	EmbedFn     func(ctx context.Context, text string) ([]float32, error)
}

func (l *countingLLM) Ask(_ context.Context, _ string, _ string, _ []ports.MessagePair, _ string) (*ports.LLMResponse, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.callCount++
	if l.callCount == 1 {
		return &ports.LLMResponse{Answer: l.pass1Answer}, nil
	}
	return &ports.LLMResponse{Answer: l.pass2Answer}, nil
}
func (l *countingLLM) Health(_ context.Context) error { return nil }
func (l *countingLLM) Embed(_ context.Context, _ string) ([]float32, error) {
	if l.EmbedFn != nil {
		return l.EmbedFn(nil, "")
	}
	return make([]float32, 768), nil
}

// dynamicLLM generates answers via a callback keyed on call number.
type dynamicLLM struct {
	mu       sync.Mutex
	callNum  int
	answerFn func(callNum int) string
	EmbedFn  func(ctx context.Context, text string) ([]float32, error)
}

func (l *dynamicLLM) Ask(_ context.Context, _ string, _ string, _ []ports.MessagePair, _ string) (*ports.LLMResponse, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.callNum++
	return &ports.LLMResponse{Answer: l.answerFn(l.callNum)}, nil
}
func (l *dynamicLLM) Health(_ context.Context) error { return nil }
func (l *dynamicLLM) Embed(_ context.Context, _ string) ([]float32, error) {
	if l.EmbedFn != nil {
		return l.EmbedFn(nil, "")
	}
	return make([]float32, 768), nil
}

// ── F10: input truncation ────────────────────────────────────────────

func TestSearchInputTruncation(t *testing.T) {
	longMsg := strings.Repeat("a", 3000) // exceeds searchInputMaxLen (2048)
	llm := &capturingLLM{
		pass1Answer: "Hello! What can I help you with?", // conversational (no SEARCH tags)
		pass2Answer: "Here are some results!",
	}
	svc := NewSearchService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	_, err := svc.Search(t.Context(), "user-1", longMsg, nil, "", "", "", nil, nil)
	require.NoError(t, err)

	// The message passed to the LLM should be truncated + note appended.
	llm.mu.Lock()
	defer llm.mu.Unlock()
	require.Len(t, llm.messages, 1, "LLM should be called exactly once for conversational path")
	assert.Less(t, len(llm.messages[0]), searchInputMaxLen+100,
		"message passed to LLM should be shorter than original 3000 chars")
	assert.Contains(t, llm.messages[0], "[Truncated at 2048 characters]",
		"truncation note should be appended")
}

// ── Full-filter cache hit (single tier) ─────────────────────────────
//
// The full-filter cache is checked after Pass-1 (the cache key depends on
// the extracted filters), so Pass-1 still runs on resubmit. It skips
// FindWorkers and Pass-2, and strips per-user ConversationID.
func TestSearchFullFilterCacheHit(t *testing.T) {
	llm := &countingLLM{
		pass1Answer: `[SEARCH]{"profession":"plumber","city":"Madrid"}[/SEARCH]`,
		pass2Answer: "Here are some plumbers in Madrid!",
	}
	profiles := &testingutil.MockProfiles{
		Workers: []core.WorkerProfile{
			{ID: "w1", Profession: "plumber", City: "Madrid"},
		},
	}
	svc := NewSearchService(llm, profiles, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	// First search — should call LLM twice (Pass-1 + Pass-2).
	result1, err := svc.Search(t.Context(), "user-1", "need a plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result1)

	llm.mu.Lock()
	assert.Equal(t, 2, llm.callCount, "first search: LLM called twice (Pass-1 + Pass-2)")
	llm.mu.Unlock()

	// Second identical search — Pass-1 still runs (cache key needs filters),
	// but FindWorkers and Pass-2 are skipped by the full-filter cache.
	result2, err := svc.Search(t.Context(), "user-1", "need a plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result2)

	llm.mu.Lock()
	assert.Equal(t, 3, llm.callCount,
		"second search: only Pass-1 runs; FindWorkers + Pass-2 skipped by cache")
	llm.mu.Unlock()

	// Both results should be identical (same cached value).
	assert.Equal(t, result1.Answer, result2.Answer)
	// ConversationID should NOT be leaked from cache.
	assert.Equal(t, "", result2.ConversationID)
}

// ── F13: templated 0-result message ──────────────────────────────────

func TestSearchZeroResultsTemplatedMessage(t *testing.T) {
	llm := &countingLLM{
		pass1Answer: `[SEARCH]{"profession":"plumber","city":"Madrid"}[/SEARCH]`,
		pass2Answer: "This should never be called", // Pass-2 is skipped for 0 results
	}
	profiles := &testingutil.MockProfiles{
		Workers: []core.WorkerProfile{}, // 0 workers
	}
	svc := NewSearchService(llm, profiles, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	result, err := svc.Search(t.Context(), "user-1", "need a plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	// The answer should contain the templated "no workers found" message (Spanish default).
	assert.Contains(t, result.Answer, "no encontré",
		"zero-result path should return templated message")
	assert.Empty(t, result.Workers, "Workers slice should be empty")
	// Pass-2 should NOT have been called (only1 LLM call for Pass-1).
	llm.mu.Lock()
	assert.Equal(t, 1, llm.callCount,
		"only Pass-1 should be called; Pass-2 skipped for 0 results")
	llm.mu.Unlock()
}

// ── Cache key must include lang to avoid cross-language pollution ────

func TestSearchCacheKeyIncludesLang(t *testing.T) {
	llm := &dynamicLLM{
		answerFn: func(callNum int) string {
			if callNum%2 == 1 {
				return `[SEARCH]{"profession":"plumber","city":"Madrid"}[/SEARCH]`
			}
			return "Here are some plumbers in Madrid!"
		},
	}
	profiles := &testingutil.MockProfiles{
		Workers: []core.WorkerProfile{
			{ID: "w1", Profession: "plumber", City: "Madrid"},
		},
	}
	svc := NewSearchService(llm, profiles, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	// First search in English.
	_, err := svc.Search(t.Context(), "user-1", "need a plumber", nil, "", "en", "", nil, nil)
	require.NoError(t, err)

	// Second search in Spanish with identical filters — should NOT hit the
	// English cache; Pass-2 must run again to produce a Spanish answer.
	_, err = svc.Search(t.Context(), "user-1", "need a plumber", nil, "", "es", "", nil, nil)
	require.NoError(t, err)

	llm.mu.Lock()
	assert.Equal(t, 4, llm.callNum,
		"Spanish search should not reuse English Pass-2 result; expected 4 LLM calls total")
	llm.mu.Unlock()
}

// ── F8: cache eviction (no panic, cache bounded) ─────────────────────

func TestSearchCacheEviction(t *testing.T) {
	// dynamicLLM returns unique SEARCH filters for each search so that
	// every search gets a unique cacheKey, forcing cache growth + eviction.
	llm := &dynamicLLM{
		answerFn: func(callNum int) string {
			// Pair calls: callNum 1,2 → city_0; 3,4 → city_1; etc.
			city := fmt.Sprintf("city_%d", (callNum-1)/2)
			return fmt.Sprintf(`[SEARCH]{"profession":"plumber","city":"%s"}[/SEARCH]`, city)
		},
	}
	profiles := &testingutil.MockProfiles{
		Workers: []core.WorkerProfile{
			{ID: "w1", Profession: "plumber", City: "Madrid"},
		},
	}
	svc := NewSearchService(llm, profiles, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	// Perform 210 unique searches — each adds 1 cache entry (cacheKey).
	// After 200 searches the cache is at capacity (maxSearchCacheEntries).
	// Searches 201–210 trigger lazy eviction of the oldest entry before each insert.
	for i := 0; i < 210; i++ {
		msg := fmt.Sprintf("find worker %d in city %d", i, i)
		_, err := svc.Search(t.Context(), "user-1", msg, nil, "", "", "", nil, nil)
		require.NoError(t, err, "search %d should not fail", i)
	}

	// Verify: no panic, cache bounded.
	svc.searchCacheMu.RLock()
	cacheSize := len(svc.searchCache)
	svc.searchCacheMu.RUnlock()

	// Without eviction: 210 entries. With eviction, the oldest entry is removed
	// before each insert after the cache hits 200, so net +1 per search.
	assert.LessOrEqual(t, cacheSize, maxSearchCacheEntries,
		"cache should be bounded by lazy eviction (no unbounded growth)")
}

// ── embed-result cache (VECTOR_SEARCH_PLAN §8.6 Phase 2) ───────────
//
// The embed-result cache is keyed by sha256(message). On a cache hit,
// the expensive llm.Embed round-trip to the helper is skipped entirely.
// Note: Pass-1 still runs on a same-message resubmit because the
// full-filter cache key depends on the extracted filters — but Embed is
// the call we want to avoid on a refiner loop, not Ask.

// embedCountingLLM tracks Ask and Embed calls separately so we can
// assert that the embed-result cache truly skips Embed on the second
// identical message.
type embedCountingLLM struct {
	mu          sync.Mutex
	askCount    int
	embedCount  int
	pass1Answer string
	pass2Answer string
}

func (l *embedCountingLLM) Ask(_ context.Context, _ string, _ string, _ []ports.MessagePair, _ string) (*ports.LLMResponse, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.askCount++
	if l.askCount == 1 {
		return &ports.LLMResponse{Answer: l.pass1Answer}, nil
	}
	return &ports.LLMResponse{Answer: l.pass2Answer}, nil
}

func (l *embedCountingLLM) Health(_ context.Context) error { return nil }

func (l *embedCountingLLM) Embed(_ context.Context, _ string) ([]float32, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.embedCount++
	return make([]float32, 768), nil
}

func TestSearchEmbedResultCacheHit(t *testing.T) {
	llm := &embedCountingLLM{
		pass1Answer: `[SEARCH]{"profession":"plumber","city":"Madrid"}[/SEARCH]`,
		pass2Answer: "Here are some plumbers in Madrid!",
	}
	profiles := &testingutil.MockProfiles{
		Workers: []core.WorkerProfile{
			{ID: "w1", Profession: "plumber", City: "Madrid"},
		},
	}
	svc := NewSearchService(llm, profiles, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	// First search — Embed is called exactly once (cache miss → fetch).
	_, err := svc.Search(t.Context(), "user-1", "need a plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)

	llm.mu.Lock()
	assert.Equal(t, 2, llm.askCount, "first search should call Ask twice (Pass-1 + Pass-2)")
	assert.Equal(t, 1, llm.embedCount, "first search should call Embed exactly once (cache miss)")
	llm.mu.Unlock()

	// Second search with the SAME message — cache hit on sha256(message).
	// Ask still runs (Pass-1 needed for cache key), but Embed MUST NOT run.
	_, err = svc.Search(t.Context(), "user-1", "need a plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)

	llm.mu.Lock()
	assert.Equal(t, 3, llm.askCount,
		"second search: only Pass-1 runs (Pass-2 is skipped by the full-filter cache)")
	assert.Equal(t, 1, llm.embedCount,
		"second identical search: Embed MUST NOT be called (cache hit on sha256(message))")
	llm.mu.Unlock()
}

func TestSearchEmbedResultCacheKeyDiffersForDifferentMessages(t *testing.T) {
	llm := &embedCountingLLM{
		pass1Answer: `[SEARCH]{"profession":"plumber","city":"Madrid"}[/SEARCH]`,
		pass2Answer: "Here are some plumbers in Madrid!",
	}
	profiles := &testingutil.MockProfiles{
		Workers: []core.WorkerProfile{
			{ID: "w1", Profession: "plumber", City: "Madrid"},
		},
	}
	svc := NewSearchService(llm, profiles, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	// Two searches with DIFFERENT messages → different sha256 keys → Embed called for each.
	_, err := svc.Search(t.Context(), "user-1", "need a plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	_, err = svc.Search(t.Context(), "user-1", "need an electrician", nil, "", "", "", nil, nil)
	require.NoError(t, err)

	llm.mu.Lock()
	assert.Equal(t, 2, llm.embedCount,
		"different messages produce different cache keys; Embed must run for each")
	llm.mu.Unlock()

	// Sanity: the cache should hold two distinct entries now.
	svc.embedResultCacheMu.RLock()
	cacheSize := len(svc.embedResultCache)
	svc.embedResultCacheMu.RUnlock()
	assert.Equal(t, 2, cacheSize,
		"two distinct messages must produce two distinct embedResultCache entries")
}
