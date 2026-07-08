package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/adapters/realtime"
	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/testingutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── P2-4 (audit) — DisallowUnknownFields ─────────────────────────────

// TestChatHandlerRejectsUnknownField verifies that an extra JSON field
// beyond the chatRequest struct causes a 400 invalid-json response
// (instead of silently ignoring the field or mapping it to zero values).
// Frontend/src/services/chat.ts ChatRequest interface matches chatRequest
// exactly, so adding DisallowUnknownFields is a defense-in-depth guarantee.
func TestChatHandlerRejectsUnknownField(t *testing.T) {
	h := chatSetup()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/chat",
		`{"mode":"worker_intake","message":"hello","injected_field":"x"}`))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid json",
		"DisallowUnknownFields rejection must surface the same 'invalid json' reason as other decode errors")
}

// TestDirectMessagingHandlerSendMessageRejectsUnknownField — same
// coverage for the DM send body. Struct only has `body`; anything else
// is a probe or client bug.
func TestDirectMessagingHandlerSendMessageRejectsUnknownField(t *testing.T) {
	repo := &testingutil.MockDMRepo{
		Conv:              &core.DirectConversation{ID: "conv-1", UserAID: "user-1", UserBID: "other-1"},
		IsParticipantBool: true,
	}
	h := newDMHandlerWithRepo(repo)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPost, "/api/v1/direct-messages/conv-1/messages",
		`{"body":"hello","injected":"x"}`))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid json")
}

// TestSystemPromptHandlerRejectsUnknownField — admin PUT body has only
// `content`. Extra fields must 400.
func TestSystemPromptHandlerRejectsUnknownField(t *testing.T) {
	h := newSystemPromptHandler(&core.SystemPrompt{})
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/system-prompts/worker_profile",
		strings.NewReader(`{"content":"new prompt","injected":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := contextkeys.SetUserID(req.Context(), "admin-1")
	ctx = contextkeys.SetIsAdmin(ctx, true)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid json")
}

// TestReembedToggleRejectsUnknownField — toggle POST body has only
// `enabled`. Extra fields must 400.
func TestReembedToggleRejectsUnknownField(t *testing.T) {
	mock := &mockToggler{}
	h := NewReembedToggleHandler(mock)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reembed",
		strings.NewReader(`{"enabled":true,"injected":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid JSON body",
		"reembed handler emits its own reason string distinct from chat/dm")
}

// ── P2-5 (audit) — report handler hardening ─────────────────────────

// TestDirectMessagingHandlerReportRejectsEmptyReason — empty reason is
// a moderation no-op; the audit fixes the pre-fix silent-success behaviour.
func TestDirectMessagingHandlerReportRejectsEmptyReason(t *testing.T) {
	repo := &testingutil.MockDMRepo{IsParticipantBool: true}
	h := newDMHandlerWithRepo(repo)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPost, "/api/v1/direct-messages/conv-1/report",
		`{"reason":"   "}`))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "reason cannot be empty")
}

// TestDirectMessagingHandlerReportRejectsOversizedReason — >1000 chars
// is treated as abuse.
func TestDirectMessagingHandlerReportRejectsOversizedReason(t *testing.T) {
	repo := &testingutil.MockDMRepo{IsParticipantBool: true}
	h := newDMHandlerWithRepo(repo)
	big := strings.Repeat("x", maxReasonLength+1)
	body, _ := json.Marshal(map[string]string{"reason": big})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPost, "/api/v1/direct-messages/conv-1/report", string(body)))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "1000 characters or fewer")
}

// TestDirectMessagingHandlerReportRejectsOversizedBody — the
// MaxBytesReader envelope should fire on a body larger than 8 KiB.
func TestDirectMessagingHandlerReportRejectsOversizedBody(t *testing.T) {
	repo := &testingutil.MockDMRepo{IsParticipantBool: true}
	h := newDMHandlerWithRepo(repo)
	big := strings.Repeat("x", maxReportBodyBytes+1)
	body, _ := json.Marshal(map[string]string{"reason": "ok", "padding": big})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPost, "/api/v1/direct-messages/conv-1/report", string(body)))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Contains(t, rec.Body.String(), "request body too large")
}

