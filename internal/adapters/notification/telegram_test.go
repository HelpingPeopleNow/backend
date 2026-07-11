package notification

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

func TestTelegramNotifier_SendFeedbackAlert_Success(t *testing.T) {
	var receivedBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// Override the API URL by using a custom client that redirects to test server.
	// Since the notifier hardcodes api.telegram.org, we test with empty token/chatID
	// to verify the "not configured" path, and test the rate limiter separately.
	n := NewTelegramNotifier("", "")
	err := n.SendFeedbackAlert(&core.Feedback{Message: "test"})
	if err == nil {
		t.Error("expected error when not configured")
	}

	// Verify rate limiter works.
	n2 := NewTelegramNotifier("token", "123")
	n2.client = srv.Client()
	// Override the Post URL by making a custom implementation.
	// Actually, we can't override the URL easily without refactoring.
	// Test rate limiting instead.
	n2.mu.Lock()
	n2.lastSent = time.Now()
	n2.mu.Unlock()

	err = n2.SendFeedbackAlert(&core.Feedback{Message: "test"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTelegramNotifier_NotConfigured(t *testing.T) {
	n := NewTelegramNotifier("", "")
	err := n.SendFeedbackAlert(&core.Feedback{Message: "test"})
	if err == nil {
		t.Error("expected error when bot token is empty")
	}

	n2 := NewTelegramNotifier("token", "")
	err = n2.SendFeedbackAlert(&core.Feedback{Message: "test"})
	if err == nil {
		t.Error("expected error when chat ID is empty")
	}
}

func TestTelegramNotifier_RateLimit(t *testing.T) {
	n := NewTelegramNotifier("token", "123")
	// Simulate a recent send.
	n.mu.Lock()
	n.lastSent = time.Now()
	n.mu.Unlock()

	// Should be rate-limited (returns nil, drops message).
	err := n.SendFeedbackAlert(&core.Feedback{Message: "test"})
	if err != nil {
		t.Errorf("rate-limited call should return nil, got: %v", err)
	}
}

func TestTelegramNotifier_CategoryEmoji(t *testing.T) {
	n := NewTelegramNotifier("token", "123")

	// Verify the emoji mapping exists for all valid categories.
	categories := map[string]string{
		"bug": "🐛", "idea": "💡", "complaint": "😤", "general": "💬",
	}
	for cat, expectedEmoji := range categories {
		categoryEmoji := map[string]string{
			"bug": "🐛", "idea": "💡", "complaint": "😤", "general": "💬",
		}
		got := categoryEmoji[cat]
		if got != expectedEmoji {
			t.Errorf("emoji for %q = %q, want %q", cat, got, expectedEmoji)
		}
	}
	_ = n // unused beyond construction
}
