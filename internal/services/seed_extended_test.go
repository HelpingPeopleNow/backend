package services

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/testingutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── SeedSystemPrompts ────────────────────────────────────────────────

func TestSeedSystemPromptsAllFourEmpty(t *testing.T) {
	// SystemPrompt with all four fields empty → all four Update calls fire.
	updateCount := atomic.Int32{}
	prompts := &countingUpdatePrompts{
		MockPrompts: &testingutil.MockPrompts{SP: &core.SystemPrompt{}},
		count:       &updateCount,
	}
	svc := NewSeedService(prompts)

	err := svc.SeedSystemPrompts(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(4), updateCount.Load(), "all four Update calls should fire")
}

func TestSeedSystemPromptsGetFailure(t *testing.T) {
	prompts := &testingutil.MockPrompts{GetErr: errors.New("db connection lost")}
	svc := NewSeedService(prompts)

	err := svc.SeedSystemPrompts(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load system prompts")
}

func TestSeedSystemPromptsWorkerUpdateFailure(t *testing.T) {
	// First Update (worker_profile_prompt) fails.
	updateCount := atomic.Int32{}
	prompts := &failingUpdatePrompts{
		MockPrompts: &testingutil.MockPrompts{SP: &core.SystemPrompt{}},
		count:       &updateCount,
		failOn:      1, // fail on first Update call
	}
	svc := NewSeedService(prompts)

	err := svc.SeedSystemPrompts(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "worker_profile_prompt")
	// Should only have fired 1 Update (the one that failed) then returned.
	assert.Equal(t, int32(1), updateCount.Load())
}

func TestSeedSystemPromptsClientUpdateFailure(t *testing.T) {
	// Second Update (client_profile_prompt) fails.
	updateCount := atomic.Int32{}
	prompts := &failingUpdatePrompts{
		MockPrompts: &testingutil.MockPrompts{SP: &core.SystemPrompt{}},
		count:       &updateCount,
		failOn:      2, // fail on second Update call
	}
	svc := NewSeedService(prompts)

	err := svc.SeedSystemPrompts(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client_profile_prompt")
	// First Update succeeds, second fails.
	assert.Equal(t, int32(2), updateCount.Load())
}

func TestSeedSystemPromptsSearchUpdateFailure(t *testing.T) {
	// Third Update (find_trader_search_prompt) fails.
	updateCount := atomic.Int32{}
	prompts := &failingUpdatePrompts{
		MockPrompts: &testingutil.MockPrompts{SP: &core.SystemPrompt{}},
		count:       &updateCount,
		failOn:      3, // fail on third Update call
	}
	svc := NewSeedService(prompts)

	err := svc.SeedSystemPrompts(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "find_trader_search_prompt")
	assert.Equal(t, int32(3), updateCount.Load())
}

func TestSeedSystemPromptsPresentationUpdateFailure(t *testing.T) {
	// Fourth Update (find_trader_presentation_prompt) fails.
	updateCount := atomic.Int32{}
	prompts := &failingUpdatePrompts{
		MockPrompts: &testingutil.MockPrompts{SP: &core.SystemPrompt{}},
		count:       &updateCount,
		failOn:      4, // fail on fourth Update call
	}
	svc := NewSeedService(prompts)

	err := svc.SeedSystemPrompts(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "find_trader_presentation_prompt")
	assert.Equal(t, int32(4), updateCount.Load())
}

func TestSeedSystemPromptsAllFilledSkipsUpdates(t *testing.T) {
	// All fields filled → no Update calls.
	updateCount := atomic.Int32{}
	prompts := &countingUpdatePrompts{
		MockPrompts: &testingutil.MockPrompts{SP: &core.SystemPrompt{
			WorkerProfilePrompt:          "existing worker",
			ClientProfilePrompt:          "existing client",
			FindTraderSearchPrompt:       "existing search",
			FindTraderPresentationPrompt: "existing presentation",
		}},
		count: &updateCount,
	}
	svc := NewSeedService(prompts)

	err := svc.SeedSystemPrompts(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(0), updateCount.Load(), "no Update calls when all fields filled")
}

// ── local helpers ────────────────────────────────────────────────────

// countingUpdatePrompts counts how many times Update is called.
type countingUpdatePrompts struct {
	*testingutil.MockPrompts
	count *atomic.Int32
}

func (c *countingUpdatePrompts) Update(_ context.Context, _, _ string) (*core.SystemPrompt, error) {
	c.count.Add(1)
	return c.SP, nil
}

// failingUpdatePrompts fails on the Nth Update call.
type failingUpdatePrompts struct {
	*testingutil.MockPrompts
	count  *atomic.Int32
	failOn int32
}

func (f *failingUpdatePrompts) Update(_ context.Context, column, _ string) (*core.SystemPrompt, error) {
	n := f.count.Add(1)
	if n == f.failOn {
		return nil, errors.New("update failed for " + column)
	}
	return f.SP, nil
}
