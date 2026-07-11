package notification

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

func TestSendFeedbackAlert_Success(t *testing.T) {
	var received map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &received)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	n := NewTelegramNotifierWithClient("token123", "chat456", srv.URL, srv.Client())
	err := n.SendFeedbackAlert(&core.Feedback{
		UserID:   "user-1",
		PageURL:  "https://helpingpeople.cloud",
		Category: "bug",
		Message:  "button is broken",
	})
	if err != nil {
		t.Fatalf("expected nil error on success, got: %v", err)
	}
	if received["chat_id"] != "chat456" {
		t.Errorf("expected chat_id chat456, got %v", received["chat_id"])
	}
	if !strings.Contains(received["text"].(string), "button is broken") {
		t.Errorf("expected message text to contain feedback, got %v", received["text"])
	}
}

func TestSendFeedbackAlert_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	n := NewTelegramNotifierWithClient("token123", "chat456", srv.URL, srv.Client())
	err := n.SendFeedbackAlert(&core.Feedback{Message: "test"})
	if err == nil {
		t.Fatal("expected error on non-OK HTTP status")
	}
	if !strings.Contains(err.Error(), "telegram API returned 502") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSendFeedbackAlert_NetworkError(t *testing.T) {
	// Point to a non-existent server.
	client := &http.Client{Timeout: 100 * time.Millisecond}
	n := NewTelegramNotifierWithClient("token123", "chat456", "http://127.0.0.1:1", client)
	err := n.SendFeedbackAlert(&core.Feedback{Message: "test"})
	if err == nil {
		t.Fatal("expected error on network failure")
	}
}

func TestSendFeedbackAlert_NotConfigured_EmptyToken(t *testing.T) {
	n := NewTelegramNotifierWithClient("", "chat456", "http://example.com", nil)
	err := n.SendFeedbackAlert(&core.Feedback{Message: "test"})
	if err == nil {
		t.Fatal("expected error when bot token is empty")
	}
}

func TestSendFeedbackAlert_NotConfigured_EmptyChatID(t *testing.T) {
	n := NewTelegramNotifierWithClient("token123", "", "http://example.com", nil)
	err := n.SendFeedbackAlert(&core.Feedback{Message: "test"})
	if err == nil {
		t.Fatal("expected error when chat ID is empty")
	}
}

func TestSendFeedbackAlert_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("expected no HTTP request when rate limited")
	}))
	defer srv.Close()

	n := NewTelegramNotifierWithClient("token123", "chat456", srv.URL, srv.Client())
	n.mu.Lock()
	n.lastSent = time.Now()
	n.mu.Unlock()

	// Should be rate-limited (returns nil, drops message).
	err := n.SendFeedbackAlert(&core.Feedback{Message: "test"})
	if err != nil {
		t.Errorf("rate-limited call should return nil, got: %v", err)
	}
}

func TestSendFeedbackAlert_CategoryEmoji(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	n := NewTelegramNotifierWithClient("token123", "chat456", srv.URL, srv.Client())

	tests := []struct {
		category string
		want     string
	}{
		{"bug", "🐛"},
		{"idea", "💡"},
		{"complaint", "😤"},
		{"general", "💬"},
		{"unknown", "💬"},
	}

	for _, tt := range tests {
		// Reset rate limit by advancing lastSent well into the past.
		n.mu.Lock()
		n.lastSent = time.Now().Add(-2 * time.Second)
		n.mu.Unlock()

		err := n.SendFeedbackAlert(&core.Feedback{Category: tt.category, Message: "msg"})
		if err != nil {
			t.Errorf("category %q: unexpected error: %v", tt.category, err)
		}
	}
}
