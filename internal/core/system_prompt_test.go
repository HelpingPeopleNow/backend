package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEffectiveWorkerPromptCustom(t *testing.T) {
	sp := &SystemPrompt{WorkerProfilePrompt: "Custom worker prompt"}
	assert.Equal(t, "Custom worker prompt", sp.EffectiveWorkerPrompt())
}

func TestEffectiveWorkerPromptEmpty(t *testing.T) {
	sp := &SystemPrompt{}
	assert.Equal(t, "", sp.EffectiveWorkerPrompt())
}

func TestEffectiveClientPromptCustom(t *testing.T) {
	sp := &SystemPrompt{ClientProfilePrompt: "Custom client prompt"}
	assert.Equal(t, "Custom client prompt", sp.EffectiveClientPrompt())
}

func TestEffectiveClientPromptEmpty(t *testing.T) {
	sp := &SystemPrompt{}
	assert.Equal(t, "", sp.EffectiveClientPrompt())
}

func TestEffectiveFindTraderPresentationCustom(t *testing.T) {
	sp := &SystemPrompt{FindTraderPresentationPrompt: "Custom present"}
	assert.Equal(t, "Custom present", sp.EffectiveFindTraderPresentation())
}

func TestEffectiveFindTraderPresentationFallback(t *testing.T) {
	sp := &SystemPrompt{}
	assert.Equal(t, "You are a helpful assistant. Present search results conversationally.", sp.EffectiveFindTraderPresentation())
}

func TestSystemPromptTableName(t *testing.T) {
	sp := &SystemPrompt{}
	assert.Equal(t, "system_prompts", sp.TableName())
}
