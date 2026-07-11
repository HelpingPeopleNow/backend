package services

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
)

type SeedService struct {
	prompts ports.SystemPromptRepository
}

func NewSeedService(prompts ports.SystemPromptRepository) *SeedService {
	return &SeedService{prompts: prompts}
}

func (s *SeedService) SeedSystemPrompts(ctx context.Context) error {
	slog.Info("seed: loading system prompts")
	sp, err := s.prompts.Get(ctx)
	if err != nil {
		return fmt.Errorf("load system prompts: %w", err)
	}

	if sp.WorkerProfilePrompt == "" {
		if _, err := s.prompts.Update(ctx, "worker_profile_prompt", core.DefaultWorkerProfilePrompt); err != nil {
			return fmt.Errorf("seed worker_profile_prompt: %w", err)
		}
		slog.Info("seed: seeded", "column", "worker_profile_prompt")
	}
	if sp.ClientProfilePrompt == "" {
		if _, err := s.prompts.Update(ctx, "client_profile_prompt", core.DefaultClientProfilePrompt); err != nil {
			return fmt.Errorf("seed client_profile_prompt: %w", err)
		}
		slog.Info("seed: seeded", "column", "client_profile_prompt")
	}
	if sp.FindTraderSearchPrompt == "" {
		if _, err := s.prompts.Update(ctx, "find_trader_search_prompt", core.DefaultFindTraderSearchPrompt); err != nil {
			return fmt.Errorf("seed find_trader_search_prompt: %w", err)
		}
		slog.Info("seed: seeded", "column", "find_trader_search_prompt")
	}
	if sp.FindTraderPresentationPrompt == "" {
		if _, err := s.prompts.Update(ctx, "find_trader_presentation_prompt", core.DefaultFindTraderPresentationPrompt); err != nil {
			return fmt.Errorf("seed find_trader_presentation_prompt: %w", err)
		}
		slog.Info("seed: seeded", "column", "find_trader_presentation_prompt")
	}
	slog.Info("seed: all prompts present, no seeding needed")
	return nil
}
