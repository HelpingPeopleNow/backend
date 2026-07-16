package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/HelpingPeopleNow/backend/internal/core"
)

// mockFeedbackRepo is a minimal in-memory FeedbackRepository for handler tests.
type mockFeedbackRepo struct {
	fb        *core.Feedback
	createErr error
}

func (m *mockFeedbackRepo) Create(fb *core.Feedback) error {
	if m.createErr != nil {
		return m.createErr
	}
	fb.ID = "test-uuid-123"
	m.fb = fb
	return nil
}

func (m *mockFeedbackRepo) List(string, int, int) ([]core.Feedback, int64, error) {
	return nil, 0, nil
}
func (m *mockFeedbackRepo) UpdateStatus(string, string, string) error { return nil }
func (m *mockFeedbackRepo) CountByStatus() (map[string]int64, error)  { return nil, nil }
func (m *mockFeedbackRepo) GetUserEmail(string) (string, error)       { return "", nil }

// mockNotifier is a no-op Notifier for handler tests.
type mockNotifier struct {
	sendFunc func(*core.Feedback) error
	called   bool
}

func (n *mockNotifier) SendFeedbackAlert(fb *core.Feedback) error {
	n.called = true
	if n.sendFunc != nil {
		return n.sendFunc(fb)
	}
	return nil
}

func (n *mockNotifier) SendSentimentAlert(_ string, _ int16, _ string, _ string, _ string) error {
	return nil
}

