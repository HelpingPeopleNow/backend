package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	client   *http.Client
	mu       sync.Mutex
	lastSent time.Time
}

func NewTelegramNotifier(botToken, chatID string) *TelegramNotifier {
	return &TelegramNotifier{
		botToken: botToken,
		chatID:   chatID,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *TelegramNotifier) SendFeedbackAlert(fb *core.Feedback) error {
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
			"📄 Page: %s\n"+
			"🏷️ Category: %s %s\n\n"+
			"\"%s\"",
		fb.UserID, fb.PageURL, emoji, fb.Category, fb.Message,
	)

	body, _ := json.Marshal(map[string]interface{}{
		"chat_id":    n.chatID,
		"text":       text,
		"parse_mode": "HTML",
	})

	resp, err := n.client.Post(
		fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.botToken),
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
