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
	return NewChatHandler(intakeSvc, searchSvc, prompts, nil)
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
	h := NewChatHandler(intakeSvc, searchSvc, prompts, nil)

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

// --- P1-1 body cap + intake rate limit regression tests ---

// chatSetupWithLimiter returns a ChatHandler wired to a fresh in-memory
// rate limiter so each test gets a clean per-user token budget. The
// default chatSetup() passes nil, keeping the existing tests free of
// rate-limit side-effects.
func chatSetupWithLimiter(t *testing.T) (*ChatHandler, *ratelimit.RateLimiter) {
	t.Helper()
	mockLLM := &testingutil.MockLLM{Answer: `[FIELDS] profession=Plumber city=Madrid [/FIELDS] Profile saved!`}
	profiles := &testingutil.MockProfiles{
		Workers: []core.WorkerProfile{{UserID: "w1", Profession: "Plumber", City: "Madrid"}},
	}
	chats := &testingutil.MockChatRepo{ReturnID: "conv-1"}
	prompts := &testingutil.MockPrompts{}
	intakeSvc := services.NewIntakeService(mockLLM, profiles, chats, prompts)
	searchSvc := services.NewSearchService(mockLLM, profiles, chats, prompts)
	limiter := ratelimit.NewRateLimiter(searchRateLimit, time.Minute)
	return NewChatHandler(intakeSvc, searchSvc, prompts, limiter), limiter
}

// TestChatHandlerBodyTooLarge verifies the MaxBytesReader envelope. A body
// larger than maxBodyBytes returns 413 Request Entity Too Large before any
// JSON decode or LLM cost is incurred (P1-1 audit, F4).
func TestChatHandlerBodyTooLarge(t *testing.T) {
	h, _ := chatSetupWithLimiter(t)
	rec := httptest.NewRecorder()

	// 65 KiB > 64 KiB cap.
	big := strings.Repeat("x", 65*1024)
	body := `{"mode":"worker_intake","message":"` + big + `"}`
	h.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/chat", body))

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

// TestChatHandlerMessageTooLong verifies the per-message char cap. The body
// itself is well under maxBodyBytes, but the message field exceeds the
// defense-in-depth 8000-char limit (P1-1 audit).
func TestChatHandlerMessageTooLong(t *testing.T) {
	h, _ := chatSetupWithLimiter(t)
	rec := httptest.NewRecorder()

	big := strings.Repeat("m", maxMessageLength+1)
	body := `{"mode":"worker_intake","message":"` + big + `"}`
	h.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/chat", body))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "message too long", resp["error"])
}

// TestChatHandlerIntakeRateLimit verifies worker_intake is rate-limited.
// This was the audit's PARTIAL gap: search had a 10/min cap, intake didn't.
// Both modes now share one per-user budget (P1-1 audit, F4).
func TestChatHandlerIntakeRateLimit(t *testing.T) {
	h, _ := chatSetupWithLimiter(t)

	// searchRateLimit successful intake calls; then 429.
	for i := 0; i < searchRateLimit; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/chat", `{"mode":"worker_intake","message":"hello"}`))
		assert.Equal(t, http.StatusOK, rec.Code, "intake call %d should pass", i+1)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/chat", `{"mode":"client_intake","message":"hello"}`))
	assert.Equal(t, http.StatusTooManyRequests, rec.Code,
		"intake mode should be rate-limited once token budget is exhausted")

	// Pin the response shape so future refactors don't accidentally
	// remove the "rate limit exceeded" marker (the prior per-mode message
	// was "search rate limit exceeded"; the audit unification drops "search").
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Contains(t, resp["error"], "rate limit exceeded",
		"P1-1 audit: response must surface the rate-limit reason")
}

// TestChatHandlerSearchRateLimit verifies search mode is still rate-limited
// (it was already; this guards against future regressions).
func TestChatHandlerSearchRateLimit(t *testing.T) {
	h, _ := chatSetupWithLimiter(t)

	for i := 0; i < searchRateLimit; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/chat", `{"mode":"search","message":"plumber in Madrid"}`))
		assert.Equal(t, http.StatusOK, rec.Code, "search call %d should pass", i+1)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/chat", `{"mode":"search","message":"plumber in Barcelona"}`))
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}
