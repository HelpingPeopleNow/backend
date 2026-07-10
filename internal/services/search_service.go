package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
)

// SearchService orchestrates the two-pass LLM workflow for search mode.
//
// Architecture (VECTOR_SEARCH_PLAN §8.6):
//  1. Pass-1: LLM extracts search filters from user message
//  2. Embed: embed the raw message for pgvector cosine similarity
//  3. FindWorkers: hybrid pgvector + ILIKE query
//  4. Pass-2: LLM presents results conversationally
type SearchService struct {
	llm      ports.LLMService
	profiles ports.ProfileRepository
	chats    ports.ChatRepository
	prompts  ports.SystemPromptRepository

	// VECTOR_SEARCH_PLAN P2 / third-pass review — search cache.
	// Key: sha256(canonicalized filters JSON).
	// Entry expires either:
	//   - by age (TTL = 60s)
	//   - by worker-list mutation (workerFloor < MAX(worker_profiles.updated_at))
	// The floor mechanism avoids the marketplace failure mode where
	// new workers are invisible for the entire TTL window.
	// F8: bounded by maxSearchCacheEntries (lazy eviction of oldest).
	searchCache    map[string]searchCacheEntry
	searchCacheTTL time.Duration
	searchCacheMu  sync.RWMutex

	// floorMu / floorCached / floorCachedAt — 1-second-granular memoization
	// around SELECT MAX(updated_at) FROM worker_profiles so rapid refinement
	// ("plumber" → "plumber in Madrid") doesn't repeatedly hit the floor query.
	floorMu       sync.Mutex
	floorCached   time.Time
	floorCachedAt time.Time
}

const (
	maxSearchCacheEntries = 200  // F8: bound cache size
	searchInputMaxLen     = 2048 // F10: cap input at 2KB to prevent oversized prompts
)

type searchCacheEntry struct {
	result      *SearchResult
	cachedAt    time.Time
	workerFloor time.Time
}

func NewSearchService(
	llm ports.LLMService,
	profiles ports.ProfileRepository,
	chats ports.ChatRepository,
	prompts ports.SystemPromptRepository,
) *SearchService {
	return &SearchService{
		llm:            llm,
		profiles:       profiles,
		chats:          chats,
		prompts:        prompts,
		searchCache:    make(map[string]searchCacheEntry),
		searchCacheTTL: 60 * time.Second,
	}
}

type SearchResult struct {
	Answer         string
	Workers        []core.WorkerProfile
	TopScore       float64
	Branch         string // "vector" | "ilike" | "ilike_disabled_via_env" | "ilike_low_top_score" | "ilike_embed_failed"
	ConversationID string
}

