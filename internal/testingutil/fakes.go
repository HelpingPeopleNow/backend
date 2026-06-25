package testingutil

import (
	"context"
	"encoding/json"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
	"gorm.io/gorm"
)

// ── LLMService fake (Strategy A) ───────────────────────────────────
// MockLLM is a configurable fake for ports.LLMService.
// Fields: Answer (canned), AskErr (injected), HealthErr (injected),
// EmbedFn (function-pointer override, Strategy C hybrid).
type MockLLM struct {
	Answer    string
	AskErr    error
	HealthErr error
	EmbedFn   func(ctx context.Context, text string) ([]float32, error)
}

func (m *MockLLM) Ask(_ context.Context, _, _ string, _ []ports.MessagePair, _ string) (*ports.LLMResponse, error) {
	if m.AskErr != nil {
		return nil, m.AskErr
	}
	return &ports.LLMResponse{Answer: m.Answer, Role: "assistant"}, nil
}

func (m *MockLLM) Health(_ context.Context) error { return m.HealthErr }

func (m *MockLLM) Embed(_ context.Context, text string) ([]float32, error) {
	if m.EmbedFn != nil {
		return m.EmbedFn(nil, text)
	}
	return make([]float32, 768), nil
}

// ── ChatRepository fake (Strategy A) ───────────────────────────────
// MockChatRepo tracks SaveMessages calls.
type MockChatRepo struct {
	SavedUserID         string
	SavedConvType       string
	SavedUserMessage    string
	SavedAssistantReply string
	SavedConversationID string
	SavedFields         json.RawMessage
	ReturnID            string
}

func (m *MockChatRepo) SaveMessages(_ context.Context, userID, convType, userMessage, assistantResponse, conversationID string, fields json.RawMessage, _ map[string]interface{}) (string, error) {
	m.SavedUserID = userID
	m.SavedConvType = convType
	m.SavedUserMessage = userMessage
	m.SavedAssistantReply = assistantResponse
	m.SavedConversationID = conversationID
	m.SavedFields = fields
	return m.ReturnID, nil
}

func (m *MockChatRepo) LoadConversation(_ context.Context, _, _ string) (*core.Conversation, error) {
	return nil, nil
}

func (m *MockChatRepo) ListConversations(_ context.Context, _, _ string, _, _ int) ([]core.Conversation, int64, error) {
	return nil, 0, nil
}

func (m *MockChatRepo) GetConversation(_ context.Context, _, _ string) (*core.Conversation, error) {
	return nil, nil
}

func (m *MockChatRepo) GetMessages(_ context.Context, _ string) ([]core.Message, error) {
	return nil, nil
}

// ── ProfileRepository fake (Strategy A) ────────────────────────────
// MockProfiles tracks profile upsert calls and returns canned profiles.
type MockProfiles struct {
	UpsertedWorkerID  string
	UpsertedWorkerMap map[string]interface{}
	UpsertedClientID  string
	UpsertedClientMap map[string]interface{}
	WorkerProfile     *core.WorkerProfile
	ClientProfile     *core.ClientProfile
	Workers           []core.WorkerProfile
	WorkersErr        error
}

func (m *MockProfiles) GetWorkerProfile(_ context.Context, _ string) (*core.WorkerProfile, error) {
	return m.WorkerProfile, nil
}

func (m *MockProfiles) UpsertWorkerProfile(_ context.Context, userID string, fields map[string]interface{}) error {
	m.UpsertedWorkerID = userID
	m.UpsertedWorkerMap = fields
	return nil
}

func (m *MockProfiles) DeleteWorkerProfile(_ context.Context, _ string) error { return nil }

func (m *MockProfiles) GetClientProfile(_ context.Context, _ string) (*core.ClientProfile, error) {
	return m.ClientProfile, nil
}

func (m *MockProfiles) UpsertClientProfile(_ context.Context, userID string, fields map[string]interface{}) error {
	m.UpsertedClientID = userID
	m.UpsertedClientMap = fields
	return nil
}

func (m *MockProfiles) DeleteClientProfile(_ context.Context, _ string) error { return nil }

func (m *MockProfiles) FindWorkers(_ context.Context, filters core.WorkerSearchFilters) (ports.FindResult, error) {
	return ports.FindResult{Workers: m.Workers, Branch: "ilike"}, m.WorkersErr
}

func (m *MockProfiles) UpsertWorkerEmbedding(_ context.Context, _, _ string, _ []float32, _ string) error {
	return nil
}

func (m *MockProfiles) GetWorkerEmbeddingHashes(_ context.Context, _ string) (map[string]ports.EmbeddingMeta, error) {
	return nil, nil
}

func (m *MockProfiles) DeleteWorkerEmbedding(_ context.Context, _, _ string) error { return nil }

func (m *MockProfiles) FindStaleWorkerIDs(_ context.Context) ([]string, error) {
	return nil, nil
}

func (m *MockProfiles) RawQuery(_ context.Context, _ string, _ ...interface{}) *gorm.DB {
	return nil
}

// ── SystemPromptRepository fake (Strategy A+C) ─────────────────────
// MockPrompts returns a configurable SystemPrompt. GetErr lets tests
// simulate DB failures. SP overrides the default prompt set.
type MockPrompts struct {
	SP     *core.SystemPrompt
	GetErr error
}

func (m *MockPrompts) Get(_ context.Context) (*core.SystemPrompt, error) {
	if m.GetErr != nil {
		return nil, m.GetErr
	}
	if m.SP != nil {
		return m.SP, nil
	}
	return &core.SystemPrompt{
		WorkerProfilePrompt:          core.DefaultWorkerProfilePrompt,
		ClientProfilePrompt:          core.DefaultClientProfilePrompt,
		FindTraderSearchPrompt:       core.DefaultFindTraderSearchPrompt,
		FindTraderPresentationPrompt: core.DefaultFindTraderPresentationPrompt,
	}, nil
}

func (m *MockPrompts) Update(_ context.Context, _, _ string) (*core.SystemPrompt, error) {
	return m.SP, nil
}
