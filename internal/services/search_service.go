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
	"golang.org/x/sync/errgroup"
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

	// VECTOR_SEARCH_PLAN Phase 2 — single full-result cache.
	// Key: sha256(canonicalized filters JSON).
	// Entry expires either:
	//   - by age (TTL = 60s)
	//   - by worker-list mutation (signature changed)
	// The signature mechanism detects inserts, updates, and deletes.
	// Bounded by maxSearchCacheEntries (lazy eviction of oldest).
	searchCache    map[string]searchCacheEntry
	searchCacheTTL time.Duration
	searchCacheMu  sync.RWMutex

	// VECTOR_SEARCH_PLAN Phase 2 — embed-result cache.
	// Key: sha256(message). Caches the embedding vector for identical
	// resubmits so the expensive Embed call is skipped. Pure function of
	// message + model, so it needs no worker-signature invalidation.
	embedResultCache   map[string]embedCacheEntry
	embedResultCacheMu sync.RWMutex

	// signatureMu / signatureCached / signatureCachedAt — 1-second-granular
	// memoization around SELECT COUNT(*), MAX(updated_at) FROM worker_profiles
	// so rapid refinement doesn't repeatedly hit the DB.
	signatureMu       sync.Mutex
	signatureCached   workerSignature
	signatureCachedAt time.Time
}

const (
	maxSearchCacheEntries = 200  // F8: bound cache size
	searchInputMaxLen     = 2048 // F10: cap input at 2KB to prevent oversized prompts
)

type searchCacheEntry struct {
	result      *SearchResult
	cachedAt    time.Time
	workerFloor time.Time
	// signature tracks the worker table state at cache time.
	signature workerSignature
}

// workerSignature replaces the MAX(updated_at) floor with a count+max
// pair so deletes (which lower the count) also invalidate the cache.
type workerSignature struct {
	Count     int
	MaxUpdate time.Time
}

func (sig workerSignature) Equal(other workerSignature) bool {
	return sig.Count == other.Count && sig.MaxUpdate.Equal(other.MaxUpdate)
}

type embedCacheEntry struct {
	qvec     []float32
	cachedAt time.Time
}

type SearchResult struct {
	Answer         string
	Workers        []core.WorkerProfile
	TopScore       float64
	Branch         string // "vector" | "ilike" | "ilike_disabled_via_env" | "ilike_low_top_score" | "ilike_embed_failed"
	ConversationID string
}

