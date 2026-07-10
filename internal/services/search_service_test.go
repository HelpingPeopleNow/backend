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

// ── F11: pre-key cache hit (skip Pass-1 + Embed) ────────────────────

func TestSearchPreKeyCacheHit(t *testing.T) {
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

	// First search — should call LLM twice (Pass-1 filter extraction + Pass-2 presentation).
	result1, err := svc.Search(t.Context(), "user-1", "need a plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result1)

	llm.mu.Lock()
	assert.Equal(t, 2, llm.callCount, "first search: LLM called twice (Pass-1 + Pass-2)")
	llm.mu.Unlock()

	// Second identical search — should return from pre-key cache without calling LLM.
	result2, err := svc.Search(t.Context(), "user-1", "need a plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result2)

	llm.mu.Lock()
	assert.Equal(t, 2, llm.callCount,
		"second search: LLM call count unchanged (pre-key cache hit, no Pass-1/Embed)")
	llm.mu.Unlock()

	// Both results should be identical (same cached value).
	assert.Equal(t, result1.Answer, result2.Answer)
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

	// Perform 210 unique searches — each adds 2 cache entries (cacheKey + preKey).
	// After 100 searches the cache is at 200 entries (maxSearchCacheEntries).
	// Searches 101–210 trigger lazy eviction of the oldest entry before each insert.
	for i := 0; i < 210; i++ {
		msg := fmt.Sprintf("find worker %d in city %d", i, i)
		_, err := svc.Search(t.Context(), "user-1", msg, nil, "", "", "", nil, nil)
		require.NoError(t, err, "search %d should not fail", i)
	}

	// Verify: no panic, cache bounded.
	svc.searchCacheMu.RLock()
	cacheSize := len(svc.searchCache)
	svc.searchCacheMu.RUnlock()

	// Without eviction: 210 * 2 = 420 entries. With eviction, the oldest entry
	// is removed before each insert after the cache hits 200, so net +1 per search.
	// Final size: ~200 + 110 = ~310. Assert well below the un-evicted 420.
	assert.Less(t, cacheSize, 210*2,
		"cache should be bounded by lazy eviction (no unbounded growth)")
}
