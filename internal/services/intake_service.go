package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/metrics"
	"github.com/HelpingPeopleNow/backend/internal/ports"
)

type IntakeService struct {
	llm      ports.LLMService
	profiles ports.ProfileRepository
	chats    ports.ChatRepository
	prompts  ports.SystemPromptRepository

	// VECTOR_SEARCH_PLAN §8.8 (Improvements #4, #5, Pitfall #2):
	//
	// reembedSem bounds *in-flight* re-embed goroutines so the
	// NUM_PARALLEL=1 Ollama slot isn't swamped.
	// reembedMu guards reembedTimers so concurrent ProcessIntake calls
	// don't race on the same user's pending timer.
	// reembedTimers holds at most ONE pending time.Timer per user.
	//
	// reembedEnabled (P2-2 audit remediation): global kill switch. When
	// false, scheduleReembed/ReembedWorker become no-ops so a melting Ollama
	// daemon can be paused without disabling the whole vector search.
	reembedEnabled bool
	reembedSem     chan struct{}
	reembedMu      sync.Mutex
	reembedTimers  map[string]*time.Timer
}

func NewIntakeService(
	llm ports.LLMService,
	profiles ports.ProfileRepository,
	chats ports.ChatRepository,
	prompts ports.SystemPromptRepository,
) *IntakeService {
	enabled := core.GetEnvBool("REEMBED_ENABLED", true)
	if !enabled {
		slog.Warn("intake: REEMBED_ENABLED=false — re-embedding is paused (existing vectors continue to serve searches)")
	}
	return &IntakeService{
		llm:            llm,
		profiles:       profiles,
		chats:          chats,
		prompts:        prompts,
		reembedEnabled: enabled,
		reembedSem:     make(chan struct{}, 3),
		reembedTimers:  make(map[string]*time.Timer),
	}
}

// IsReembedEnabled reports whether the re-embedding kill switch is on.
// P2-2 audit: needed by the admin toggle handler so it can return the
// current state and by tests to assert flip behavior.
func (s *IntakeService) IsReembedEnabled() bool {
	s.reembedMu.Lock()
	defer s.reembedMu.Unlock()
	return s.reembedEnabled
}

// SetReembedEnabled toggles the re-embedding kill switch at runtime.
// P2-2 audit: lets ops pause / resume embedding via the admin endpoint
// without redeploying. When set to false, in-flight ReembedWorker calls
// complete (semaphore is held), but new scheduleReembed/sweeper work
// becomes a no-op. Threadsafe under reembedMu (the same lock guarding
// the timer map).
func (s *IntakeService) SetReembedEnabled(enabled bool) {
	s.reembedMu.Lock()
	previous := s.reembedEnabled
	s.reembedEnabled = enabled
	s.reembedMu.Unlock()

	if previous != enabled {
		if enabled {
			slog.Info("intake: REEMBED_ENABLED toggled ON — new re-embeds will be scheduled")
		} else {
			slog.Warn("intake: REEMBED_ENABLED toggled OFF — re-embedding paused (in-flight may complete)")
		}
	}
}

type IntakeResult struct {
	Answer         string
	DetectedFields map[string]interface{}
	FieldsRaw      json.RawMessage
	ConversationID string
}

type IntakeMode string

const (
	IntakeModeWorker IntakeMode = "worker"
	IntakeModeClient IntakeMode = "client"
)

func (s *IntakeService) ProcessIntake(
	ctx context.Context,
	userID string,
	mode IntakeMode,
	message string,
	history []ports.MessagePair,
	provider string,
	lang string,
	conversationID string,
	latitude *float64,
	longitude *float64,
) (*IntakeResult, error) {
	slog.Info("intake: ProcessIntake", "user_id", userID, "mode", mode)
	sp, err := s.prompts.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("load system prompt: %w", err)
	}

	systemPrompt := s.selectPrompt(sp, mode)
	systemPrompt = applyLanguage(systemPrompt, lang)

	llmResp, err := s.llm.Ask(ctx, systemPrompt, message, history, provider)
	if err != nil {
		return nil, fmt.Errorf("llm call: %w", err)
	}

	answer, fieldsMap := core.ParseFieldsMap(llmResp.Answer)
	_, fieldsRaw := core.ParseFields(llmResp.Answer)

	if fieldsMap != nil && userID != "" {
		// Inject GPS coords into fields map — bypasses LLM extraction
		// since coordinates come from the browser, not conversation.
		if latitude != nil {
			fieldsMap["latitude"] = *latitude
		}
		if longitude != nil {
			fieldsMap["longitude"] = *longitude
		}
		var upsertErr error
		switch mode {
		case IntakeModeWorker:
			upsertErr = s.profiles.UpsertWorkerProfile(ctx, userID, fieldsMap)
		case IntakeModeClient:
			upsertErr = s.profiles.UpsertClientProfile(ctx, userID, fieldsMap)
		default:
			return nil, fmt.Errorf("unknown intake mode: %s", mode)
		}
		if upsertErr != nil {
			slog.Warn("intake: profile upsert failed", "mode", mode, "user_id", userID, "error", upsertErr)
		} else if mode == IntakeModeWorker {
			// VECTOR_SEARCH_PLAN §8.8: trigger a deferred re-embed with
			// per-user debouncing so a 5-message chat produces 1 timer
			// fire (Improvement #5).
			s.scheduleReembed(userID)
		}
	}

	convType := "worker"
	if mode == IntakeModeClient {
		convType = "client"
	}

	convID := ""
	if userID != "" {
		meta := map[string]interface{}{}
		if fieldsRaw != nil {
			meta["extracted_fields"] = fieldsRaw
		}
		id, err := s.chats.SaveMessages(ctx, userID, convType, message, answer, conversationID, fieldsRaw, meta, "")
		if err != nil {
			slog.Warn("intake: save conversation failed", "user_id", userID, "error", err)
		} else {
			convID = id
		}
	}

	return &IntakeResult{
		Answer:         answer,
		DetectedFields: fieldsMap,
		FieldsRaw:      fieldsRaw,
		ConversationID: convID,
	}, nil
}

