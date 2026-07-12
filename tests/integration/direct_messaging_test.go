//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/adapters/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ── Direct messaging integration tests ──────────────────────────────────
// Contact creation, inbox listing, message thread, send message, mark read,
// archive, block, report.
//
// Audit reminder: this file runs under `//go:build integration` so it is
// skipped by default `go test ./...`. CI must pass `-tags=integration` to
// actually exercise SendMessage end-to-end against Postgres — that is the
// only path that catches a regression of the direct_messages.sender_role
// NOT NULL bug.

const (
	dmClientUserID = "client-dm-send"
	dmWorkerUserID = "worker-dm1"
)

// dmMux returns the integration mux with both a worker AND a client
// upserted so sender_role resolution can find a profile on each side
// (audit: relevance-tested on real DB path).
func dmMux(t *testing.T, db *gorm.DB) http.Handler {
	t.Helper()
	profileRepo := repository.NewGormProfileRepository(db)
	require.NoError(t, profileRepo.UpsertWorkerProfile(context.Background(), dmWorkerUserID, map[string]interface{}{
		"profession": "Plumber",
		"city":       "Madrid",
	}))
	require.NoError(t, profileRepo.UpsertClientProfile(context.Background(), dmClientUserID, map[string]interface{}{
		"full_name": "Test Client",
		"city":      "Madrid",
	}))
	return buildIntegrationMux(t, db, newFakeLLM("irrelevant"))
}

// setupDMWorker creates a worker profile and returns its ID. Kept for
// tests that exercise worker-only paths.
func setupDMWorker(t *testing.T, db *gorm.DB) string {
	t.Helper()
	profileRepo := repository.NewGormProfileRepository(db)
	require.NoError(t, profileRepo.UpsertWorkerProfile(t.Context(), dmWorkerUserID, map[string]interface{}{
		"profession": "Plumber",
		"city":       "Madrid",
	}))
	wp, err := profileRepo.GetWorkerProfile(t.Context(), dmWorkerUserID)
	require.NoError(t, err)
	require.NotNil(t, wp)
	return wp.ID
}

func TestDirectMessagingContactCreation(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	mux := dmMux(t, db)

	// Client creates contact with worker
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["conversation_id"])
	assert.Contains(t, resp, "worker")
	assert.Equal(t, true, resp["created"])

	// Audit assertion: user_a_role + user_b_role must be populated on
	// the conversation row so SendMessage can derive sender_role O(1).
	dmRepo := repository.NewGormDirectMessageRepository(db)
	convResp := resp["conversation_id"].(string)
	conv, err := dmRepo.GetConversation(t.Context(), convResp)
	require.NoError(t, err)
	require.NotNil(t, conv)
	assert.Equal(t, "client", conv.UserARole, "client should resolve to 'client'")
	assert.Equal(t, "worker", conv.UserBRole, "worker should resolve to 'worker'")
}

func TestDirectMessagingContactIdempotent(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	mux := dmMux(t, db)

	// First contact creation
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w1 := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code)
	var resp1 map[string]interface{}
	require.NoError(t, json.Unmarshal(w1.Body.Bytes(), &resp1))

	// Second contact — same IDs, should return same conversation
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w2 := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)
	var resp2 map[string]interface{}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp2))

	assert.Equal(t, resp1["conversation_id"], resp2["conversation_id"])
}

// TestDirectMessagingSendMessage is the regression test for the original
// production bug (audit): the prior test exercised the wrong route and
// was silently hitting a 404. This is now the authoritative end-to-end
// SendMessage integration test — it MUST catch a future direct_messages
// .sender_role NOT NULL violation.
func TestDirectMessagingSendMessage(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	mux := dmMux(t, db)

	// Create contact
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var convResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &convResp))
	convID := convResp["conversation_id"].(string)

	// Send message — the correct route this time.
	body, _ := json.Marshal(map[string]interface{}{"body": "Hi, I need a plumber urgently!"})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/direct-messages/"+convID+"/messages", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusCreated, w2.Code, "real SendMessage must return 201")

	var msgResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &msgResp))
	assert.NotEmpty(t, msgResp["id"])
	assert.Equal(t, "Hi, I need a plumber urgently!", msgResp["body"])
	// Audit assertion: response must include sender_role so the
	// frontend can render role-aware UI without an extra roundtrip.
	assert.Equal(t, "client", msgResp["sender_role"], "client user sending → sender_role=client")

	// Persisted row assertion: direct_messages.sender_role must equal
	// 'client' (the regression bug was a NOT NULL violation here).
	dmRepo := repository.NewGormDirectMessageRepository(db)
	msgs, err := dmRepo.GetMessages(t.Context(), convID, 10, "")
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "client", msgs[0].SenderRole)
}

