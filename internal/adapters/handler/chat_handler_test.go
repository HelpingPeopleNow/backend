package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/services"
	"github.com/HelpingPeopleNow/backend/internal/testingutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper to make an authenticated request
func authReq(method, path, body string) *http.Request {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Content-Type", "application/json")
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	return req.WithContext(ctx)
}

func chatSetup() *ChatHandler {
	mockLLM := &testingutil.MockLLM{Answer: `[FIELDS] profession=Plumber city=Madrid [/FIELDS] Profile saved!`}
	profiles := &testingutil.MockProfiles{
		Workers: []core.WorkerProfile{{UserID: "w1", Profession: "Plumber", City: "Madrid"}},
	}
	chats := &testingutil.MockChatRepo{ReturnID: "conv-1"}
	prompts := &testingutil.MockPrompts{}
	intakeSvc := services.NewIntakeService(mockLLM, profiles, chats, prompts)
	searchSvc := services.NewSearchService(mockLLM, profiles, chats, prompts)
	return NewChatHandler(intakeSvc, searchSvc, prompts)
}

func TestChatHandlerRejectsGet(t *testing.T) {
	h := chatSetup()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/chat", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestChatHandlerInvalidJSON(t *testing.T) {
	h := chatSetup()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/chat", "not json"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestChatHandlerEmptyMessage(t *testing.T) {
	h := chatSetup()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/chat", `{"mode":"worker_intake","message":""}`))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestChatHandlerInvalidMode(t *testing.T) {
	h := chatSetup()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/chat", `{"mode":"bogus","message":"hi"}`))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestChatHandlerWorkerIntake(t *testing.T) {
	h := chatSetup()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/chat", `{"mode":"worker_intake","message":"I'm a plumber"}`))
	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Contains(t, resp, "answer")
	assert.Contains(t, resp, "conversation_id")
}

func TestChatHandlerClientIntake(t *testing.T) {
	mockLLM := &testingutil.MockLLM{Answer: `[FIELDS] full_name=Jane city=Barcelona [/FIELDS] Done!`}
	profiles := &testingutil.MockProfiles{}
	chats := &testingutil.MockChatRepo{ReturnID: "conv-2"}
	prompts := &testingutil.MockPrompts{}
	intakeSvc := services.NewIntakeService(mockLLM, profiles, chats, prompts)
	searchSvc := services.NewSearchService(mockLLM, profiles, chats, prompts)
	h := NewChatHandler(intakeSvc, searchSvc, prompts)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/chat", `{"mode":"client_intake","message":"I'm Jane"}`))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestChatHandlerSearch(t *testing.T) {
	h := chatSetup()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/chat", `{"mode":"search","message":"plumber in Madrid"}`))
	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Contains(t, resp, "answer")
}

func TestChatHandlerDefaultMode(t *testing.T) {
	h := chatSetup()
	rec := httptest.NewRecorder()
	// Empty mode defaults to worker_intake
	h.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/chat", `{"message":"I'm a plumber"}`))
	// Should succeed (defaults to worker_intake)
	assert.Equal(t, http.StatusOK, rec.Code)
}