func NewSearchService(
	llm ports.LLMService,
	profiles ports.ProfileRepository,
	chats ports.ChatRepository,
	prompts ports.SystemPromptRepository,
) *SearchService {
	return &SearchService{
		llm:              llm,
		profiles:         profiles,
		chats:            chats,
		prompts:          prompts,
		searchCache:      make(map[string]searchCacheEntry),
		searchCacheTTL:   60 * time.Second,
		embedResultCache: make(map[string]embedCacheEntry),
	}
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

	// ── VECTOR_SEARCH_PLAN P-D: parallelize Pass-1 ∥ Embed ──
	// Pass-1 consumes the prompt; Embed consumes the raw message. They
	// have no data dependency, so run them concurrently. Both must
	// complete before FindWorkers.
	//
	// Phase 2 embed-result cache: identical messages reuse the previously
	// computed vector without another round-trip to the helper.
	var pass1Resp *ports.LLMResponse
	var qvec []float32
	var pass1Err, embedErr error

	embedKey := sha256Hex(message)
	s.embedResultCacheMu.RLock()
	embedEntry, embedHit := s.embedResultCache[embedKey]
	s.embedResultCacheMu.RUnlock()
	if embedHit && time.Since(embedEntry.cachedAt) < s.searchCacheTTL && len(embedEntry.qvec) > 0 {
		qvec = embedEntry.qvec
		slog.Info("search: embed-result cache hit", "key", embedKey[:12])
	}

	g, gctx := errgroup.WithContext(ctx)

	// Pass-1: LLM extracts search filters.
	g.Go(func() error {
		pass1Resp, pass1Err = s.llm.Ask(gctx, searchPrompt, message, history, provider)
		return pass1Err
	})

	// Embed: raw message → vector (only if not served from cache).
	if !embedHit {
		g.Go(func() error {
			qvec, embedErr = s.llm.Embed(gctx, message)
			return nil // embed failure is handled gracefully below
		})
	}

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("pass 1: %w", err)
	}

	// Cache the freshly computed vector for identical resubmits.
	if !embedHit && embedErr == nil && len(qvec) > 0 {
		s.embedResultCacheMu.Lock()
		if len(s.embedResultCache) >= maxSearchCacheEntries {
			var oldestKey string
			var oldestTime time.Time
			for k, v := range s.embedResultCache {
				if oldestKey == "" || v.cachedAt.Before(oldestTime) {
					oldestKey = k
					oldestTime = v.cachedAt
				}
			}
			if oldestKey != "" {
				delete(s.embedResultCache, oldestKey)
			}
		}
		s.embedResultCache[embedKey] = embedCacheEntry{qvec: qvec, cachedAt: time.Now()}
		s.embedResultCacheMu.Unlock()
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
	// VECTOR_SEARCH_PLAN Phase 1: explicit GPS precedence.
	// 1. request coords (browser geolocation, most current)
	// 2. stored client profile coords
	// 3. nil → no distance sorting / proximity filter
	filters.Latitude, filters.Longitude = resolveSearchCoords(requestLat, requestLng, clientLat, clientLng)

	slog.Info("search: querying workers",
		"profession", filters.Profession,
		"city", filters.City,
		"emergency", filters.EmergencyOnly,
		"free_estimate", filters.FreeEstimateOnly,
		"insured", filters.InsuredOnly,
	)

	// Apply embedding if available.
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
	//
	// Include lang because the Pass-2 presentation prompt is translated;
	// without it, an English resubmit could return a cached Spanish answer.
	cacheKey := s.buildCacheKey(filters, lang)
	signature, _ := s.currentWorkerSignature(ctx)

	s.searchCacheMu.RLock()
	entry, ok := s.searchCache[cacheKey]
	s.searchCacheMu.RUnlock()
	cacheHit := false

	if ok && time.Since(entry.cachedAt) < s.searchCacheTTL && entry.signature.Equal(signature) {
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
		// Return a shallow copy so callers cannot mutate the cached object.
		copyVal := *entry.result
		return &copyVal, nil
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
		result := &SearchResult{
			Answer:         templatedAnswer,
			Workers:        workers,
			TopScore:       topScore,
			Branch:         branch,
			ConversationID: convID,
		}
		// Cache 0-result response under the full-filter key.
		// Strip ConversationID before caching so it is not leaked across users.
		cacheVal := *result
		cacheVal.ConversationID = ""
		s.writeSearchCache(cacheKey, &cacheVal, signature)
		return result, nil
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

	s.writeSearchCache(cacheKey, &cacheVal, signature)

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

// writeSearchCache stores a result with lazy eviction. It must be called
// with the searchCacheMu unlocked.
func (s *SearchService) writeSearchCache(key string, result *SearchResult, signature workerSignature) {
	s.searchCacheMu.Lock()
	defer s.searchCacheMu.Unlock()
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
	s.searchCache[key] = searchCacheEntry{
		result:    result,
		cachedAt:  time.Now(),
		signature: signature,
	}
}

// buildCacheKey returns a stable SHA-256 key for the resolved filters.
// lang is included because the Pass-2 presentation prompt is translated.
func (s *SearchService) buildCacheKey(filters core.WorkerSearchFilters, lang string) string {
	type cacheKeyParts struct {
		Profession    string
		City          string
		Latitude      float64
		Longitude     float64
		MaxDistanceKm float64
		Emergency     bool
		FreeEstimate  bool
		Insured       bool
		Lang          string
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
		Lang:          lang,
	}
	keyBytes, _ := json.Marshal(keyParts)
	return sha256Hex(string(keyBytes))
}

// currentWorkerSignature returns (COUNT(*), MAX(updated_at)) from
// worker_profiles, memoized for 1s so rapid refinement doesn't hammer
// Postgres. The count detects deletes; the max detects inserts/updates.
//
// Nil-DB guard: in tests, mockProfiles.RawQuery returns nil (mockProfiles
// has no real gorm.DB). Rather than panic, return a zero signature so
// cache invalidation falls back to age-only.
func (s *SearchService) currentWorkerSignature(ctx context.Context) (workerSignature, error) {
	s.signatureMu.Lock()
	defer s.signatureMu.Unlock()
	if time.Since(s.signatureCachedAt) < time.Second {
		return s.signatureCached, nil
	}

	tx := s.profiles.RawQuery(ctx, "SELECT COUNT(id), COALESCE(MAX(updated_at), 'epoch') FROM worker_profiles")
	if tx == nil {
		return workerSignature{}, nil
	}
	var sig workerSignature
	if err := tx.Row().Scan(&sig.Count, &sig.MaxUpdate); err != nil {
		return workerSignature{}, err
	}

	s.signatureCached = sig
	s.signatureCachedAt = time.Now()
	return sig, nil
}

// resolveSearchCoords returns the coordinates to use for a search,
// following the precedence:
//  1. requestLat/requestLng (browser geolocation, most current)
//  2. stored client profile coords
//  3. nil, nil (no distance sorting / proximity filter)
func resolveSearchCoords(requestLat, requestLng, clientLat, clientLng *float64) (*float64, *float64) {
	if requestLat != nil && requestLng != nil {
		return requestLat, requestLng
	}
	if clientLat != nil && clientLng != nil {
		return clientLat, clientLng
	}
	return nil, nil
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
	if v, ok := m["max_distance_km"].(float64); ok && v > 0 {
		d := int(v)
		filters.MaxDistanceKm = &d
	}
	return filters
}

// normalizeProfession delegates to the shared core.NormalizeProfession
// so search queries and embedding text canonicalize to identical strings.
func normalizeProfession(p string) string {
	return core.NormalizeProfession(p)
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