// scheduleReembed is the per-user debouncer (Improvement #5).
//
// One pending timer per user. 5 rapid ProcessIntake calls produce 1
// timer fire, not 5: each call .Stop()s the existing timer, then arms
// a fresh one. The timer closure acquires the semaphore and calls
// reembedWorker; if the user is mid-chat at fire time, the embedding
// will reflect only the latest merged profile.
func (s *IntakeService) scheduleReembed(userID string) {
	if !s.reembedEnabled {
		return
	}
	s.reembedMu.Lock()
	defer s.reembedMu.Unlock()

	if t, ok := s.reembedTimers[userID]; ok {
		t.Stop()
	}
	s.reembedTimers[userID] = time.AfterFunc(60*time.Second, func() {
		s.reembedMu.Lock()
		delete(s.reembedTimers, userID)
		s.reembedMu.Unlock()

		s.ReembedWorker(userID)
	})
}

// ReembedWorker is the public entry point shared by both scheduleReembed
// and the §8.10 staleness sweeper (Improvement #4 / Pitfall #2 fix).
// Acquiring s.reembedSem here means the intake path and the sweeper
// keep competing for the same Ollama slot — no two uncoordinated caps.
//
// P2-2 audit: respects the REEMBED_ENABLED kill switch. Returns
// immediately (without touching the semaphore) when re-embedding is
// paused, so existing vectors continue to serve searches unchanged.
func (s *IntakeService) ReembedWorker(userID string) {
	slog.Info("intake: ReembedWorker", "user_id", userID)
	if !s.reembedEnabled {
		slog.Debug("reembedWorker: skipped (REEMBED_ENABLED=false)", "user_id", userID)
		metrics.IncrReembedSkipped("kill_switch")
		return
	}
	s.reembedSem <- struct{}{}
	defer func() { <-s.reembedSem }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s.reembedWorker(ctx, userID)
}

// reembedWorker is the actual re-embed implementation. It walks
// BuildFieldTexts(current_profile), compares each field's SHA-256 hash
// and model against GetWorkerEmbeddingHashes, and for each field where
// either differs, calls Embed(text) and upserts via the repository.
//
// Improvement #2: the comparison is hash AND model — a model rollout
// forces every field to re-embed, otherwise the table silently mixes
// latent spaces.
func (s *IntakeService) reembedWorker(ctx context.Context, userID string) {
	wp, err := s.profiles.GetWorkerProfile(ctx, userID)
	if err != nil || wp == nil {
		slog.Warn("reembedWorker: profile not found", "user_id", userID, "error", err)
		metrics.IncrReembedSkipped("no_profile")
		return
	}

	fieldTexts := core.BuildFieldTexts(wp)
	if len(fieldTexts) == 0 {
		slog.Debug("reembedWorker: no fields to embed", "user_id", userID)
		metrics.IncrReembedSkipped("no_fields")
		return
	}

	existing, err := s.profiles.GetWorkerEmbeddingHashes(ctx, userID)
	if err != nil {
		slog.Warn("reembedWorker: failed to fetch existing hashes",
			"user_id", userID, "error", err)
		// On read failure, force re-embed (treat as cache miss).
		existing = map[string]ports.EmbeddingMeta{}
	}

	const currentModel = "granite-embedding:278m"

	reembedded := 0
	skipped := 0
	for fieldName, text := range fieldTexts {
		newHash := core.HashField(text)
		prior, ok := existing[fieldName]
		if ok && prior.Hash == newHash && prior.Model == currentModel {
			skipped++
			metrics.IncrReembedSkipped("no_change")
			continue
		}
		vec, err := s.llm.Embed(ctx, text)
		if err != nil {
			slog.Warn("reembedWorker: Embed failed", "user_id", userID,
				"field", fieldName, "error", err)
			metrics.IncrReembedCompleted("embed_err")
			continue
		}
		if err := s.profiles.UpsertWorkerEmbedding(ctx, userID, fieldName, vec, newHash); err != nil {
			slog.Warn("reembedWorker: Upsert failed", "user_id", userID,
				"field", fieldName, "error", err)
			metrics.IncrReembedCompleted("upsert_err")
			continue
		}
		reembedded++
		metrics.IncrReembedCompleted("ok")
	}

	slog.Info("reembedWorker: done", "user_id", userID,
		"reembedded", reembedded, "skipped", skipped)
}

func (s *IntakeService) selectPrompt(sp *core.SystemPrompt, mode IntakeMode) string {
	switch mode {
	case IntakeModeWorker:
		if p := sp.EffectiveWorkerPrompt(); p != "" {
			return p
		}
		return core.DefaultWorkerProfilePrompt
	case IntakeModeClient:
		if p := sp.EffectiveClientPrompt(); p != "" {
			return p
		}
		return core.DefaultClientProfilePrompt
	default:
		return ""
	}
}

func applyLanguage(prompt, lang string) string {
	switch lang {
	case "es":
		return prompt + "\n\nIMPORTANTE: Responde SIEMPRE en español al usuario. Todas tus respuestas deben ser en español."
	case "en":
		return prompt + "\n\nIMPORTANT: Always respond in English to the user. All your responses must be in English."
	default:
		return prompt
	}
}