func (s *SearchService) Search(
	ctx context.Context,
	userID string,
	message string,
	history []ports.MessagePair,
	provider string,
	lang string,
	conversationID string,
	requestLat *float64,
	requestLng *float64,
) (*SearchResult, error) {
	start := time.Now()

	sp, err := s.prompts.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("load prompts: %w", err)
	}

	if sp.FindTraderSearchPrompt == "" {
		return &SearchResult{
			Answer: "Search is not configured yet. Please contact an administrator.",
			Branch: "ilike_disabled_via_env",
		}, nil
	}

	clientCity := ""
	var clientLat, clientLng *float64
	if userID != "" {
		if cp, err := s.profiles.GetClientProfile(ctx, userID); err == nil && cp != nil {
			clientCity = cp.City
			clientLat = cp.Latitude
			clientLng = cp.Longitude
		}
	}

	searchPrompt := sp.FindTraderSearchPrompt
	if clientCity != "" {
		searchPrompt = fmt.Sprintf("The client is based in %s. Use this as the default city unless they specify a different one.\n\n%s", clientCity, searchPrompt)
	}
	searchPrompt = applyLanguage(searchPrompt, lang)

	// F10: cap input at 2KB to prevent oversized prompts.
	if len(message) > searchInputMaxLen {
		message = message[:searchInputMaxLen] + "\n\n[Truncated at 2048 characters]"
	}

	// F11: cheap pre-key check before Pass-1/Embed — hash just the raw
	// message + city to short-circuit identical repeat queries.
	preKeyBytes, _ := json.Marshal(struct{ Msg, City string }{message, clientCity})
	preKey := sha256Hex(string(preKeyBytes))
	s.searchCacheMu.RLock()
	preEntry, preOk := s.searchCache[preKey]
	s.searchCacheMu.RUnlock()
	if preOk && time.Since(preEntry.cachedAt) < s.searchCacheTTL {
		slog.Info("search: pre-key cache hit (skipped Pass-1+Embed)",
			"key", preKey[:12], "branch", preEntry.result.Branch)
		return preEntry.result, nil
	}

	pass1Resp, err := s.llm.Ask(ctx, searchPrompt, message, history, provider)
	if err != nil {
		return nil, fmt.Errorf("pass 1: %w", err)
	}

	pass1Clean, searchParams := core.ParseSearch(pass1Resp.Answer)

	if searchParams == nil {
		slog.Info("search: pass 1 — no search params, returning conversational response", "answer_len", len(pass1Clean))
		convID := ""
		if userID != "" {
			id, err := s.chats.SaveMessages(ctx, userID, "client-find", message, pass1Clean, conversationID, nil, nil, "")
			if err != nil {
				slog.Warn("search: save conversation failed", "error", err)
			} else {
				convID = id
			}
		}
		return &SearchResult{Answer: pass1Clean, ConversationID: convID, Branch: "ilike"}, nil
	}

	filters := searchFiltersFromJSON(searchParams)
	if filters.City == "" {
		filters.City = clientCity
	}
	// Inject client GPS coords into search filters — request coords
	// override stored profile coords (more recent / accurate).
	if requestLat != nil && requestLng != nil {
		filters.Latitude = requestLat
		filters.Longitude = requestLng
	} else if clientLat != nil && clientLng != nil {
		filters.Latitude = clientLat
		filters.Longitude = clientLng
	}

	slog.Info("search: querying workers",
		"profession", filters.Profession,
		"city", filters.City,
		"emergency", filters.EmergencyOnly,
		"free_estimate", filters.FreeEstimateOnly,
		"insured", filters.InsuredOnly,
	)

	// ── VECTOR_SEARCH_PLAN §8.6: hybrid branch ──
	//
	// If the embedding call succeeds AND we have a non-zero QueryVector, the
	// repository's FindWorkers will run findWorkersVector first and fall back
	// to findWorkersILIKE on error or low top-score — but we ALSO cache the
	// whole SearchResult keyed on the canonicalized search params to absorb
	// "plumber" → "plumber in Madrid" → "plumber in Madrid insured" refinement
	// (P2 — TTL 60 s + worker-floor invalidation).

	qvec, embedErr := s.llm.Embed(ctx, message)
	if embedErr == nil && len(qvec) > 0 {
		filters.QueryVector = qvec
	} else if embedErr != nil {
		// F4: flag embed failure so FindWorkers returns ilike_embed_failed
		filters.EmbedFailed = true
		slog.Warn("search: Embed failed, falling back to ILIKE",
			"error", embedErr)
	}

	// F1/F16 fix: key on resolved filters (post city-default + GPS
	// injection), not raw LLM searchParams. This prevents cross-city
	// and cross-geo cache leaks where two users in different locations
	// with the same query share a cache entry.
	type cacheKeyParts struct {
		Profession    string
		City          string
		Latitude      float64
		Longitude     float64
		MaxDistanceKm float64
		Emergency     bool
		FreeEstimate  bool
		Insured       bool
	}
	var latVal, lngVal, maxDist float64
	if filters.Latitude != nil {
		latVal = *filters.Latitude
	}
	if filters.Longitude != nil {
		lngVal = *filters.Longitude
	}
	if filters.MaxDistanceKm != nil {
		maxDist = float64(*filters.MaxDistanceKm)
	}
	keyParts := cacheKeyParts{
		Profession:    filters.Profession,
		City:          filters.City,
		Latitude:      latVal,
		Longitude:     lngVal,
		MaxDistanceKm: maxDist,
		Emergency:     filters.EmergencyOnly,
		FreeEstimate:  filters.FreeEstimateOnly,
		Insured:       filters.InsuredOnly,
	}
	keyBytes, _ := json.Marshal(keyParts)
	cacheKey := sha256Hex(string(keyBytes))
	floor, _ := s.currentWorkerFloor(ctx)

	s.searchCacheMu.RLock()
	entry, ok := s.searchCache[cacheKey]
	s.searchCacheMu.RUnlock()
	cacheHit := false
	if ok && time.Since(entry.cachedAt) < s.searchCacheTTL && !floor.After(entry.workerFloor) {
		cacheHit = true
		slog.Info("search: cache hit",
			"key", cacheKey[:12], "age_s", int(time.Since(entry.cachedAt).Seconds()))
		// Idea C structured log (post-resolve)
		slog.Info("search",
			"user_id", userID, "branch", entry.result.Branch,
			"top_score", entry.result.TopScore,
			"result_count", len(entry.result.Workers),
			"cache_hit", true,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return entry.result, nil
	}

	// Cache miss (or invalidation): run the repository. It picks ILIKE or
	// vector internally based on filters.QueryVector + env kill switch
	// AND reports the branch it actually used.
	findResult, err := s.profiles.FindWorkers(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("find workers: %w", err)
	}
	workers := findResult.Workers
	branch := findResult.Branch // fourth-pass review: post-fact, not intent.
	topScore := findResult.TopScore

	// F13: skip Pass-2 when no workers found — return a templated
	// message instead of wasting an LLM completion on 0 results.
	if len(workers) == 0 {
		slog.Info("search: 0 workers found, skipping Pass-2", "branch", branch)
		templatedAnswer := "Lo siento, no encontré trabajadores que coincidan con tu búsqueda en este momento. " +
			"¿Puedo ayudarte con algo más?"
		if lang == "en" {
			templatedAnswer = "Sorry, I couldn't find any workers matching your search right now. " +
				"Can I help you with anything else?"
		}
		// Still save the conversation so the conversation ID is returned.
		convID := ""
		if userID != "" {
			meta := map[string]interface{}{
				"search_params": searchParams,
				"workers_found": 0,
			}
			id, err := s.chats.SaveMessages(ctx, userID, "client-find", message, templatedAnswer, conversationID, nil, meta, "")
			if err != nil {
				slog.Warn("search: save conversation failed (0-results path)", "error", err)
			} else {
				convID = id
			}
		}
		// Cache 0-result response so repeat queries hit the cache.
		cacheVal := SearchResult{
			Answer:         templatedAnswer,
			Workers:        workers,
			TopScore:       topScore,
			Branch:         branch,
			ConversationID: "",
		}
		newFloor, _ := s.currentWorkerFloor(ctx)
		s.searchCacheMu.Lock()
		s.searchCache[preKey] = searchCacheEntry{result: &cacheVal, cachedAt: time.Now(), workerFloor: newFloor}
		s.searchCacheMu.Unlock()
		return &SearchResult{
			Answer:         templatedAnswer,
			Workers:        workers,
			TopScore:       topScore,
			Branch:         branch,
			ConversationID: convID,
		}, nil
	}

	// Pre-bound presentation prompt (cached lookup already done above).
	presentationPrompt := sp.EffectiveFindTraderPresentation()
	presentationPrompt = applyLanguage(presentationPrompt, lang)

	pass2Question := buildWorkerSummaries(workers, message)
	pass2Resp, err := s.llm.Ask(ctx, presentationPrompt, pass2Question, nil, provider)
	if err != nil {
		return nil, fmt.Errorf("pass 2: %w", err)
	}

	convID := ""
	if userID != "" {
		meta := map[string]interface{}{
			"search_params": searchParams,
			"workers_found": len(workers),
		}
		workersJSON := ""
		if cards := core.FindTraderCards(workers); len(cards) > 0 {
			if b, err := json.Marshal(cards); err == nil {
				workersJSON = string(b)
			}
		}
		id, err := s.chats.SaveMessages(ctx, userID, "client-find", message, pass2Resp.Answer, conversationID, nil, meta, workersJSON)
		if err != nil {
			slog.Warn("search: save conversation failed", "error", err)
		} else {
			convID = id
		}
	}

	result := &SearchResult{
		Answer:         pass2Resp.Answer,
		Workers:        workers,
		TopScore:       topScore,
		Branch:         branch,
		ConversationID: convID,
	}

	// Cache write. Critical: strip ConversationID before caching so two
	// users with the same searchParams don't see user A's conv-ID when
	// user B hits the cache (third-pass P2 hygiene bug). And finalize
	// Branch only AFTER FindWorkers so the slog doesn't lie when the
	// vector branch falls back to ILIKE.
	cacheVal := *result
	cacheVal.ConversationID = "" // P2 cache hygiene — never leak per-user IDs.

	newFloor, _ := s.currentWorkerFloor(ctx)
	s.searchCacheMu.Lock()
	// F8: lazy eviction — if cache is at capacity, evict the oldest entry
	if len(s.searchCache) >= maxSearchCacheEntries {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range s.searchCache {
			if oldestKey == "" || v.cachedAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.cachedAt
			}
		}
		if oldestKey != "" {
			delete(s.searchCache, oldestKey)
		}
	}
	s.searchCache[cacheKey] = searchCacheEntry{
		result: &cacheVal, cachedAt: time.Now(), workerFloor: newFloor,
	}
	// F11: also store the pre-key (message+city) for future short-circuit
	s.searchCache[preKey] = searchCacheEntry{
		result: &cacheVal, cachedAt: time.Now(), workerFloor: newFloor,
	}
	s.searchCacheMu.Unlock()

	// Idea C: structured slog per search so grep+jq can eyeball branches
	// before wiring percentile-tracking machinery (N6 — V1.1).
	//
	// Metrics counters (IncrVectorSearch + ObserveVectorScore) live in
	// internal/adapters/handler/metrics_handler.go and require a
	// handler-level integration. We intentionally don't import the
	// handler package from services (cycle: handlers → services is
	// allowed; services → handlers breaks the layer rules).
	// The slog line below is the V1 observability surface.
	slog.Info("search",
		"user_id", userID, "branch", branch,
		"top_score", result.TopScore,
		"result_count", len(result.Workers),
		"cache_hit", cacheHit,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return result, nil
}

