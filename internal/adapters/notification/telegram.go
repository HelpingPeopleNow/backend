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
	botToken string
	chatID   string
	baseURL  string
	client   *http.Client
	mu       sync.Mutex
	lastSent time.Time
}

const defaultTelegramBaseURL = "https://api.telegram.org"

// NewTelegramNotifier creates a TelegramNotifier with sane defaults.
// Pass empty strings to disable notifications (returns error on Send).
func NewTelegramNotifier(botToken, chatID string) *TelegramNotifier {
	return &TelegramNotifier{
		botToken: botToken,
		chatID:   chatID,
		baseURL:  defaultTelegramBaseURL,
		client:   &http.Client{Timeout: 10 * time.Second},
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
		botToken: botToken,
		chatID:   chatID,
		baseURL:  baseURL,
		client:   client,
	}
}

func (n *TelegramNotifier) SendFeedbackAlert(fb *core.Feedback) error {
	slog.Info("notifier: SendFeedbackAlert", "user_id", fb.UserID, "category", fb.Category)
	if n.botToken == "" || n.chatID == "" {
		return fmt.Errorf("telegram not configured")
	}

	// Global rate limit: max 1 msg/sec to avoid flooding.
	n.mu.Lock()
	if time.Since(n.lastSent) < time.Second {
		n.mu.Unlock()
		return nil // drop — feedback is still saved in DB
	}
	n.lastSent = time.Now()
	n.mu.Unlock()

	categoryEmoji := map[string]string{
		"bug": "🐛", "idea": "💡", "complaint": "😤", "general": "💬",
	}
	emoji := categoryEmoji[fb.Category]
	if emoji == "" {
		emoji = "💬"
	}

	text := fmt.Sprintf(
		"💬 New Feedback\n\n"+
			"👤 User: %s\n"+
			"📧 Email: %s\n"+
			"📄 Page: %s\n"+
			"🏷️ Category: %s %s\n\n"+
			"\"%s\"",
		fb.UserID, fb.Email, fb.PageURL, emoji, fb.Category, fb.Message,
	)

	body, _ := json.Marshal(map[string]interface{}{
		"chat_id":    n.chatID,
		"text":       text,
		"parse_mode": "HTML",
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
