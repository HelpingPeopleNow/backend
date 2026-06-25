package services

import (
	"context"
	"fmt"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

// ── applyLanguage ───────────────────────────────────────────────────

func TestApplyLanguageSpanish(t *testing.T) {
	result := applyLanguage("You are a helpful assistant.", "es")
	expected := "You are a helpful assistant.\n\nIMPORTANTE: Responde SIEMPRE en español al usuario. Todas tus respuestas deben ser en español."
	if result != expected {
		t.Fatalf("unexpected result:\n got: %q\nwant: %q", result, expected)
	}
}

func TestApplyLanguageEnglish(t *testing.T) {
	result := applyLanguage("You are a helpful assistant.", "en")
	expected := "You are a helpful assistant.\n\nIMPORTANT: Always respond in English to the user. All your responses must be in English."
	if result != expected {
		t.Fatalf("unexpected result:\n got: %q\nwant: %q", result, expected)
	}
}

func TestApplyLanguageDefault(t *testing.T) {
	result := applyLanguage("You are a helpful assistant.", "fr")
	expected := "You are a helpful assistant."
	if result != expected {
		t.Fatalf("expected unchanged prompt for unknown lang, got %q", result)
	}
}

func TestApplyLanguageEmpty(t *testing.T) {
	result := applyLanguage("You are a helpful assistant.", "")
	expected := "You are a helpful assistant."
	if result != expected {
		t.Fatalf("expected unchanged prompt for empty lang, got %q", result)
	}
}

// ── selectPrompt ────────────────────────────────────────────────────

func TestSelectPromptWorkerWithCustom(t *testing.T) {
	svc := &IntakeService{}
	sp := &core.SystemPrompt{WorkerProfilePrompt: "Custom worker prompt"}
	got := svc.selectPrompt(sp, IntakeModeWorker)
	if got != "Custom worker prompt" {
		t.Fatalf("expected custom prompt, got %q", got)
	}
}

func TestSelectPromptWorkerEmpty(t *testing.T) {
	svc := &IntakeService{}
	sp := &core.SystemPrompt{WorkerProfilePrompt: ""}
	got := svc.selectPrompt(sp, IntakeModeWorker)
	if got != core.DefaultWorkerProfilePrompt {
		t.Fatalf("expected default worker prompt, got %q", got)
	}
}

func TestSelectPromptClientWithCustom(t *testing.T) {
	svc := &IntakeService{}
	sp := &core.SystemPrompt{ClientProfilePrompt: "Custom client prompt"}
	got := svc.selectPrompt(sp, IntakeModeClient)
	if got != "Custom client prompt" {
		t.Fatalf("expected custom prompt, got %q", got)
	}
}

func TestSelectPromptClientEmpty(t *testing.T) {
	svc := &IntakeService{}
	sp := &core.SystemPrompt{ClientProfilePrompt: ""}
	got := svc.selectPrompt(sp, IntakeModeClient)
	if got != core.DefaultClientProfilePrompt {
		t.Fatalf("expected default client prompt, got %q", got)
	}
}

func TestSelectPromptUnknownMode(t *testing.T) {
	svc := &IntakeService{}
	sp := &core.SystemPrompt{}
	got := svc.selectPrompt(sp, "unknown")
	if got != "" {
		t.Fatalf("expected empty for unknown mode, got %q", got)
	}
}

// ── ProcessIntake ───────────────────────────────────────────────────

func TestProcessIntakeWorkerFields(t *testing.T) {
	llm := &mockLLM{
		answer: `Here's what I know about you.
[FIELDS]{"profession":"plumber","city":"Madrid"}[/FIELDS]`,
	}
	prompts := &mockPrompts{}
	profs := &mockProfiles{}
	chats := &mockChatRepo{returnID: "conv-1"}
	svc := NewIntakeService(llm, profs, chats, prompts)

	result, err := svc.ProcessIntake(context.Background(), "user-1", IntakeModeWorker, "I'm a plumber in Madrid", nil, "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DetectedFields == nil {
		t.Fatal("expected detected fields")
	}
	if result.DetectedFields["profession"] != "plumber" {
		t.Fatalf("expected profession=plumber, got %v", result.DetectedFields["profession"])
	}
	if result.ConversationID != "conv-1" {
		t.Fatalf("expected conv-1, got %q", result.ConversationID)
	}
}

func TestProcessIntakeClientFields(t *testing.T) {
	llm := &mockLLM{
		answer: `[FIELDS]{"full_name":"Alvaro","city":"Madrid"}[/FIELDS]`,
	}
	prompts := &mockPrompts{}
	profs := &mockProfiles{}
	chats := &mockChatRepo{returnID: "conv-2"}
	svc := NewIntakeService(llm, profs, chats, prompts)

	result, err := svc.ProcessIntake(context.Background(), "user-1", IntakeModeClient, "I'm Alvaro from Madrid", nil, "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DetectedFields == nil {
		t.Fatal("expected detected fields")
	}
	if result.DetectedFields["full_name"] != "Alvaro" {
		t.Fatalf("expected full_name=Alvaro, got %v", result.DetectedFields["full_name"])
	}
}

func TestProcessIntakeNoFieldsConversational(t *testing.T) {
	llm := &mockLLM{
		answer: "Hello! I'm here to help you find a plumber.",
	}
	prompts := &mockPrompts{}
	profs := &mockProfiles{}
	chats := &mockChatRepo{returnID: "conv-3"}
	svc := NewIntakeService(llm, profs, chats, prompts)

	result, err := svc.ProcessIntake(context.Background(), "user-1", IntakeModeWorker, "Hi!", nil, "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DetectedFields != nil {
		t.Fatalf("expected nil fields for conversational response, got %v", result.DetectedFields)
	}
}

func TestProcessIntakeUnknownMode(t *testing.T) {
	llm := &mockLLM{answer: `[FIELDS]{"profession":"plumber"}[/FIELDS]`}
	prompts := &mockPrompts{}
	svc := NewIntakeService(llm, &mockProfiles{}, &mockChatRepo{}, prompts)

	_, err := svc.ProcessIntake(context.Background(), "user-1", "bad_mode", "test", nil, "", "", "")
	if err == nil {
		t.Fatal("expected error for unknown mode with fields")
	}
}

func TestProcessIntakeLLMError(t *testing.T) {
	llm := &mockLLM{askErr: fmt.Errorf("LLM down")}
	prompts := &mockPrompts{}
	svc := NewIntakeService(llm, &mockProfiles{}, &mockChatRepo{}, prompts)

	_, err := svc.ProcessIntake(context.Background(), "user-1", IntakeModeWorker, "test", nil, "", "", "")
	if err == nil {
		t.Fatal("expected error when LLM fails")
	}
}

func TestProcessIntakePromptLoadError(t *testing.T) {
	llm := &mockLLM{answer: "test"}
	prompts := &mockPrompts{getErr: fmt.Errorf("DB down")}
	svc := NewIntakeService(llm, &mockProfiles{}, &mockChatRepo{}, prompts)

	_, err := svc.ProcessIntake(context.Background(), "user-1", IntakeModeWorker, "test", nil, "", "", "")
	if err == nil {
		t.Fatal("expected error when prompt load fails")
	}
}

func TestProcessIntakeWithLanguage(t *testing.T) {
	llm := &mockLLM{
		answer: `[FIELDS]{"profession":"plumber"}[/FIELDS]`,
	}
	prompts := &mockPrompts{}
	svc := NewIntakeService(llm, &mockProfiles{}, &mockChatRepo{}, prompts)

	result, err := svc.ProcessIntake(context.Background(), "user-1", IntakeModeWorker, "soy fontanero", nil, "", "es", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DetectedFields["profession"] != "plumber" {
		t.Fatalf("expected profession=plumber, got %v", result.DetectedFields["profession"])
	}
}

func TestProcessIntakeWithConversationID(t *testing.T) {
	llm := &mockLLM{
		answer: `[FIELDS]{"profession":"plumber"}[/FIELDS]`,
	}
	prompts := &mockPrompts{}
	chats := &mockChatRepo{returnID: "new-conv"}
	svc := NewIntakeService(llm, &mockProfiles{}, chats, prompts)

	result, err := svc.ProcessIntake(context.Background(), "user-1", IntakeModeWorker, "plumber", nil, "", "", "existing-conv")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ConversationID != "new-conv" {
		t.Fatalf("expected new-conv, got %q", result.ConversationID)
	}
}

func TestProcessIntakeNoUserID(t *testing.T) {
	llm := &mockLLM{
		answer: `[FIELDS]{"profession":"plumber"}[/FIELDS]`,
	}
	prompts := &mockPrompts{}
	svc := NewIntakeService(llm, &mockProfiles{}, &mockChatRepo{}, prompts)

	result, err := svc.ProcessIntake(context.Background(), "", IntakeModeWorker, "plumber", nil, "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ConversationID != "" {
		t.Fatalf("expected empty conversation ID for no user, got %q", result.ConversationID)
	}
}
