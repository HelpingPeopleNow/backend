//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/adapters/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Conversations integration tests ─────────────────────────────────────
// List conversations, get conversation by ID, auto-save on chat.

func TestListConversationsEmpty(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	llm := newFakeLLM("hello")
	mux := buildIntegrationMux(t, db, llm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
	w := httptest.NewRecorder()
	fakeAuth("user-conv-list")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	convs, ok := resp["conversations"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, convs)
	assert.Equal(t, float64(0), resp["total"])
}

func TestListConversationsWithData(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	// Create conversations directly via the repo
	chatRepo := repository.NewGormChatRepository(db)
	_, err := chatRepo.SaveMessages(t.Context(), "user-conv-list2", "worker", "msg1", "reply1", "", nil, nil)
	require.NoError(t, err)
	_, err = chatRepo.SaveMessages(t.Context(), "user-conv-list2", "search", "msg2", "reply2", "", nil, nil)
	require.NoError(t, err)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
	w := httptest.NewRecorder()
	fakeAuth("user-conv-list2")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	convs := resp["conversations"].([]interface{})
	assert.Len(t, convs, 2)
	assert.Equal(t, float64(2), resp["total"])
}

func TestListConversationsFilterByType(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	chatRepo := repository.NewGormChatRepository(db)
	_, err := chatRepo.SaveMessages(t.Context(), "user-conv-type", "worker", "msg1", "reply1", "", nil, nil)
	require.NoError(t, err)
	_, err = chatRepo.SaveMessages(t.Context(), "user-conv-type", "search", "msg2", "reply2", "", nil, nil)
	require.NoError(t, err)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	// Filter by worker type
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations?type=worker", nil)
	w := httptest.NewRecorder()
	fakeAuth("user-conv-type")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	convs := resp["conversations"].([]interface{})
	assert.Len(t, convs, 1)
}

func TestGetConversationByID(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	chatRepo := repository.NewGormChatRepository(db)
	convID, err := chatRepo.SaveMessages(t.Context(), "user-conv-get", "worker", "hello", "hi there", "", nil, nil)
	require.NoError(t, err)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+convID, nil)
	w := httptest.NewRecorder()
	fakeAuth("user-conv-get")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, convID, resp["id"])
	assert.Equal(t, "worker", resp["type"])

	// Check messages are included
	msgs := resp["messages"].([]interface{})
	assert.Len(t, msgs, 2)
}

func TestGetConversationNotFound(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/nonexistent-uuid", nil)
	w := httptest.NewRecorder()
	fakeAuth("user-conv-nf")(mux).ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAutoSaveOnChat(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	llm := newFakeLLM(`[FIELDS]
{"profession":"Painter","city":"Valencia"}
[/FIELDS]`)
	mux := buildIntegrationMux(t, db, llm)

	// First chat — creates a conversation
	body, _ := json.Marshal(map[string]interface{}{
		"mode":    "worker_intake",
		"message": "I am a painter",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fakeAuth("user-autosave")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp1 map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp1))
	convID1 := resp1["conversation_id"].(string)
	require.NotEmpty(t, convID1)

	// Second chat with same conversation_id — appends to same conversation
	body2, _ := json.Marshal(map[string]interface{}{
		"mode":            "worker_intake",
		"message":         "I also do interior painting",
		"conversation_id": convID1,
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	fakeAuth("user-autosave")(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)

	var resp2 map[string]interface{}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp2))
	convID2 := resp2["conversation_id"].(string)
	assert.Equal(t, convID1, convID2, "second chat should reuse same conversation")

	// Verify the conversation has 4 messages (2 user + 2 assistant)
	chatRepo := repository.NewGormChatRepository(db)
	msgs, err := chatRepo.GetMessages(t.Context(), convID1)
	require.NoError(t, err)
	assert.Len(t, msgs, 4)
}
