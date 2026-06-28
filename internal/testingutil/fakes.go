package testingutil

import (
	"context"
	"encoding/json"
	"time"

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
	Conv                *core.Conversation
	Convs               []core.Conversation
	Msgs                []core.Message
	GetErr              error
	MsgErr              error
	ListErr             error
	SaveErr             error
}

func (m *MockChatRepo) SaveMessages(_ context.Context, userID, convType, userMessage, assistantResponse, conversationID string, fields json.RawMessage, _ map[string]interface{}) (string, error) {
	m.SavedUserID = userID
	m.SavedConvType = convType
	m.SavedUserMessage = userMessage
	m.SavedAssistantReply = assistantResponse
	m.SavedConversationID = conversationID
	m.SavedFields = fields
	if m.SaveErr != nil {
		return "", m.SaveErr
	}
	return m.ReturnID, nil
}

func (m *MockChatRepo) LoadConversation(_ context.Context, _, _ string) (*core.Conversation, error) {
	return m.Conv, m.GetErr
}

func (m *MockChatRepo) ListConversations(_ context.Context, _, _ string, _, _ int) ([]core.Conversation, int64, error) {
	return m.Convs, int64(len(m.Convs)), m.ListErr
}

func (m *MockChatRepo) GetConversation(_ context.Context, _, _ string) (*core.Conversation, error) {
	return m.Conv, m.GetErr
}

func (m *MockChatRepo) GetMessages(_ context.Context, _ string) ([]core.Message, error) {
	return m.Msgs, m.MsgErr
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
	WorkerEmbedding   []float32
	EmbeddingMeta     map[string]ports.EmbeddingMeta
	StaleWorkerIDs    []string
	WorkerByProfileID *core.WorkerProfile
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

func (m *MockProfiles) UpsertWorkerEmbedding(_ context.Context, _, _ string, vec []float32, _ string) error {
	m.WorkerEmbedding = vec
	return nil
}

func (m *MockProfiles) GetWorkerEmbeddingHashes(_ context.Context, _ string) (map[string]ports.EmbeddingMeta, error) {
	return m.EmbeddingMeta, nil
}

func (m *MockProfiles) DeleteWorkerEmbedding(_ context.Context, _, _ string) error { return nil }

func (m *MockProfiles) FindStaleWorkerIDs(_ context.Context) ([]string, error) {
	return m.StaleWorkerIDs, nil
}

func (m *MockProfiles) RawQuery(_ context.Context, _ string, _ ...interface{}) *gorm.DB {
	return nil
}

func (m *MockProfiles) FindBySlug(_ context.Context, slug string) (*core.WorkerProfile, error) {
	if m.WorkerProfile != nil && m.WorkerProfile.Slug == slug {
		return m.WorkerProfile, nil
	}
	return nil, nil
}

func (m *MockProfiles) FindLatestWithSlug(_ context.Context, limit int) ([]core.WorkerProfile, error) {
	if m.WorkerProfile != nil && m.WorkerProfile.Slug != "" {
		return []core.WorkerProfile{*m.WorkerProfile}, nil
	}
	return nil, nil
}

func (m *MockProfiles) GetWorkerByProfileID(_ context.Context, _ string) (*core.WorkerProfile, error) {
	return m.WorkerByProfileID, nil
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

// ── Broker fake (Strategy A) ────────────────────────────────────────
// MockBroker records Publish calls and delivers to subscribers.
type MockBroker struct {
	PublishedUserID string
	PublishedEvent  ports.Event
	Subscribers     map[string][]chan ports.Event
}

func NewMockBroker() *MockBroker {
	return &MockBroker{Subscribers: make(map[string][]chan ports.Event)}
}

func (m *MockBroker) Subscribe(_ context.Context, userID string) (<-chan ports.Event, error) {
	ch := make(chan ports.Event, 32)
	m.Subscribers[userID] = append(m.Subscribers[userID], ch)
	return ch, nil
}

func (m *MockBroker) Publish(userID string, event ports.Event) error {
	m.PublishedUserID = userID
	m.PublishedEvent = event
	for _, ch := range m.Subscribers[userID] {
		select {
		case ch <- event:
		default:
		}
	}
	return nil
}

// ── DirectMessageRepository fake (Strategy A) ───────────────────────
// MockDMRepo tracks DM calls and returns canned data.
type MockDMRepo struct {
	Conv              *core.DirectConversation
	Convs             []core.DirectConversation
	Msgs              []core.DirectMessage
	Err               error
	Created           bool
	Marked            int
	IsParticipantBool bool
	WorkerByProfileID *core.WorkerProfile
}

func (m *MockDMRepo) GetOrCreateConversation(_ context.Context, _, _ string) (*core.DirectConversation, bool, error) {
	return m.Conv, m.Created, m.Err
}

func (m *MockDMRepo) GetConversation(_ context.Context, _ string) (*core.DirectConversation, error) {
	return m.Conv, m.Err
}

func (m *MockDMRepo) ListConversations(_ context.Context, _, _, _ string, _ int, _ *time.Time) ([]core.DirectConversation, error) {
	return m.Convs, m.Err
}

func (m *MockDMRepo) ArchiveConversation(_ context.Context, _, _, _ string) error {
	return m.Err
}

func (m *MockDMRepo) BlockConversation(_ context.Context, _ string) error {
	return m.Err
}

func (m *MockDMRepo) GetMessages(_ context.Context, _ string, _ int, _ string) ([]core.DirectMessage, error) {
	return m.Msgs, m.Err
}

func (m *MockDMRepo) SendMessage(_ context.Context, _ *core.DirectMessage) error {
	return m.Err
}

func (m *MockDMRepo) MarkRead(_ context.Context, _, _ string) (int, error) {
	return m.Marked, m.Err
}

func (m *MockDMRepo) PollSince(_ context.Context, _ string, _ time.Time) ([]core.DirectMessage, error) {
	return m.Msgs, m.Err
}

func (m *MockDMRepo) GetWorkerByProfileID(_ context.Context, _ string) (*core.WorkerProfile, error) {
	if m.WorkerByProfileID != nil {
		return m.WorkerByProfileID, m.Err
	}
	return nil, m.Err
}

func (m *MockDMRepo) IsParticipant(_ context.Context, _, _ string) (bool, string, error) {
	return m.IsParticipantBool, "", m.Err
}
