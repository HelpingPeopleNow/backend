package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/adapters/ratelimit"
	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/testingutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDMHandler() *DirectMessagingHandler {
	return NewDirectMessagingHandler(
		&testingutil.MockDMRepo{
			Conv: &core.DirectConversation{ID: "conv-1"},
			Msgs: []core.DirectMessage{{ID: "msg-1"}},
			IsParticipantBool: true,
		},
		&testingutil.MockProfiles{WorkerProfile: &core.WorkerProfile{UserID: "w-1"}},
		testingutil.NewMockBroker(),
		ratelimit.NewRateLimiter(30, time.Minute),
	)
}

func newDMHandlerWithRepo(repo *testingutil.MockDMRepo) *DirectMessagingHandler {
	return NewDirectMessagingHandler(
		repo,
		&testingutil.MockProfiles{WorkerProfile: &core.WorkerProfile{UserID: "w-1"}},
		testingutil.NewMockBroker(),
		ratelimit.NewRateLimiter(30, time.Minute),
	)
}

func dmAuthReq(method, path string, body string) *http.Request {
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

// ── List conversations ───────────────────────────────────────────────

func TestDirectMessagingHandlerListConversations(t *testing.T) {
	h := newDMHandler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodGet, "/api/v1/direct-messages", ""))
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	assert.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Contains(t, resp, "conversations")
}

func TestDirectMessagingHandlerNoAuth(t *testing.T) {
	h := newDMHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/direct-messages", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestDirectMessagingHandlerMethodNotAllowed(t *testing.T) {
	h := newDMHandler()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/direct-messages", nil)
	ctx := contextkeys.SetUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ── Mark read ────────────────────────────────────────────────────────

func TestDirectMessagingHandlerMarkRead(t *testing.T) {
	repo := &testingutil.MockDMRepo{
		Conv: &core.DirectConversation{ID: "conv-1", ClientID: "user-1"},
		Marked: 3,
		IsParticipantBool: true,
	}
	h := newDMHandlerWithRepo(repo)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPatch, "/api/v1/direct-messages/conv-1/read", ""))
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestDirectMessagingHandlerMarkReadNotParticipant(t *testing.T) {
	repo := &testingutil.MockDMRepo{
		IsParticipantBool: false,
	}
	h := newDMHandlerWithRepo(repo)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPatch, "/api/v1/direct-messages/conv-1/read", ""))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// ── Archive ──────────────────────────────────────────────────────────

func TestDirectMessagingHandlerArchive(t *testing.T) {
	repo := &testingutil.MockDMRepo{
		Conv: &core.DirectConversation{ID: "conv-1"},
		IsParticipantBool: true,
	}
	h := newDMHandlerWithRepo(repo)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPost, "/api/v1/direct-messages/conv-1/archive", ""))
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestDirectMessagingHandlerArchiveNotParticipant(t *testing.T) {
	repo := &testingutil.MockDMRepo{IsParticipantBool: false}
	h := newDMHandlerWithRepo(repo)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPost, "/api/v1/direct-messages/conv-1/archive", ""))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// ── Block ────────────────────────────────────────────────────────────

func TestDirectMessagingHandlerBlock(t *testing.T) {
	repo := &testingutil.MockDMRepo{IsParticipantBool: true}
	h := newDMHandlerWithRepo(repo)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPost, "/api/v1/direct-messages/conv-1/block", ""))
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestDirectMessagingHandlerBlockNotParticipant(t *testing.T) {
	repo := &testingutil.MockDMRepo{IsParticipantBool: false}
	h := newDMHandlerWithRepo(repo)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPost, "/api/v1/direct-messages/conv-1/block", ""))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// ── Report ───────────────────────────────────────────────────────────

