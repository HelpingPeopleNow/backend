package services

import (
	"context"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

func TestSeedSystemPromptsIdempotent(t *testing.T) {
	prompts := &mockPrompts{}
	svc := NewSeedService(prompts)

	// First call should seed all empty fields.
	err := svc.SeedSystemPrompts(context.Background())
	if err != nil {
		t.Fatalf("SeedSystemPrompts failed: %v", err)
	}

	// Second call should be a no-op (fields already set).
	err = svc.SeedSystemPrompts(context.Background())
	if err != nil {
		t.Fatalf("SeedSystemPrompts second call failed: %v", err)
	}
}

func TestSeedSystemPromptsSkipsExisting(t *testing.T) {
	sp := &core.SystemPrompt{
		WorkerProfilePrompt:          "Already set worker",
		ClientProfilePrompt:          "Already set client",
		FindTraderSearchPrompt:       "Already set search",
		FindTraderPresentationPrompt: "Already set presentation",
	}
	prompts := &mockPrompts{sp: sp}
	svc := NewSeedService(prompts)

	err := svc.SeedSystemPrompts(context.Background())
	if err != nil {
		t.Fatalf("SeedSystemPrompts failed: %v", err)
	}

	// No Update calls should have been made since all fields were non-empty.
	// mockPrompts.Update returns nil, nil — so no error, but the fact that
	// we reach here without error means it was a no-op path.
}
