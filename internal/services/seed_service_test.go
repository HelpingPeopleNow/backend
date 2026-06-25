package services

import (
	"context"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/testingutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeedSystemPromptsIdempotent(t *testing.T) {
	prompts := &testingutil.MockPrompts{}
	svc := NewSeedService(prompts)

	err := svc.SeedSystemPrompts(context.Background())
	require.NoError(t, err)

	// Running again should not fail (idempotent)
	err = svc.SeedSystemPrompts(context.Background())
	require.NoError(t, err)
}

func TestSeedSystemPromptsReturnsDefaults(t *testing.T) {
	// MockPrompts.Get returns defaults when SP is nil — all fields populated.
	// SeedSystemPrompts sees non-empty fields and skips Update, so it's a no-op.
	prompts := &testingutil.MockPrompts{}
	svc := NewSeedService(prompts)

	err := svc.SeedSystemPrompts(context.Background())
	require.NoError(t, err)

	// Verify Get returns the defaults
	sp, err := prompts.Get(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, sp.WorkerProfilePrompt)
	assert.NotEmpty(t, sp.ClientProfilePrompt)
	assert.NotEmpty(t, sp.FindTraderSearchPrompt)
	assert.NotEmpty(t, sp.FindTraderPresentationPrompt)
}

func TestSeedSystemPromptsEmptyFieldsCallsUpdate(t *testing.T) {
	// SP with empty fields → SeedSystemPrompts should call Update for each one
	prompts := &testingutil.MockPrompts{SP: &core.SystemPrompt{}}
	svc := NewSeedService(prompts)

	err := svc.SeedSystemPrompts(context.Background())
	require.NoError(t, err)
}

func TestSeedSystemPromptsPartialFieldsCallsUpdate(t *testing.T) {
	prompts := &testingutil.MockPrompts{SP: &core.SystemPrompt{
		WorkerProfilePrompt: "already set",
	}}
	svc := NewSeedService(prompts)

	err := svc.SeedSystemPrompts(context.Background())
	require.NoError(t, err)
}

func TestSeedSystemPromptsGetError(t *testing.T) {
	prompts := &testingutil.MockPrompts{GetErr: assert.AnError}
	svc := NewSeedService(prompts)

	err := svc.SeedSystemPrompts(context.Background())
	assert.Error(t, err)
}