// SearchCacheSize returns the current size of the search-cache. Surfaced
// as the `search_cache_size` gauge so /metrics can alert when the cache
// is near its 200-entry cap (P2-1 audit / F6 observability).
func (s *SearchService) SearchCacheSize() int {
	s.searchCacheMu.RLock()
	defer s.searchCacheMu.RUnlock()
	return len(s.searchCache)
}

// currentWorkerFloor returns MAX(worker_profiles.updated_at), but
// memoizes the result for 1s so rapid refinement doesn't hammer Postgres.
// P2 (third-pass review) requires the floor mechanism for cache
// invalidation correctness.
//
// Nil-DB guard: in tests, mockProfiles.RawQuery returns nil (mockProfiles
// has no real gorm.DB). Rather than panic, return zero time so cache
// invalidation falls back to age-only — safe in tests because the test
// runs against in-memory state anyway.
func (s *SearchService) currentWorkerFloor(ctx context.Context) (time.Time, error) {
	s.floorMu.Lock()
	defer s.floorMu.Unlock()
	if time.Since(s.floorCachedAt) < time.Second {
		return s.floorCached, nil
	}
	tx := s.profiles.RawQuery(ctx, "SELECT MAX(updated_at) FROM worker_profiles")
	if tx == nil {
		return time.Time{}, nil
	}
	var floor time.Time
	if err := tx.Scan(&floor).Error; err != nil {
		return time.Time{}, err
	}
	s.floorCached = floor
	s.floorCachedAt = time.Now()
	return floor, nil
}

