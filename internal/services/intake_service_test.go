package services

import (
	"context"
	"fmt"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
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

	result, err := svc.ProcessIntake(t.Context(), "user-1", IntakeModeWorker, "I'm a plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "plumber", result.DetectedFields["profession"])
	assert.Equal(t, "Madrid", result.DetectedFields["city"])
	assert.Equal(t, "conv-1", result.ConversationID)
}

func TestProcessIntakeClientFields(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "[FIELDS]{\"full_name\":\"Alvaro\",\"city\":\"Madrid\"}[/FIELDS]"}
	chatRepo := &testingutil.MockChatRepo{ReturnID: "conv-2"}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, chatRepo, &testingutil.MockPrompts{})

	result, err := svc.ProcessIntake(t.Context(), "user-1", IntakeModeClient, "I'm Alvaro", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "Alvaro", result.DetectedFields["full_name"])
	assert.Equal(t, "Madrid", result.DetectedFields["city"])
}

func TestProcessIntakeNoFieldsConversational(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "Hello! I'm here to help."}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	result, err := svc.ProcessIntake(t.Context(), "user-1", IntakeModeWorker, "Hi!", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	assert.Nil(t, result.DetectedFields)
}

func TestProcessIntakeUnknownModeWithFields(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "[FIELDS]{\"profession\":\"plumber\"}[/FIELDS]"}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	_, err := svc.ProcessIntake(t.Context(), "user-1", "bad_mode", "test", nil, "", "", "", nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown intake mode")
}

func TestProcessIntakeLLMError(t *testing.T) {
	llm := &testingutil.MockLLM{AskErr: fmt.Errorf("LLM down")}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	_, err := svc.ProcessIntake(t.Context(), "user-1", IntakeModeWorker, "test", nil, "", "", "", nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM down")
}

func TestProcessIntakePromptLoadError(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "test"}
	prompts := &testingutil.MockPrompts{GetErr: fmt.Errorf("DB down")}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, prompts)

	_, err := svc.ProcessIntake(t.Context(), "user-1", IntakeModeWorker, "test", nil, "", "", "", nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DB down")
}

func TestProcessIntakeWithLanguage(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "[FIELDS]{\"profession\":\"plumber\"}[/FIELDS]"}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	result, err := svc.ProcessIntake(t.Context(), "user-1", IntakeModeWorker, "soy fontanero", nil, "", "es", "", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "plumber", result.DetectedFields["profession"])
}

func TestProcessIntakeNoUserID(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "[FIELDS]{\"profession\":\"plumber\"}[/FIELDS]"}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	result, err := svc.ProcessIntake(t.Context(), "", IntakeModeWorker, "plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "", result.ConversationID)
}

// ── ProcessIntake additional edge cases ──────────────────────────────

func TestProcessIntakeSaveErrorDoesNotFail(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "[FIELDS]{\"profession\":\"plumber\"}[/FIELDS]"}
	chatRepo := &testingutil.MockChatRepo{SaveErr: fmt.Errorf("disk full")}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, chatRepo, &testingutil.MockPrompts{})

	result, err := svc.ProcessIntake(t.Context(), "user-1", IntakeModeWorker, "plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "plumber", result.DetectedFields["profession"])
	assert.Equal(t, "", result.ConversationID)
}

func TestProcessIntakeWorkerTriggersReembed(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "[FIELDS]{\"profession\":\"plumber\"}[/FIELDS]"}
	chatRepo := &testingutil.MockChatRepo{ReturnID: "conv-1"}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, chatRepo, &testingutil.MockPrompts{})

	_, err := svc.ProcessIntake(t.Context(), "user-1", IntakeModeWorker, "plumber", nil, "", "", "", nil, nil)
	require.NoError(t, err)

	svc.reembedMu.Lock()
	_, ok := svc.reembedTimers["user-1"]
	svc.reembedMu.Unlock()
	assert.True(t, ok, "expected reembed timer for user-1")

	svc.reembedMu.Lock()
	if t, ok := svc.reembedTimers["user-1"]; ok {
		t.Stop()
	}
	svc.reembedMu.Unlock()
}

func TestProcessIntakeClientDoesNotTriggerReembed(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "[FIELDS]{\"full_name\":\"Alvaro\"}[/FIELDS]"}
	chatRepo := &testingutil.MockChatRepo{ReturnID: "conv-2"}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, chatRepo, &testingutil.MockPrompts{})

	_, err := svc.ProcessIntake(t.Context(), "user-1", IntakeModeClient, "I'm Alvaro", nil, "", "", "", nil, nil)
	require.NoError(t, err)

	svc.reembedMu.Lock()
	_, ok := svc.reembedTimers["user-1"]
	svc.reembedMu.Unlock()
	assert.False(t, ok, "should not reembed for client intake")
}

