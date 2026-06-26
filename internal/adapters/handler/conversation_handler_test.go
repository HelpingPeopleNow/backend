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