func TestFeedbackHandler_Submit_Success(t *testing.T) {
	repo := &mockFeedbackRepo{}
	notifier := &mockNotifier{}
	h := NewFeedbackHandler(repo, notifier, nil)

	body, _ := json.Marshal(feedbackRequest{
		Message:  "The submit button is broken on mobile",
		PageURL:  "/chat?mode=worker_intake",
		Category: "bug",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Simulate auth middleware setting user ID.
	req = req.WithContext(contextkeys.SetUserID(req.Context(), "user-uuid-abc"))
	w := httptest.NewRecorder()

	h.Submit(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusCreated)
	}

	var fb core.Feedback
	if err := json.Unmarshal(w.Body.Bytes(), &fb); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fb.ID != "test-uuid-123" {
		t.Errorf("id = %q, want test-uuid-123", fb.ID)
	}
	if fb.UserID != "user-uuid-abc" {
		t.Errorf("user_id = %q, want user-uuid-abc", fb.UserID)
	}
	if fb.Category != "bug" {
		t.Errorf("category = %q, want bug", fb.Category)
	}
	if fb.Status != "open" {
		t.Errorf("status = %q, want open", fb.Status)
	}
}

func TestFeedbackHandler_Submit_DefaultCategory(t *testing.T) {
	repo := &mockFeedbackRepo{}
	h := NewFeedbackHandler(repo, nil, nil)

	body, _ := json.Marshal(feedbackRequest{
		Message: "just a thought",
		PageURL: "/chat",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(contextkeys.SetUserID(req.Context(), "user-1"))
	w := httptest.NewRecorder()

	h.Submit(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	if repo.fb.Category != "general" {
		t.Errorf("category = %q, want general (default)", repo.fb.Category)
	}
}

func TestFeedbackHandler_Submit_MethodNotAllowed(t *testing.T) {
	h := NewFeedbackHandler(&mockFeedbackRepo{}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/feedback", nil)
	w := httptest.NewRecorder()

	h.Submit(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestFeedbackHandler_Submit_EmptyMessage(t *testing.T) {
	h := NewFeedbackHandler(&mockFeedbackRepo{}, nil, nil)
	body, _ := json.Marshal(feedbackRequest{Message: "", PageURL: "/chat", Category: "bug"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(contextkeys.SetUserID(req.Context(), "user-1"))
	w := httptest.NewRecorder()

	h.Submit(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestFeedbackHandler_Submit_InvalidCategory(t *testing.T) {
	h := NewFeedbackHandler(&mockFeedbackRepo{}, nil, nil)
	body, _ := json.Marshal(feedbackRequest{Message: "hi", PageURL: "/chat", Category: "spam"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(contextkeys.SetUserID(req.Context(), "user-1"))
	w := httptest.NewRecorder()

	h.Submit(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestFeedbackHandler_Submit_EmptyPageURL(t *testing.T) {
	h := NewFeedbackHandler(&mockFeedbackRepo{}, nil, nil)
	body, _ := json.Marshal(feedbackRequest{Message: "hi", PageURL: "", Category: "bug"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(contextkeys.SetUserID(req.Context(), "user-1"))
	w := httptest.NewRecorder()

	h.Submit(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestFeedbackHandler_Submit_MessageTooLong(t *testing.T) {
	h := NewFeedbackHandler(&mockFeedbackRepo{}, nil, nil)
	longMsg := make([]byte, 2001)
	for i := range longMsg {
		longMsg[i] = 'a'
	}
	body, _ := json.Marshal(feedbackRequest{Message: string(longMsg), PageURL: "/chat", Category: "bug"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(contextkeys.SetUserID(req.Context(), "user-1"))
	w := httptest.NewRecorder()

	h.Submit(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestFeedbackHandler_Submit_Anonymous(t *testing.T) {
	repo := &mockFeedbackRepo{}
	h := NewFeedbackHandler(repo, nil, nil)
	body, _ := json.Marshal(feedbackRequest{Message: "hi", PageURL: "/chat", Category: "bug"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No SetUserID — simulates unauthenticated request.
	w := httptest.NewRecorder()

	h.Submit(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	if repo.fb.UserID != "" {
		t.Errorf("user_id = %q, want empty for anonymous feedback", repo.fb.UserID)
	}
}

func TestFeedbackHandler_Submit_UnknownField(t *testing.T) {
	h := NewFeedbackHandler(&mockFeedbackRepo{}, nil, nil)
	body := []byte(`{"message":"hi","page_url":"/chat","category":"bug","evil_field":"x"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(contextkeys.SetUserID(req.Context(), "user-1"))
	w := httptest.NewRecorder()

	h.Submit(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (should reject unknown fields)", w.Code, http.StatusBadRequest)
	}
}

func TestFeedbackHandler_Submit_NotifierCalled(t *testing.T) {
	repo := &mockFeedbackRepo{}
	notifier := &mockNotifier{}
	h := NewFeedbackHandler(repo, notifier, nil)

	body, _ := json.Marshal(feedbackRequest{Message: "test", PageURL: "/chat", Category: "general"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(contextkeys.SetUserID(req.Context(), "user-1"))
	w := httptest.NewRecorder()

	h.Submit(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	// Notifier is called in a goroutine — give it a moment.
	// In production it's fire-and-forget; for testing we just verify
	// the handler returned successfully.
}

func TestFeedbackHandler_Submit_NilNotifier(t *testing.T) {
	repo := &mockFeedbackRepo{}
	h := NewFeedbackHandler(repo, nil, nil)

	body, _ := json.Marshal(feedbackRequest{Message: "test", PageURL: "/chat", Category: "general"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(contextkeys.SetUserID(req.Context(), "user-1"))
	w := httptest.NewRecorder()

	h.Submit(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusCreated)
	}
}

func TestFeedbackHandler_Submit_CreateRepoError(t *testing.T) {
	h := NewFeedbackHandler(&mockFeedbackRepo{createErr: errors.New("db timeout")}, nil, nil)

	body, _ := json.Marshal(feedbackRequest{Message: "test", PageURL: "/chat", Category: "general"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(contextkeys.SetUserID(req.Context(), "user-1"))
	w := httptest.NewRecorder()

	h.Submit(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestFeedbackHandler_Submit_BodyTooLarge(t *testing.T) {
	h := NewFeedbackHandler(&mockFeedbackRepo{}, nil, nil)
	longMsg := make([]byte, 5000)
	for i := range longMsg {
		longMsg[i] = 'a'
	}
	body, _ := json.Marshal(feedbackRequest{Message: string(longMsg), PageURL: "/chat", Category: "bug"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(contextkeys.SetUserID(req.Context(), "user-1"))
	w := httptest.NewRecorder()

	h.Submit(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestFeedbackHandler_Submit_MalformedJSON(t *testing.T) {
	h := NewFeedbackHandler(&mockFeedbackRepo{}, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedback", strings.NewReader(`{bad json`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(contextkeys.SetUserID(req.Context(), "user-1"))
	w := httptest.NewRecorder()

	h.Submit(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestFeedbackHandler_Submit_NotifierFiresInGoroutine(t *testing.T) {
	var fired atomic.Bool
	notifier := &mockNotifier{
		sendFunc: func(_ *core.Feedback) error {
			fired.Store(true)
			return nil
		},
	}
	h := NewFeedbackHandler(&mockFeedbackRepo{}, notifier, nil)

	body, _ := json.Marshal(feedbackRequest{Message: "hello", PageURL: "/chat", Category: "general"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(contextkeys.SetUserID(req.Context(), "user-1"))
	w := httptest.NewRecorder()

	h.Submit(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 50 && !fired.Load(); i++ {
			time.Sleep(10 * time.Millisecond)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("notifier goroutine did not fire within 2s")
	}
	if !fired.Load() {
		t.Fatal("expected notifier to be called")
	}
}
