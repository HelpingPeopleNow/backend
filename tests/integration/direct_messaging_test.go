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
	"gorm.io/gorm"
)

// ── Direct messaging integration tests ──────────────────────────────────
// Contact creation, inbox listing, message thread, send message, mark read,
// archive, block, report.

// setupDMWorker creates a worker profile and returns its ID.
func setupDMWorker(t *testing.T, db *gorm.DB) string {
	t.Helper()
	profileRepo := repository.NewGormProfileRepository(db)
	require.NoError(t, profileRepo.UpsertWorkerProfile(t.Context(), "worker-dm1", map[string]interface{}{
		"profession": "Plumber",
		"city":       "Madrid",
	}))
	wp, err := profileRepo.GetWorkerProfile(t.Context(), "worker-dm1")
	require.NoError(t, err)
	require.NotNil(t, wp)
	return wp.ID
}

func TestDirectMessagingContactCreation(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	// Client creates contact with worker
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth("client-dm1")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["conversation_id"])
	assert.Contains(t, resp, "worker")
	assert.Equal(t, true, resp["created"])
}

func TestDirectMessagingContactIdempotent(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	// First contact creation
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w1 := httptest.NewRecorder()
	fakeAuth("client-dm-idem")(mux).ServeHTTP(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code)
	var resp1 map[string]interface{}
	require.NoError(t, json.Unmarshal(w1.Body.Bytes(), &resp1))

	// Second contact — same IDs, should return same conversation
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w2 := httptest.NewRecorder()
	fakeAuth("client-dm-idem")(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)
	var resp2 map[string]interface{}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp2))

	assert.Equal(t, resp1["id"], resp2["id"])
}

func TestDirectMessagingSendMessage(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	// Create contact
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth("client-dm-send")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var convResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &convResp))
	convID := convResp["id"].(string)

	// Send message
	body, _ := json.Marshal(map[string]interface{}{
		"conversation_id": convID,
		"body":            "Hi, I need a plumber urgently!",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/direct-messages", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	fakeAuth("client-dm-send")(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)

	var msgResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &msgResp))
	assert.NotEmpty(t, msgResp["id"])
	assert.Equal(t, "Hi, I need a plumber urgently!", msgResp["body"])
}

func TestDirectMessagingInbox(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	// Create contact and send a message
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth("client-dm-inbox")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var convResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &convResp))
	convID := convResp["id"].(string)

	body, _ := json.Marshal(map[string]interface{}{
		"conversation_id": convID,
		"body":            "Hello from client",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/direct-messages", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	fakeAuth("client-dm-inbox")(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)

	// Client lists inbox
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/direct-messages", nil)
	w3 := httptest.NewRecorder()
	fakeAuth("client-dm-inbox")(mux).ServeHTTP(w3, req3)
	require.Equal(t, http.StatusOK, w3.Code)

	var inbox []map[string]interface{}
	require.NoError(t, json.Unmarshal(w3.Body.Bytes(), &inbox))
	require.GreaterOrEqual(t, len(inbox), 1)
}

func TestDirectMessagingThread(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	// Create contact
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth("client-dm-thread")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var convResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &convResp))
	convID := convResp["id"].(string)

	// Send a message
	body, _ := json.Marshal(map[string]interface{}{
		"conversation_id": convID,
		"body":            "Test message",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/direct-messages", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	fakeAuth("client-dm-thread")(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)

	// Get message thread
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/direct-messages/"+convID, nil)
	w3 := httptest.NewRecorder()
	fakeAuth("client-dm-thread")(mux).ServeHTTP(w3, req3)
	require.Equal(t, http.StatusOK, w3.Code)

	var msgs []map[string]interface{}
	require.NoError(t, json.Unmarshal(w3.Body.Bytes(), &msgs))
	require.GreaterOrEqual(t, len(msgs), 1)
}

func TestDirectMessagingMarkRead(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	// Create contact + send message as client
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth("client-dm-read")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var convResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &convResp))
	convID := convResp["id"].(string)

	body, _ := json.Marshal(map[string]interface{}{
		"conversation_id": convID,
		"body":            "Unread message",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/direct-messages", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	fakeAuth("client-dm-read")(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)

	// Worker marks as read (worker reads client messages)
	req3 := httptest.NewRequest(http.MethodPatch, "/api/v1/direct-messages/"+convID+"/read", nil)
	w3 := httptest.NewRecorder()
	fakeAuth("worker-dm1")(mux).ServeHTTP(w3, req3)
	require.Equal(t, http.StatusOK, w3.Code)
}

func TestDirectMessagingArchive(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	// Create contact
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth("client-dm-archive")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var convResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &convResp))
	convID := convResp["id"].(string)

	// Client archives conversation
	req2 := httptest.NewRequest(http.MethodPatch, "/api/v1/direct-messages/"+convID+"/archive", nil)
	w2 := httptest.NewRecorder()
	fakeAuth("client-dm-archive")(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)
}

func TestDirectMessagingBlock(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	// Create contact
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth("client-dm-block")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var convResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &convResp))
	convID := convResp["id"].(string)

	// Block conversation
	req2 := httptest.NewRequest(http.MethodPatch, "/api/v1/direct-messages/"+convID+"/block", nil)
	w2 := httptest.NewRecorder()
	fakeAuth("client-dm-block")(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)

	// Verify blocked
	dmRepo := repository.NewGormDirectMessageRepository(db)
	conv, err := dmRepo.GetConversation(t.Context(), convID)
	require.NoError(t, err)
	assert.Equal(t, "blocked", conv.Status)
}

func TestDirectMessagingReport(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	// Create contact + send message
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth("client-dm-report")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var convResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &convResp))
	convID := convResp["id"].(string)

	body, _ := json.Marshal(map[string]interface{}{
		"conversation_id": convID,
		"body":            "Reportable message",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/direct-messages", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	fakeAuth("client-dm-report")(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)
	var msgResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &msgResp))
	msgID := msgResp["id"].(string)

	// Report the message
	reportBody, _ := json.Marshal(map[string]interface{}{
		"reason": "spam",
	})
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/direct-messages/"+msgID+"/report", bytes.NewReader(reportBody))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	fakeAuth("client-dm-report")(mux).ServeHTTP(w3, req3)
	// Report endpoint logs a warning; it returns 200 on success
	assert.Equal(t, http.StatusOK, w3.Code)
}

func TestDirectMessagingConversationStatus(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	workerProfileID := setupDMWorker(t, db)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	// Create contact
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers/"+workerProfileID+"/contact", nil)
	w := httptest.NewRecorder()
	fakeAuth("client-dm-status")(mux).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var convResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &convResp))
	convID := convResp["id"].(string)
	assert.Equal(t, "active", convResp["status"])

	// Worker can also list their inbox
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/direct-messages", nil)
	w2 := httptest.NewRecorder()
	fakeAuth("worker-dm1")(mux).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)
	_ = convID
}