func TestDirectMessagingHandlerReport(t *testing.T) {
	repo := &testingutil.MockDMRepo{IsParticipantBool: true}
	h := newDMHandlerWithRepo(repo)
	body := `{"reason":"spam"}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPost, "/api/v1/direct-messages/conv-1/report", body))
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestDirectMessagingHandlerReportNotParticipant(t *testing.T) {
	repo := &testingutil.MockDMRepo{IsParticipantBool: false}
	h := newDMHandlerWithRepo(repo)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPost, "/api/v1/direct-messages/conv-1/report", ""))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// ── Send message ─────────────────────────────────────────────────────

func TestDirectMessagingHandlerSendMessage(t *testing.T) {
	repo := &testingutil.MockDMRepo{
		Conv: &core.DirectConversation{ID: "conv-1", ClientID: "user-1", WorkerProfileID: "wp-1"},
		IsParticipantBool: true,
	}
	h := newDMHandlerWithRepo(repo)
	body := `{"body":"Hello!"}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPost, "/api/v1/direct-messages/conv-1/messages", body))
	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestDirectMessagingHandlerSendMessageEmptyBody(t *testing.T) {
	repo := &testingutil.MockDMRepo{IsParticipantBool: true}
	h := newDMHandlerWithRepo(repo)
	body := `{"body":""}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPost, "/api/v1/direct-messages/conv-1/messages", body))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDirectMessagingHandlerSendMessageTooLong(t *testing.T) {
	repo := &testingutil.MockDMRepo{IsParticipantBool: true}
	h := newDMHandlerWithRepo(repo)
	longBody := `{"body":"` + strings.Repeat("x", 4001) + `"}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPost, "/api/v1/direct-messages/conv-1/messages", longBody))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDirectMessagingHandlerSendMessageNotParticipant(t *testing.T) {
	repo := &testingutil.MockDMRepo{IsParticipantBool: false}
	h := newDMHandlerWithRepo(repo)
	body := `{"body":"Hello!"}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPost, "/api/v1/direct-messages/conv-1/messages", body))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestDirectMessagingHandlerSendMessageBlocked(t *testing.T) {
	repo := &testingutil.MockDMRepo{
		Conv: &core.DirectConversation{
			ID:     "conv-1",
			Status: "blocked",
		},
		IsParticipantBool: true,
	}
	h := newDMHandlerWithRepo(repo)
	body := `{"body":"Hello!"}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPost, "/api/v1/direct-messages/conv-1/messages", body))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestDirectMessagingHandlerSendMessageInvalidJSON(t *testing.T) {
	repo := &testingutil.MockDMRepo{IsParticipantBool: true}
	h := newDMHandlerWithRepo(repo)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPost, "/api/v1/direct-messages/conv-1/messages", "not json"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ── Get messages ─────────────────────────────────────────────────────

func TestDirectMessagingHandlerGetMessages(t *testing.T) {
	repo := &testingutil.MockDMRepo{
		Msgs:              []core.DirectMessage{{ID: "msg-1", Body: "hi"}},
		IsParticipantBool: true,
	}
	h := newDMHandlerWithRepo(repo)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodGet, "/api/v1/direct-messages/conv-1/messages", ""))
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Contains(t, resp, "messages")
}

func TestDirectMessagingHandlerGetMessagesNotParticipant(t *testing.T) {
	repo := &testingutil.MockDMRepo{IsParticipantBool: false}
	h := newDMHandlerWithRepo(repo)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodGet, "/api/v1/direct-messages/conv-1/messages", ""))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// ── Poll since ───────────────────────────────────────────────────────

func TestDirectMessagingHandlerPollSince(t *testing.T) {
	h := newDMHandler()
	ts := time.Now().UTC().Format(time.RFC3339)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodGet, "/api/v1/direct-messages/since?ts="+ts, ""))
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Contains(t, resp, "messages")
	assert.Contains(t, resp, "server_time")
}

func TestDirectMessagingHandlerPollSinceMissingTS(t *testing.T) {
	h := newDMHandler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodGet, "/api/v1/direct-messages/since", ""))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDirectMessagingHandlerPollSinceInvalidTS(t *testing.T) {
	h := newDMHandler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodGet, "/api/v1/direct-messages/since?ts=not-a-date", ""))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ── Get or create contact ────────────────────────────────────────────

func TestDirectMessagingHandlerGetOrCreateContact(t *testing.T) {
	repo := &testingutil.MockDMRepo{
		Conv:              &core.DirectConversation{ID: "conv-new"},
		Created:           true,
		WorkerByProfileID: &core.WorkerProfile{UserID: "w-1", Profession: "plumber", BusinessName: "PlumbCo", City: "Madrid"},
	}
	profs := &testingutil.MockProfiles{}
	h := NewDirectMessagingHandler(repo, profs, testingutil.NewMockBroker(), ratelimit.NewRateLimiter(30, time.Minute))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodGet, "/api/v1/workers/wp-1/contact", ""))

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Contains(t, resp, "conversation_id")
	assert.Contains(t, resp, "worker")
	assert.Equal(t, true, resp["created"])
}

