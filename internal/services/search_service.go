package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
)

type SearchService struct {
	llm      ports.LLMService
	profiles ports.ProfileRepository
	chats    ports.ChatRepository
	prompts  ports.SystemPromptRepository
}

func NewSearchService(
	llm ports.LLMService,
	profiles ports.ProfileRepository,
	chats ports.ChatRepository,
	prompts ports.SystemPromptRepository,
) *SearchService {
	return &SearchService{llm: llm, profiles: profiles, chats: chats, prompts: prompts}
}

type SearchResult struct {
	Answer         string
	Workers        []core.WorkerProfile
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
	sp, err := s.prompts.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("load prompts: %w", err)
	}

	if sp.FindTraderSearchPrompt == "" {
		return &SearchResult{
			Answer: "Search is not configured yet. Please contact an administrator.",
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
		return &SearchResult{Answer: pass1Clean, ConversationID: convID}, nil
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

	workers, err := s.profiles.FindWorkers(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("find workers: %w", err)
	}

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

	return &SearchResult{
		Answer:         pass2Resp.Answer,
		Workers:        workers,
		ConversationID: convID,
	}, nil
}

func searchFiltersFromJSON(raw []byte) core.WorkerSearchFilters {
	var m map[string]interface{}
	if err := jsonUnmarshal(raw, &m); err != nil {
		return core.WorkerSearchFilters{}
	}
	filters := core.WorkerSearchFilters{}
	if v, ok := m["profession"].(string); ok {
		filters.Profession = v
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
