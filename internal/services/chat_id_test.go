package services

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
)

// ── Mock implementations ──────────────────────────────────────────

type mockLLM struct {
	answer string
}

func (m *mockLLM) Ask(ctx context.Context, systemPrompt, userMessage string, history []ports.MessagePair, provider string) (*ports.LLMResponse, error) {
	return &ports.LLMResponse{Answer: m.answer, Role: "worker"}, nil
}
func (m *mockLLM) Health(ctx context.Context) error { return nil }

type mockChatRepo struct {
	savedConversationID string
	returnID            string
}

func (m *mockChatRepo) SaveMessages(ctx context.Context, userID, convType, userMessage, assistantResponse, conversationID string, fields json.RawMessage, metadata map[string]interface{}) (string, error) {
	m.savedConversationID = conversationID
	return m.returnID, nil
}
func (m *mockChatRepo) LoadConversation(ctx context.Context, userID, convType string) (*core.Conversation, error) {
	return nil, nil
}
func (m *mockChatRepo) ListConversations(ctx context.Context, userID, convType string, limit, offset int) ([]core.Conversation, int64, error) {
	return nil, 0, nil
}
func (m *mockChatRepo) GetConversation(ctx context.Context, userID, conversationID string) (*core.Conversation, error) {
	return nil, nil
}
func (m *mockChatRepo) GetMessages(ctx context.Context, conversationID string) ([]core.Message, error) {
	return nil, nil
}

type mockProfiles struct{}

func (m *mockProfiles) GetWorkerProfile(ctx context.Context, userID string) (*core.WorkerProfile, error) {
	return nil, nil
}
func (m *mockProfiles) UpsertWorkerProfile(ctx context.Context, userID string, fields map[string]interface{}) error {
	return nil
}
func (m *mockProfiles) DeleteWorkerProfile(ctx context.Context, userID string) error { return nil }
func (m *mockProfiles) GetClientProfile(ctx context.Context, userID string) (*core.ClientProfile, error) {
	return nil, nil
}
func (m *mockProfiles) UpsertClientProfile(ctx context.Context, userID string, fields map[string]interface{}) error {
	return nil
}
func (m *mockProfiles) DeleteClientProfile(ctx context.Context, userID string) error { return nil }
func (m *mockProfiles) FindWorkers(ctx context.Context, filters core.WorkerSearchFilters) ([]core.WorkerProfile, error) {
	return []core.WorkerProfile{}, nil
}

type mockPrompts struct {
	sp *core.SystemPrompt
}

func (m *mockPrompts) Get(ctx context.Context) (*core.SystemPrompt, error) {
	if m.sp != nil {
		return m.sp, nil
	}
	return &core.SystemPrompt{
		WorkerProfilePrompt:          core.DefaultWorkerProfilePrompt,
		ClientProfilePrompt:          core.DefaultClientProfilePrompt,
		FindTraderSearchPrompt:       core.DefaultFindTraderSearchPrompt,
		FindTraderPresentationPrompt: core.DefaultFindTraderPresentationPrompt,
	}, nil
}
func (m *mockPrompts) Update(ctx context.Context, column, value string) (*core.SystemPrompt, error) {
	return nil, nil
}

// ── Tests ─────────────────────────────────────────────────────────

func TestProcessIntakePassesConversationID(t *testing.T) {
	expectedID := "conv-abc-123"
	chatRepo := &mockChatRepo{returnID: expectedID}
	llm := &mockLLM{answer: `Hello! What trade do you work in?
[FIELDS]{"profession":"plumber"}[/FIELDS]`}
	prompts := &mockPrompts{}

	svc := NewIntakeService(llm, &mockProfiles{}, chatRepo, prompts)

	result, err := svc.ProcessIntake(context.Background(), "user-1", IntakeModeWorker, "I'm a plumber", nil, "", "", expectedID)
	if err != nil {
		t.Fatalf("ProcessIntake failed: %v", err)
	}

	if chatRepo.savedConversationID != expectedID {
		t.Fatalf("expected conversationID %q passed to SaveMessages, got %q", expectedID, chatRepo.savedConversationID)
	}

	if result.ConversationID != expectedID {
		t.Fatalf("expected result.ConversationID %q, got %q", expectedID, result.ConversationID)
	}

	t.Logf("✓ ProcessIntake passed conversationID %q → SaveMessages received %q → result returns %q",
		expectedID, chatRepo.savedConversationID, result.ConversationID)
}

func TestProcessIntakeEmptyConversationIDCreatesNew(t *testing.T) {
	chatRepo := &mockChatRepo{returnID: "new-conv-456"}
	llm := &mockLLM{answer: `Hello! What trade do you work in?
[FIELDS]{"profession":"electrician"}[/FIELDS]`}
	prompts := &mockPrompts{}

	svc := NewIntakeService(llm, &mockProfiles{}, chatRepo, prompts)

	result, err := svc.ProcessIntake(context.Background(), "user-2", IntakeModeWorker, "I'm an electrician", nil, "", "", "")
	if err != nil {
		t.Fatalf("ProcessIntake failed: %v", err)
	}

	if chatRepo.savedConversationID != "" {
		t.Fatalf("expected empty conversationID passed to SaveMessages, got %q", chatRepo.savedConversationID)
	}

	if result.ConversationID != "new-conv-456" {
		t.Fatalf("expected result.ConversationID %q, got %q", "new-conv-456", result.ConversationID)
	}

	t.Logf("✓ Empty conversationID → SaveMessages received \"\" → new conversation %q created", result.ConversationID)
}

func TestSearchPassesConversationID(t *testing.T) {
	expectedID := "search-conv-789"
	chatRepo := &mockChatRepo{returnID: expectedID}
	llm := &mockLLM{answer: `I'll search for plumbers in your area.
[SEARCH]{"profession":"plumber","city":"Madrid"}[/SEARCH]`}
	prompts := &mockPrompts{}

	svc := NewSearchService(llm, &mockProfiles{}, chatRepo, prompts)

	result, err := svc.Search(context.Background(), "user-3", "need a plumber", nil, "", "", expectedID)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if chatRepo.savedConversationID != expectedID {
		t.Fatalf("expected conversationID %q passed to SaveMessages, got %q", expectedID, chatRepo.savedConversationID)
	}

	if result.ConversationID != expectedID {
		t.Fatalf("expected result.ConversationID %q, got %q", expectedID, result.ConversationID)
	}

	t.Logf("✓ Search passed conversationID %q → SaveMessages received %q → result returns %q",
		expectedID, chatRepo.savedConversationID, result.ConversationID)
}

func TestSearchConversationalPathPassesConversationID(t *testing.T) {
	expectedID := "search-chat-001"
	chatRepo := &mockChatRepo{returnID: expectedID}
	llm := &mockLLM{answer: `Hello! What kind of professional are you looking for?`}
	prompts := &mockPrompts{}

	svc := NewSearchService(llm, &mockProfiles{}, chatRepo, prompts)

	result, err := svc.Search(context.Background(), "user-4", "hi, I need help", nil, "", "", expectedID)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if chatRepo.savedConversationID != expectedID {
		t.Fatalf("expected conversationID %q passed to SaveMessages, got %q", expectedID, chatRepo.savedConversationID)
	}

	if result.ConversationID != expectedID {
		t.Fatalf("expected result.ConversationID %q, got %q", expectedID, result.ConversationID)
	}

	t.Logf("✓ Search (conversational path) passed conversationID %q → SaveMessages received %q → result returns %q",
		expectedID, chatRepo.savedConversationID, result.ConversationID)
}
