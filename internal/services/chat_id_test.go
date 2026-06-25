package services

import (
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/testingutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── ProcessIntake conversationID passthrough ────────────────────────

func TestProcessIntakePassesConversationID(t *testing.T) {
	expectedID := "conv-abc-123"
	chatRepo := &testingutil.MockChatRepo{ReturnID: expectedID}
	llm := &testingutil.MockLLM{Answer: "Hello!\n[FIELDS]{\"profession\":\"plumber\"}[/FIELDS]"}
	prompts := &testingutil.MockPrompts{}

	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, chatRepo, prompts)
	result, err := svc.ProcessIntake(t.Context(), "user-1", IntakeModeWorker, "I'm a plumber", nil, "", "", expectedID)

	require.NoError(t, err)
	assert.Equal(t, expectedID, chatRepo.SavedConversationID)
	assert.Equal(t, expectedID, result.ConversationID)
}

func TestProcessIntakeEmptyConversationIDCreatesNew(t *testing.T) {
	chatRepo := &testingutil.MockChatRepo{ReturnID: "new-conv-456"}
	llm := &testingutil.MockLLM{Answer: "Hello!\n[FIELDS]{\"profession\":\"electrician\"}[/FIELDS]"}
	prompts := &testingutil.MockPrompts{}

	svc := NewIntakeService(llm, &testingutil.MockProfiles{}, chatRepo, prompts)
	result, err := svc.ProcessIntake(t.Context(), "user-2", IntakeModeWorker, "I'm an electrician", nil, "", "", "")

	require.NoError(t, err)
	assert.Equal(t, "", chatRepo.SavedConversationID)
	assert.Equal(t, "new-conv-456", result.ConversationID)
}

// ── Search conversationID passthrough ───────────────────────────────

func TestSearchPassesConversationID(t *testing.T) {
	expectedID := "search-conv-789"
	chatRepo := &testingutil.MockChatRepo{ReturnID: expectedID}
	llm := &testingutil.MockLLM{Answer: "[SEARCH]{\"profession\":\"plumber\",\"city\":\"Madrid\"}[/SEARCH]"}
	prompts := &testingutil.MockPrompts{}

	svc := NewSearchService(llm, &testingutil.MockProfiles{}, chatRepo, prompts)
	result, err := svc.Search(t.Context(), "user-3", "need a plumber", nil, "", "", expectedID)

	require.NoError(t, err)
	assert.Equal(t, expectedID, chatRepo.SavedConversationID)
	assert.Equal(t, expectedID, result.ConversationID)
}

func TestSearchConversationalPathPassesConversationID(t *testing.T) {
	expectedID := "search-chat-001"
	chatRepo := &testingutil.MockChatRepo{ReturnID: expectedID}
	llm := &testingutil.MockLLM{Answer: "Hello! What kind of professional are you looking for?"}
	prompts := &testingutil.MockPrompts{}

	svc := NewSearchService(llm, &testingutil.MockProfiles{}, chatRepo, prompts)
	result, err := svc.Search(t.Context(), "user-4", "hi, I need help", nil, "", "", expectedID)

	require.NoError(t, err)
	assert.Equal(t, expectedID, chatRepo.SavedConversationID)
	assert.Equal(t, expectedID, result.ConversationID)
}