// TestDirectMessagingHandlerReportRejectsInvalidJSON — malformed JSON
// must surface as 400 (pre-fix behaviour silently decoded and treated
// reason="" as legitimate).
func TestDirectMessagingHandlerReportRejectsInvalidJSON(t *testing.T) {
	repo := &testingutil.MockDMRepo{IsParticipantBool: true}
	h := newDMHandlerWithRepo(repo)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPost, "/api/v1/direct-messages/conv-1/report",
		`{"reason":`)) // missing value
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid json")
}

// TestDirectMessagingHandlerReportAcceptsValidReport — guards the
// happy-path after the validation tightening.
func TestDirectMessagingHandlerReportAcceptsValidReport(t *testing.T) {
	repo := &testingutil.MockDMRepo{IsParticipantBool: true}
	h := newDMHandlerWithRepo(repo)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, dmAuthReq(http.MethodPost, "/api/v1/direct-messages/conv-1/report",
		`{"reason":"user is spamming"}`))
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// ── P2-6 (audit) — SSE max-stream-duration env helper ──────────────

func TestSSEMaxStreamDurationDefault(t *testing.T) {
	t.Setenv("SSE_MAX_STREAM_DURATION", "")
	assert.Equal(t, 15*time.Minute, maxSSEStreamDuration())
}

func TestSSEMaxStreamDurationOverride(t *testing.T) {
	t.Setenv("SSE_MAX_STREAM_DURATION", "30s")
	assert.Equal(t, 30*time.Second, maxSSEStreamDuration())
}

func TestSSEMaxStreamDurationInvalidFallsBack(t *testing.T) {
	t.Setenv("SSE_MAX_STREAM_DURATION", "not-a-duration")
	assert.Equal(t, 15*time.Minute, maxSSEStreamDuration(),
		"invalid env value must fall back to default")
}

func TestSSEMaxStreamDurationZeroFallsBack(t *testing.T) {
	t.Setenv("SSE_MAX_STREAM_DURATION", "0s")
	assert.Equal(t, 15*time.Minute, maxSSEStreamDuration())
}

// ── P2-1 (audit) — Broker ActiveConnections gauge source ────────────

func TestMockBrokerActiveConnectionsEmpty(t *testing.T) {
	b := testingutil.NewMockBroker()
	assert.Equal(t, 0, b.ActiveConnections(), "empty broker should report 0 subscribers")
}

func TestMockBrokerActiveConnectionsCountsAcrossUsers(t *testing.T) {
	b := testingutil.NewMockBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 2 subs for userA, 1 for userB. ActiveConnections should sum both.
	_, _ = b.Subscribe(ctx, "userA")
	_, _ = b.Subscribe(ctx, "userA")
	_, _ = b.Subscribe(ctx, "userB")
	assert.Equal(t, 3, b.ActiveConnections())
}

func TestMockBrokerActiveConnectionsDecrementsAfterCancel(t *testing.T) {
	b := testingutil.NewMockBroker()
	ctx, cancel := context.WithCancel(context.Background())
	_, _ = b.Subscribe(ctx, "userA")
	require.Equal(t, 1, b.ActiveConnections())

	cancel()
	require.Eventually(t, func() bool { return b.ActiveConnections() == 0 },
		time.Second, 5*time.Millisecond,
		"cancel must free the broker slot")
}

// TestRealBrokerActiveConnectionsDecrementsAfterCancel — wires through
// the concrete *sseBroker and confirms ctx-cancel reaps the subscription.
// Uses require.Eventually (no sleep) so the test is race-safe.
func TestRealBrokerActiveConnectionsDecrementsAfterCancel(t *testing.T) {
	b := realtime.NewSSEBroker()
	ctx, cancel := context.WithCancel(context.Background())
	_, err := b.Subscribe(ctx, "userA")
	require.NoError(t, err)
	require.Equal(t, 1, b.ActiveConnections())

	cancel()
	require.Eventually(t, func() bool { return b.ActiveConnections() == 0 },
		time.Second, 5*time.Millisecond,
		"cancel must free the broker slot")
}