func TestProcessIntakeConversationalDoesNotTriggerReembed(t *testing.T) {
	llm := &testingutil.MockLLM{Answer: "Hello!"}
	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	_, err := svc.ProcessIntake(t.Context(), "user-1", IntakeModeWorker, "hi", nil, "", "", "", nil, nil)
	require.NoError(t, err)

	svc.reembedMu.Lock()
	_, ok := svc.reembedTimers["user-1"]
	svc.reembedMu.Unlock()
	assert.False(t, ok, "should not reembed for conversational response")
}

// ── scheduleReembed debounce ────────────────────────────────────────

func TestScheduleReembedDebounce(t *testing.T) {
	svc := NewIntakeService(&testingutil.MockLLM{}, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	svc.scheduleReembed("user-1")
	svc.scheduleReembed("user-1")

	svc.reembedMu.Lock()
	timer, ok := svc.reembedTimers["user-1"]
	svc.reembedMu.Unlock()

	assert.True(t, ok)
	assert.NotNil(t, timer)
	timer.Stop()
}

func TestScheduleReembedDifferentUsers(t *testing.T) {
	svc := NewIntakeService(&testingutil.MockLLM{}, &testingutil.MockProfiles{}, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	svc.scheduleReembed("user-1")
	svc.scheduleReembed("user-2")

	svc.reembedMu.Lock()
	_, ok1 := svc.reembedTimers["user-1"]
	_, ok2 := svc.reembedTimers["user-2"]
	svc.reembedMu.Unlock()

	assert.True(t, ok1)
	assert.True(t, ok2)

	svc.reembedMu.Lock()
	svc.reembedTimers["user-1"].Stop()
	svc.reembedTimers["user-2"].Stop()
	svc.reembedMu.Unlock()
}

// ── normalizeProfession ─────────────────────────────────────────────

func TestNormalizeProfessionSpanish(t *testing.T) {
	tests := []struct{ input, want string }{
		{"electricista", "electrician"},
		{"Electricista", "electrician"},
		{"FONTANERO", "plumber"},
		{"plomero", "plumber"},
		{"limpiador", "cleaner"},
		{"limpieza", "cleaner"},
		{"manitas", "handyman"},
		{"carpintero", "carpintero"},
		{"pintor", "painter"},
		{"jardinero", "landscaper"},
		{"tejador", "roofer"},
		{"aire acondicionado", "hvac technician"},
		{"unknown profession", "unknown profession"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeProfession(tc.input))
		})
	}
}

// ── searchFiltersFromJSON additional ────────────────────────────────

func TestSearchFiltersFromJSONPartialFields(t *testing.T) {
	f := searchFiltersFromJSON([]byte(`{"profession":"plumber"}`))
	assert.Equal(t, "plumber", f.Profession)
	assert.Equal(t, "", f.City)
	assert.False(t, f.EmergencyOnly)
}

// ── buildWorkerSummaries ────────────────────────────────────────────

func TestBuildWorkerSummariesWithWorkers(t *testing.T) {
	workers := []core.WorkerProfile{
		{UserID: "w1", Profession: "plumber", City: "Madrid", BusinessName: "PlumbCo", Phone: "123"},
	}
	summary := buildWorkerSummaries(workers, "find plumber")
	assert.Contains(t, summary, "matching workers")
	assert.Contains(t, summary, "PlumbCo")
	assert.Contains(t, summary, "123")
}

func TestBuildWorkerSummariesWithoutPhone(t *testing.T) {
	workers := []core.WorkerProfile{
		{UserID: "w1", Profession: "plumber", City: "Madrid", BusinessName: "PlumbCo"},
	}
	summary := buildWorkerSummaries(workers, "find plumber")
	assert.Contains(t, summary, "PlumbCo")
	assert.NotContains(t, summary, "phone:")
}

func TestBuildWorkerSummariesWithBio(t *testing.T) {
	workers := []core.WorkerProfile{
		{UserID: "w1", Profession: "plumber", City: "Madrid", BusinessName: "PlumbCo", Bio: "10 years exp"},
	}
	summary := buildWorkerSummaries(workers, "find plumber")
	assert.Contains(t, summary, "10 years exp")
}

// ── ReembedWorker via intake path ──────────────────────────────────

func TestReembedWorkerViaIntake(t *testing.T) {
	profs := &testingutil.MockProfiles{
		WorkerProfile: &core.WorkerProfile{
			UserID:     "user-1",
			Profession: "plumber",
			City:       "Madrid",
			Bio:        "10 years",
		},
		EmbeddingMeta: map[string]ports.EmbeddingMeta{},
	}
	llm := &testingutil.MockLLM{
		EmbedFn: func(_ context.Context, _ string) ([]float32, error) {
			return make([]float32, 768), nil
		},
	}
	svc := NewIntakeService(llm, profs, &testingutil.MockChatRepo{}, &testingutil.MockPrompts{})

	svc.ReembedWorker("user-1")
}