func searchFiltersFromJSON(raw []byte) core.WorkerSearchFilters {
	var m map[string]interface{}
	if err := jsonUnmarshal(raw, &m); err != nil {
		return core.WorkerSearchFilters{}
	}
	filters := core.WorkerSearchFilters{}
	if v, ok := m["profession"].(string); ok {
		filters.Profession = normalizeProfession(v)
	}
	if v, ok := m["city"].(string); ok {
		filters.City = v
	}
	if v, ok := m["emergency"].(bool); ok {
		filters.EmergencyOnly = v
	}
	if v, ok := m["free_estimate"].(bool); ok {
		filters.FreeEstimateOnly = v
	}
	if v, ok := m["insured"].(bool); ok {
		filters.InsuredOnly = v
	}
	return filters
}

// normalizeProfession maps Spanish (and other common) profession names to the
// English canonical values stored in worker_profiles. This is a safety net —
// the search prompt should already instruct the LLM to use English names, but
// when users speak Spanish the LLM often returns the Spanish term anyway.
//
// KEEP IN SYNC with core.normalizeProfessionForEmbedding.
func normalizeProfession(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "electricista", "electrician", "electric":
		return "Electrician"
	case "fontanero", "plomero", "plumber":
		return "Plumber"
	case "limpiador", "limpieza", "limpiadora", "cleaner", "cleaning":
		return "Cleaner"
	case "manitas", "handyman", "handy man":
		return "Handyman"
	case "carpintero", "carpenter":
		return "Carpenter"
	case "pintor", "pintura", "painter", "painting":
		return "Painter"
	case "jardinero", "landscaper", "gardener":
		return "Landscaper"
	case "tejador", "techo", "roofer", "roofing":
		return "Roofer"
	case "clima", "aire acondicionado", "hvac", "hvac technician":
		return "HVAC Technician"
	default:
		return p
	}
}