// TestDirectMessagingSendMessageFromWorker covers the WORKER side — the
// prior test only ever exercised the client side. Together they cover
// both ends of the conversation, including sender_role = 'worker'.
func TestDirectMessagingSendMessageFromWorker(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	mux := dmMux(t, db)

	// Client initiates
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var convResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &convResp))
	convID := convResp["conversation_id"].(string)

	// Worker replies
	body, _ := json.Marshal(map[string]interface{}{"body": "I can help with that."})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/direct-messages/"+convID+"/messages", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	fakeAuth(dmWorkerUserID)(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusCreated, w2.Code)

	var msgResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &msgResp))
	assert.Equal(t, "worker", msgResp["sender_role"])
}

func TestDirectMessagingInbox(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	mux := dmMux(t, db)

	// Client opens a thread and posts one message so the inbox shows
	// a last_message preview.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var convResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &convResp))
	convID := convResp["conversation_id"].(string)

	body, _ := json.Marshal(map[string]interface{}{"body": "Hello from client"})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/direct-messages/"+convID+"/messages", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusCreated, w2.Code)

	// List inbox
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/direct-messages", nil)
	w3 := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w3, req3)
	require.Equal(t, http.StatusOK, w3.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w3.Body.Bytes(), &resp))
	convs, ok := resp["conversations"].([]interface{})
	require.True(t, ok)
	require.GreaterOrEqual(t, len(convs), 1)
}

func TestDirectMessagingThread(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	mux := dmMux(t, db)

	// Open thread + send one message
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var convResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &convResp))
	convID := convResp["conversation_id"].(string)

	body, _ := json.Marshal(map[string]interface{}{"body": "Test message"})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/direct-messages/"+convID+"/messages", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusCreated, w2.Code)

	// GET messages (correct route).
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/direct-messages/"+convID+"/messages", nil)
	w3 := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w3, req3)
	require.Equal(t, http.StatusOK, w3.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w3.Body.Bytes(), &resp))
	msgs, ok := resp["messages"].([]interface{})
	require.True(t, ok)
	require.GreaterOrEqual(t, len(msgs), 1)
}

func TestDirectMessagingMarkRead(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	mux := dmMux(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var convResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &convResp))
	convID := convResp["conversation_id"].(string)

	body, _ := json.Marshal(map[string]interface{}{"body": "Unread message"})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/direct-messages/"+convID+"/messages", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusCreated, w2.Code)

	// Worker marks as read (correct route, correct method).
	req3 := httptest.NewRequest(http.MethodPatch, "/api/v1/direct-messages/"+convID+"/read", nil)
	w3 := httptest.NewRecorder()
	fakeAuth(dmWorkerUserID)(mux).ServeHTTP(w3, req3)
	require.Equal(t, http.StatusNoContent, w3.Code)
}

func TestDirectMessagingArchive(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	mux := dmMux(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var convResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &convResp))
	convID := convResp["conversation_id"].(string)

	// Archive via correct POST method (audit: prior test used PATCH).
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/direct-messages/"+convID+"/archive", nil)
	w2 := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusNoContent, w2.Code)
}

func TestDirectMessagingBlock(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	mux := dmMux(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var convResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &convResp))
	convID := convResp["conversation_id"].(string)

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/direct-messages/"+convID+"/block", nil)
	w2 := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusNoContent, w2.Code)

	dmRepo := repository.NewGormDirectMessageRepository(db)
	conv, err := dmRepo.GetConversation(t.Context(), convID)
	require.NoError(t, err)
	assert.Equal(t, "blocked", conv.Status)
}

func TestDirectMessagingReport(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	mux := dmMux(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var convResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &convResp))
	convID := convResp["conversation_id"].(string)

	body, _ := json.Marshal(map[string]interface{}{"body": "Reportable message"})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/direct-messages/"+convID+"/messages", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusCreated, w2.Code)

	// Report on the CONVERSATION id (not a message id — the handler
	// dispatches by /report suffix; the conversation id is sufficient).
	reportBody, _ := json.Marshal(map[string]interface{}{"reason": "spam"})
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/direct-messages/"+convID+"/report", bytes.NewReader(reportBody))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w3, req3)
	require.Equal(t, http.StatusNoContent, w3.Code)
}

func TestDirectMessagingConversationStatus(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	mux := dmMux(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth(dmClientUserID)(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var convResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &convResp))
	convID := convResp["conversation_id"].(string)
	assert.Equal(t, "active", convResp["status"])

	// Both sides see the conversation in their inbox.
	for _, uid := range []string{dmClientUserID, dmWorkerUserID} {
		req2 := httptest.NewRequest(http.MethodGet, "/api/v1/direct-messages", nil)
		w2 := httptest.NewRecorder()
		fakeAuth(uid)(mux).ServeHTTP(w2, req2)
		require.Equal(t, http.StatusOK, w2.Code)
	}
	_ = convID
}
