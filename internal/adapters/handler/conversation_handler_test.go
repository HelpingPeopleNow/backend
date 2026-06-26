package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/testingutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newConversationHandler() *ConversationHandler {
	return NewConversationHandler(&testingutil.MockChatRepo{
		Convs: []core.Conversation{{ID: "conv-1", UserID: "user-1"}, {ID: "conv-2", UserID: "user-1"}},
	})
}

func TestConversationHandlerList(t *testing.T) {
	h := newConversationHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Contains(t, resp, "conversations")
	assert.Contains(t, resp, "total")
}

func TestConversationHandlerGetOtherUser(t *testing.T) {
	repo := &testingutil.MockChatRepo{GetErr: assert.AnError}
	h := NewConversationHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/conv-other", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ── getOne paths ──────────────────────────────────────────────────────

func TestGetOneNotFound(t *testing.T) {
	repo := &testingutil.MockChatRepo{Conv: nil, GetErr: nil}
	h := NewConversationHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/conv-missing", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "not found", resp["error"])
}

func TestGetOneMessagesError(t *testing.T) {
	repo := &testingutil.MockChatRepo{
		Conv:   &core.Conversation{ID: "conv-1", UserID: "user-1"},
		MsgErr: assert.AnError,
	}
	h := NewConversationHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/conv-1", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetOneSuccess(t *testing.T) {
	repo := &testingutil.MockChatRepo{
		Conv: &core.Conversation{ID: "conv-1", UserID: "user-1", Type: "worker"},
		Msgs: []core.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi!"},
		},
	}
	h := NewConversationHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/conv-1", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp conversationDetail
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "conv-1", resp.ID)
	assert.Equal(t, "worker", resp.Type)
	assert.Len(t, resp.Messages, 2)
	assert.Equal(t, "user", resp.Messages[0].Role)
	assert.Equal(t, "Hello", resp.Messages[0].Content)
}

// ── list paths ────────────────────────────────────────────────────────

func TestListUnauthorized(t *testing.T) {
	h := newConversationHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
	// No userID in context

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestListDBError(t *testing.T) {
	repo := &testingutil.MockChatRepo{ListErr: assert.AnError}
	h := NewConversationHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestListWithOffsetAndLimit(t *testing.T) {
	h := newConversationHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations?limit=5&offset=10", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestListNegativeOffset(t *testing.T) {
	h := newConversationHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations?offset=-5", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestListWithTypeFilter(t *testing.T) {
	h := newConversationHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations?type=worker", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// ── method not allowed ────────────────────────────────────────────────

func TestGetOneMethodNotAllowed(t *testing.T) {
	h := newConversationHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/conv-1", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestListMethodNotAllowed(t *testing.T) {
	h := newConversationHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
