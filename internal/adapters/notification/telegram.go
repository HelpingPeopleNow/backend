package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

// TelegramNotifier sends feedback alerts via the Telegram Bot API.
// It implements ports.Notifier.
type TelegramNotifier struct {
	botToken     string
	chatID       string
	baseURL      string
	baseAdminURL string
	client       *http.Client
	mu           sync.Mutex
	lastSent     time.Time
}

const defaultTelegramBaseURL = "https://api.telegram.org"

// NewTelegramNotifier creates a TelegramNotifier with sane defaults.
// Pass empty strings to disable notifications (returns error on Send).
// baseAdminURL is used to build links in alert messages (e.g. "http://localhost" or "https://helpingpeople.cloud").
func NewTelegramNotifier(botToken, chatID, baseAdminURL string) *TelegramNotifier {
	if baseAdminURL == "" {
		baseAdminURL = "https://helpingpeople.cloud"
	}
	return &TelegramNotifier{
		botToken:     botToken,
		chatID:       chatID,
		baseURL:      defaultTelegramBaseURL,
		baseAdminURL: baseAdminURL,
		client:       &http.Client{Timeout: 10 * time.Second},
	}
}

// NewTelegramNotifierWithClient is a test constructor that lets tests inject
// a custom HTTP client (e.g. to redirect requests to httptest.NewServer).
func NewTelegramNotifierWithClient(botToken, chatID, baseURL string, client *http.Client) *TelegramNotifier {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if baseURL == "" {
		baseURL = defaultTelegramBaseURL
	}
	return &TelegramNotifier{
		botToken:     botToken,
		chatID:       chatID,
		baseURL:      baseURL,
		baseAdminURL: "https://helpingpeople.cloud",
		client:       client,
	}
}

func (n *TelegramNotifier) SendFeedbackAlert(fb *core.Feedback) error {
	slog.Info("notifier: SendFeedbackAlert", "user_id", fb.UserID, "category", fb.Category)
	if n.botToken == "" || n.chatID == "" {
		return fmt.Errorf("telegram not configured")
	}

	return n.sendMessage(fmt.Sprintf(
		"💬 New Feedback\n\n"+
			"👤 User: %s\n"+
			"📧 Email: %s\n"+
			"📄 Page: %s\n"+
			"🏷️ Category: %s %s\n\n"+
			"\"%s\"",
		fb.UserID, fb.Email, fb.PageURL, feedbackCategoryEmoji(fb.Category), fb.Category, fb.Message,
	))
}

// SendSentimentAlert notifies operators that a direct-message conversation
// has a critically low sentiment score (at or below the alert threshold).
func (n *TelegramNotifier) SendSentimentAlert(convID string, score int16, reason string, emailA, emailB string) error {
	slog.Info("notifier: SendSentimentAlert", "conv_id", convID, "score", score)
	if n.botToken == "" || n.chatID == "" {
		return fmt.Errorf("telegram not configured")
	}

	adminLink := fmt.Sprintf("%s/admin/direct-conversations?id=%s", n.baseAdminURL, convID)

	return n.sendMessage(fmt.Sprintf(
		"🚨 Low Sentiment Alert\n\n"+
			"👥 %s ↔ %s\n"+
			"⭐ Score: %d/10\n"+
			"📝 %s\n\n"+
			"%s",
		emailA, emailB,
		score,
		reason,
		adminLink,
	))
}

func (n *TelegramNotifier) sendMessage(text string) error {
	// Global rate limit: max 1 msg/sec to avoid flooding.
	n.mu.Lock()
	if time.Since(n.lastSent) < time.Second {
		n.mu.Unlock()
		return nil // drop — notification is best-effort
	}
	n.lastSent = time.Now()
	n.mu.Unlock()

	body, _ := json.Marshal(map[string]interface{}{
		"chat_id": n.chatID,
		"text":    text,
	})

	resp, err := n.client.Post(
		fmt.Sprintf("%s/bot%s/sendMessage", n.baseURL, n.botToken),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned %d", resp.StatusCode)
	}
	return nil
}

func feedbackCategoryEmoji(category string) string {
	emojis := map[string]string{
		"bug": "🐛", "idea": "💡", "complaint": "😤", "general": "💬",
	}
	if emoji, ok := emojis[category]; ok {
		return emoji
	}
	return "💬"
}
