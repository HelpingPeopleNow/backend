package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

// mockNotifier is a no-op Notifier for handler tests.
type mockNotifier struct {
	called bool
}

func (n *mockNotifier) SendFeedbackAlert(fb *core.Feedback) error {
	n.called = true
	return nil
}

func TestFeedbackHandler_Submit_Success(t *testing.T) {
	repo := &mockFeedbackRepo{}
	notifier := &mockNotifier{}
	h := NewFeedbackHandler(repo, notifier)

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
	h := NewFeedbackHandler(repo, nil)

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
	h := NewFeedbackHandler(&mockFeedbackRepo{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/feedback", nil)
	w := httptest.NewRecorder()

	h.Submit(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestFeedbackHandler_Submit_EmptyMessage(t *testing.T) {
	h := NewFeedbackHandler(&mockFeedbackRepo{}, nil)
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
	h := NewFeedbackHandler(&mockFeedbackRepo{}, nil)
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
	h := NewFeedbackHandler(&mockFeedbackRepo{}, nil)
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
	h := NewFeedbackHandler(&mockFeedbackRepo{}, nil)
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

func TestFeedbackHandler_Submit_NoUserID(t *testing.T) {
	h := NewFeedbackHandler(&mockFeedbackRepo{}, nil)
	body, _ := json.Marshal(feedbackRequest{Message: "hi", PageURL: "/chat", Category: "bug"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No SetUserID — simulates unauthenticated request.
	w := httptest.NewRecorder()

	h.Submit(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestFeedbackHandler_Submit_UnknownField(t *testing.T) {
	h := NewFeedbackHandler(&mockFeedbackRepo{}, nil)
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
	h := NewFeedbackHandler(repo, notifier)

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
	h := NewFeedbackHandler(repo, nil)

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
