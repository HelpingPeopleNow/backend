package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
)

type IntakeService struct {
	llm      ports.LLMService
	profiles ports.ProfileRepository
	chats    ports.ChatRepository
	prompts  ports.SystemPromptRepository
}

func NewIntakeService(
	llm ports.LLMService,
	profiles ports.ProfileRepository,
	chats ports.ChatRepository,
	prompts ports.SystemPromptRepository,
) *IntakeService {
	return &IntakeService{llm: llm, profiles: profiles, chats: chats, prompts: prompts}
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
) (*IntakeResult, error) {
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
		id, err := s.chats.SaveMessages(ctx, userID, convType, message, answer, conversationID, fieldsRaw, meta)
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
