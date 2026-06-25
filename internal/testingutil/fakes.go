package testingutil

import (
	"context"
	"encoding/json"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
	"gorm.io/gorm"
)

// MockLLM is a configurable fake for ports.LLMService.
type MockLLM struct {
	Answer  string
	AskErr  error
	EmbedFn func(ctx context.Context, text string) ([]float32, error)
}

func (m *MockLLM) Ask(ctx context.Context, systemPrompt, userMessage string, history []ports.MessagePair, provider string) (*ports.LLMResponse, error) {
	if m.Answer != "" || m.AskErr != nil {
		return &ports.LLMResponse{Answer: m.Answer, Role: "assistant"}, m.AskErr
	}
	return &ports.LLMResponse{Answer: "", Role: "assistant"}, nil
}

func (m *MockLLM) Health(ctx context.Context) error { return nil }

func (m *MockLLM) Embed(ctx context.Context, text string) ([]float32, error) {
	if m.EmbedFn != nil {
		return m.EmbedFn(ctx, text)
	}
	return make([]float32, 768), nil
}

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

func (m *MockChatRepo) SaveMessages(ctx context.Context, userID, convType, userMessage, assistantResponse, conversationID string, fields json.RawMessage, metadata map[string]interface{}) (string, error) {
	m.SavedUserID = userID
	m.SavedConvType = convType
	m.SavedUserMessage = userMessage
	m.SavedAssistantReply = assistantResponse
	m.SavedConversationID = conversationID
	m.SavedFields = fields
	return m.ReturnID, nil
}

func (m *MockChatRepo) LoadConversation(ctx context.Context, userID, convType string) (*core.Conversation, error) {
	return nil, nil
}

func (m *MockChatRepo) ListConversations(ctx context.Context, userID, convType string, limit, offset int) ([]core.Conversation, int64, error) {
	return nil, 0, nil
}

func (m *MockChatRepo) GetConversation(ctx context.Context, userID, conversationID string) (*core.Conversation, error) {
	return nil, nil
}

func (m *MockChatRepo) GetMessages(ctx context.Context, conversationID string) ([]core.Message, error) {
	return nil, nil
}

// MockProfiles tracks profile upsert calls.
type MockProfiles struct {
	UpsertedWorkerID   string
	UpsertedWorkerMap  map[string]interface{}
	UpsertedClientID   string
	UpsertedClientMap  map[string]interface{}
	WorkerProfile      *core.WorkerProfile
	ClientProfile      *core.ClientProfile
	Workers            []core.WorkerProfile
	WorkersErr         error
}

func (m *MockProfiles) GetWorkerProfile(ctx context.Context, userID string) (*core.WorkerProfile, error) {
	return m.WorkerProfile, nil
}

func (m *MockProfiles) UpsertWorkerProfile(ctx context.Context, userID string, fields map[string]interface{}) error {
	m.UpsertedWorkerID = userID
	m.UpsertedWorkerMap = fields
	return nil
}

func (m *MockProfiles) DeleteWorkerProfile(ctx context.Context, userID string) error { return nil }

func (m *MockProfiles) GetClientProfile(ctx context.Context, userID string) (*core.ClientProfile, error) {
	return m.ClientProfile, nil
}

func (m *MockProfiles) UpsertClientProfile(ctx context.Context, userID string, fields map[string]interface{}) error {
	m.UpsertedClientID = userID
	m.UpsertedClientMap = fields
	return nil
}

func (m *MockProfiles) DeleteClientProfile(ctx context.Context, userID string) error { return nil }

func (m *MockProfiles) FindWorkers(ctx context.Context, filters core.WorkerSearchFilters) (ports.FindResult, error) {
	return ports.FindResult{Workers: m.Workers, Branch: "ilike"}, m.WorkersErr
}

func (m *MockProfiles) UpsertWorkerEmbedding(ctx context.Context, userID, fieldName string, embedding []float32, textHash string) error {
	return nil
}

func (m *MockProfiles) GetWorkerEmbeddingHashes(ctx context.Context, userID string) (map[string]ports.EmbeddingMeta, error) {
	return nil, nil
}

func (m *MockProfiles) DeleteWorkerEmbedding(ctx context.Context, userID, fieldName string) error {
	return nil
}

func (m *MockProfiles) FindStaleWorkerIDs(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (m *MockProfiles) RawQuery(ctx context.Context, sql string, values ...interface{}) *gorm.DB {
	return nil
}

// MockPrompts returns a configurable SystemPrompt.
type MockPrompts struct {
	SP *core.SystemPrompt
}

func (m *MockPrompts) Get(ctx context.Context) (*core.SystemPrompt, error) {
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

func (m *MockPrompts) Update(ctx context.Context, column, value string) (*core.SystemPrompt, error) {
	if m.SP == nil {
		m.SP = &core.SystemPrompt{}
	}
	return m.SP, nil
}
