package services

import (
	"fmt"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/testingutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── applyLanguage ───────────────────────────────────────────────────

func TestApplyLanguageSpanish(t *testing.T) {
	got := applyLanguage("prompt", "es")
	assert.Contains(t, got, "IMPORTANTE: Responde SIEMPRE en español")
}

func TestApplyLanguageEnglish(t *testing.T) {
	got := applyLanguage("prompt", "en")
	assert.Contains(t, got, "IMPORTANT: Always respond in English")
}

func TestApplyLanguageDefault(t *testing.T) {
	got := applyLanguage("prompt", "fr")
	assert.Equal(t, "prompt", got)
}

func TestApplyLanguageEmpty(t *testing.T) {
	got := applyLanguage("prompt", "")
	assert.Equal(t, "prompt", got)
}

// ── selectPrompt ────────────────────────────────────────────────────

func TestSelectPromptWorkerWithCustom(t *testing.T) {
	svc := &IntakeService{}
	sp := &core.SystemPrompt{WorkerProfilePrompt: "Custom worker"}
	assert.Equal(t, "Custom worker", svc.selectPrompt(sp, IntakeModeWorker))
}

func TestSelectPromptWorkerFallback(t *testing.T) {
	svc := &IntakeService{}
	sp := &core.SystemPrompt{}
	assert.Equal(t, core.DefaultWorkerProfilePrompt, svc.selectPrompt(sp, IntakeModeWorker))
}

func TestSelectPromptClientWithCustom(t *testing.T) {
	svc := &IntakeService{}
	sp := &core.SystemPrompt{ClientProfilePrompt: "Custom client"}
	assert.Equal(t, "Custom client", svc.selectPrompt(sp, IntakeModeClient))
}

func TestSelectPromptClientFallback(t *testing.T) {
	svc := &IntakeService{}
	sp := &core.SystemPrompt{}
	assert.Equal(t, core.DefaultClientProfilePrompt, svc.selectPrompt(sp, IntakeModeClient))
}

func TestSelectPromptUnknownMode(t *testing.T) {
	svc := &IntakeService{}
	assert.Equal(t, "", svc.selectPrompt(&core.SystemPrompt{}, "unknown"))
}

// ── ProcessIntake ───────────────────────────────────────────────────

func TestProcessIntakeWorkerFields(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "[FIELDS]{\"profession\":\"plumber\",\"city\":\"Madrid\"}[/FIELDS]"}
	chatRepo := &testingutil.MockChatRepo{ReturnID: "conv-1"}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, chatRepo, &testingutil.MockPrompts{})

	result, err := svc.ProcessIntake(t.Context(), "user-1", IntakeModeWorker, "I'm a plumber", nil, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, "plumber", result.DetectedFields["profession"])
	assert.Equal(t, "Madrid", result.DetectedFields["city"])
	assert.Equal(t, "conv-1", result.ConversationID)
}

func TestProcessIntakeClientFields(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "[FIELDS]{\"full_name\":\"Alvaro\",\"city\":\"Madrid\"}[/FIELDS]"}
	chatRepo := &testingutil.MockChatRepo{ReturnID: "conv-2"}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, chatRepo, &testingutil.MockPrompts{})

	result, err := svc.ProcessIntake(t.Context(), "user-1", IntakeModeClient, "I'm Alvaro", nil, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, "Alvaro", result.DetectedFields["full_name"])
	assert.Equal(t, "Madrid", result.DetectedFields["city"])
}

func TestProcessIntakeNoFieldsConversational(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "Hello! I'm here to help."}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	result, err := svc.ProcessIntake(t.Context(), "user-1", IntakeModeWorker, "Hi!", nil, "", "", "")
	require.NoError(t, err)
	assert.Nil(t, result.DetectedFields)
}

func TestProcessIntakeUnknownModeWithFields(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "[FIELDS]{\"profession\":\"plumber\"}[/FIELDS]"}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	_, err := svc.ProcessIntake(t.Context(), "user-1", "bad_mode", "test", nil, "", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown intake mode")
}

func TestProcessIntakeLLMError(t *testing.T) {
	llm := &testingutil.MockLLM{AskErr: fmt.Errorf("LLM down")}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	_, err := svc.ProcessIntake(t.Context(), "user-1", IntakeModeWorker, "test", nil, "", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM down")
}

func TestProcessIntakePromptLoadError(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "test"}
	prompts := &testingutil.MockPrompts{GetErr: fmt.Errorf("DB down")}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, prompts)

	_, err := svc.ProcessIntake(t.Context(), "user-1", IntakeModeWorker, "test", nil, "", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DB down")
}

func TestProcessIntakeWithLanguage(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "[FIELDS]{\"profession\":\"plumber\"}[/FIELDS]"}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	result, err := svc.ProcessIntake(t.Context(), "user-1", IntakeModeWorker, "soy fontanero", nil, "", "es", "")
	require.NoError(t, err)
	assert.Equal(t, "plumber", result.DetectedFields["profession"])
}

func TestProcessIntakeNoUserID(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "[FIELDS]{\"profession\":\"plumber\"}[/FIELDS]"}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	result, err := svc.ProcessIntake(t.Context(), "", IntakeModeWorker, "plumber", nil, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, "", result.ConversationID)
}
