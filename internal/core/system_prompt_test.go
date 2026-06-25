package core

import "testing"

func TestEffectiveWorkerPromptFromDB(t *testing.T) {
	sp := SystemPrompt{WorkerProfilePrompt: "Custom worker prompt"}
	if got := sp.EffectiveWorkerPrompt(); got != "Custom worker prompt" {
		t.Fatalf("expected custom prompt, got %q", got)
	}
}

func TestEffectiveWorkerPromptEmpty(t *testing.T) {
	sp := SystemPrompt{WorkerProfilePrompt: ""}
	if got := sp.EffectiveWorkerPrompt(); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestEffectiveClientPromptFromDB(t *testing.T) {
	sp := SystemPrompt{ClientProfilePrompt: "Custom client prompt"}
	if got := sp.EffectiveClientPrompt(); got != "Custom client prompt" {
		t.Fatalf("expected custom prompt, got %q", got)
	}
}

func TestEffectiveClientPromptEmpty(t *testing.T) {
	sp := SystemPrompt{ClientProfilePrompt: ""}
	if got := sp.EffectiveClientPrompt(); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestEffectiveFindTraderPresentationFromDB(t *testing.T) {
	sp := SystemPrompt{FindTraderPresentationPrompt: "Custom presentation prompt"}
	if got := sp.EffectiveFindTraderPresentation(); got != "Custom presentation prompt" {
		t.Fatalf("expected custom prompt, got %q", got)
	}
}

func TestEffectiveFindTraderPresentationDefault(t *testing.T) {
	sp := SystemPrompt{FindTraderPresentationPrompt: ""}
	expected := "You are a helpful assistant. Present search results conversationally."
	if got := sp.EffectiveFindTraderPresentation(); got != expected {
		t.Fatalf("expected default prompt, got %q", got)
	}
}

func TestSystemPromptTableName(t *testing.T) {
	sp := SystemPrompt{}
	if got := sp.TableName(); got != "system_prompts" {
		t.Fatalf("expected system_prompts, got %q", got)
	}
}
