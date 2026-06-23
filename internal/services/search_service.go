package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
)

type SearchService struct {
	llm      ports.LLMService
	profiles ports.ProfileRepository
	chats    ports.ChatRepository
	prompts  ports.SystemPromptRepository

	// VECTOR_SEARCH_PLAN P2 / third-pass review — search cache.
	// Key: sha256(canonicalized searchParams JSON).
	// Entry expires either:
	//   - by age (TTL = 60s)
	//   - by worker-list mutation (workerFloor < MAX(worker_profiles.updated_at))
	// The floor mechanism avoids the marketplace failure mode where
	// new workers are invisible for the entire TTL window.
	searchCache     map[string]searchCacheEntry
	searchCacheTTL  time.Duration
	searchCacheMu   sync.RWMutex

	// floorMu / floorCached / floorCachedAt — 1-second-granular memoization
	// around SELECT MAX(updated_at) FROM worker_profiles so rapid refinement
	// ("plumber" → "plumber in Madrid") doesn't repeatedly hit the floor query.
	floorMu       sync.Mutex
	floorCached   time.Time
	floorCachedAt time.Time
}

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
		llm:           llm,
		profiles:      profiles,
		chats:         chats,
		prompts:       prompts,
		searchCache:   make(map[string]searchCacheEntry),
		searchCacheTTL: 60 * time.Second,
	}
}

type SearchResult struct {
	Answer         string
	Workers        []core.WorkerProfile
	TopScore       float64
	Branch         string // "vector" | "ilike" | "ilike_disabled_via_env" | "ilike_low_top_score"
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
	if userID != "" {
		if cp, err := s.profiles.GetClientProfile(ctx, userID); err == nil && cp != nil {
			clientCity = cp.City
		}
	}

	searchPrompt := sp.FindTraderSearchPrompt
	if clientCity != "" {
		searchPrompt = fmt.Sprintf("The client is based in %s. Use this as the default city unless they specify a different one.\n\n%s", clientCity, searchPrompt)
	}
	searchPrompt = applyLanguage(searchPrompt, lang)

	pass1Resp, err := s.llm.Ask(ctx, searchPrompt, message, history, provider)
	if err != nil {
		return nil, fmt.Errorf("pass 1: %w", err)
	}

	pass1Clean, searchParams := core.ParseSearch(pass1Resp.Answer)

	if searchParams == nil {
		slog.Info("search: pass 1 — no search params, returning conversational response", "answer_len", len(pass1Clean))
		convID := ""
		if userID != "" {
			id, err := s.chats.SaveMessages(ctx, userID, "client-find", message, pass1Clean, conversationID, nil, nil)
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
		slog.Warn("search: Embed failed, falling back to ILIKE",
			"error", embedErr)
	}

	cacheKey := sha256Hex(string(searchParams))
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
	branch := findResult.Branch  // fourth-pass review: post-fact, not intent.
	topScore := findResult.TopScore

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
		id, err := s.chats.SaveMessages(ctx, userID, "client-find", message, pass2Resp.Answer, conversationID, nil, meta)
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
	s.searchCache[cacheKey] = searchCacheEntry{
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
		return "electrician"
	case "fontanero", "plomero", "plumber":
		return "plumber"
	case "limpiador", "limpieza", "limpiadora", "cleaner", "cleaning":
		return "cleaner"
	case "manitas", "handyman", "handy man":
		return "handyman"
	case "carpintero", "carpenter":
		return "carpintero"
	case "pintor", "painter", "painting":
		return "painter"
	case "jardinero", "landscaper", "gardener":
		return "landscaper"
	case "tejador", "techo", "roofer", "roofing":
		return "roofer"
	case "clima", "aire acondicionado", "hvac", "hvac technician":
		return "hvac technician"
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