func buildWorkerSummaries(workers []core.WorkerProfile, originalMessage string) string {
	if len(workers) == 0 {
		return fmt.Sprintf("No workers matched the search criteria. Let the user know empathetically and suggest they broaden their search.\n\nUser's original request: %s", originalMessage)
	}
	var sb strings.Builder
	sb.WriteString("Here are the matching workers:\n")
	for i, w := range workers {
		sb.WriteString(w.SearchSummary(i + 1))
		if w.DistanceKm != nil {
			sb.WriteString(fmt.Sprintf(", distance: %.1f km", *w.DistanceKm))
		}
		if w.Phone != "" {
			sb.WriteString(fmt.Sprintf(", phone: %s", w.Phone))
		}
		if w.Bio != "" {
			sb.WriteString(fmt.Sprintf(", bio: %s", w.Bio))
		}
		if certs := workerCerts(w); len(certs) > 0 {
			sb.WriteString(fmt.Sprintf(", certifications: %s", strings.Join(certs, ", ")))
		}
		if w.HasInsurance {
			sb.WriteString(", insured")
		}
		if w.EmergencyService {
			sb.WriteString(", emergency service")
		}
		if w.FreeEstimate {
			sb.WriteString(", free estimates")
		}
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("\nUser's original request: %s", originalMessage))
	return sb.String()
}

// sha256Hex is a small helper duplicated from internal/core. Stays local
// to avoid cycle imports (services → core would already be a cycle).
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