func TestDirectMessagingHandlerGetOrCreateContactSelfMessaging(t *testing.T) {
	repo := &testingutil.MockDMRepo{
		WorkerByProfileID: &core.WorkerProfile{UserID: "user-1"}, // same user!
	}
	h := NewDirectMessagingHandler(repo, &testingutil.MockProfiles{}, testingutil.NewMockBroker(), ratelimit.NewRateLimiter(30, time.Minute))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodGet, "/api/v1/workers/wp-1/contact", ""))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDirectMessagingHandlerGetOrCreateContactWorkerNotFound(t *testing.T) {
	repo := &testingutil.MockDMRepo{
		WorkerByProfileID: nil, // not found
	}
	h := NewDirectMessagingHandler(repo, &testingutil.MockProfiles{}, testingutil.NewMockBroker(), ratelimit.NewRateLimiter(30, time.Minute))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodGet, "/api/v1/workers/wp-1/contact", ""))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ── SSE stream broker nil ────────────────────────────────────────────

func TestDirectMessagingHandlerSSEBrokerNil(t *testing.T) {
	h := NewDirectMessagingHandler(
		&testingutil.MockDMRepo{},
		&testingutil.MockProfiles{},
		nil, // nil broker
		ratelimit.NewRateLimiter(30, time.Minute),
	)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodGet, "/api/v1/direct-messages/stream", ""))
	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}

// ── Conversation item helper ─────────────────────────────────────────

func TestConversationItemClientRole(t *testing.T) {
	repo := &testingutil.MockDMRepo{
		WorkerByProfileID: &core.WorkerProfile{UserID: "w-1", BusinessName: "PlumbCo", Profession: "plumber"},
	}
	h := NewDirectMessagingHandler(repo, &testingutil.MockProfiles{}, testingutil.NewMockBroker(), ratelimit.NewRateLimiter(30, time.Minute))

	now := time.Now()
	conv := core.DirectConversation{
		ID:                  "conv-1",
		ClientID:            "user-1",
		WorkerProfileID:     "wp-1",
		ClientUnreadCount:   3,
		Status:              "active",
		LastMessageAt:       &now,
		LastMessagePreview:  "Hello!",
	}

	item := h.conversationItem(nil, conv, "user-1", core.SenderRoleClient)
	assert.Equal(t, "conv-1", item["id"])
	assert.Equal(t, 3, item["unread_count"])
	assert.Equal(t, "active", item["status"])

	other := item["other_party"].(map[string]interface{})
	assert.Equal(t, "w-1", other["id"])
	assert.Equal(t, "PlumbCo", other["name"])
	assert.Equal(t, "worker", other["role"])

	lastMsg := item["last_message"].(map[string]interface{})
	assert.Equal(t, "Hello!", lastMsg["preview"])
}

func TestConversationItemWorkerRole(t *testing.T) {
	h := newDMHandler()

	now := time.Now()
	conv := core.DirectConversation{
		ID:                  "conv-1",
		ClientID:            "c-1",
		WorkerProfileID:     "wp-1",
		WorkerUnreadCount:   2,
		Status:              "active",
		LastMessageAt:       &now,
		LastMessagePreview:  "Thanks!",
	}

	item := h.conversationItem(nil, conv, "w-1", core.SenderRoleWorker)
	assert.Equal(t, 2, item["unread_count"])

	other := item["other_party"].(map[string]interface{})
	assert.Equal(t, "client", other["role"])
}

func TestConversationItemNoLastMessage(t *testing.T) {
	h := newDMHandler()
	conv := core.DirectConversation{
		ID:     "conv-1",
		Status: "active",
	}
	item := h.conversationItem(nil, conv, "user-1", core.SenderRoleClient)
	_, hasLastMessage := item["last_message"]
	assert.False(t, hasLastMessage)
}

// ── Extract segment ──────────────────────────────────────────────────

func TestExtractSegment(t *testing.T) {
	got := extractSegment("/api/v1/direct-messages/abc-123/messages", "/api/v1/direct-messages/", "/messages")
	assert.Equal(t, "abc-123", got)
}
