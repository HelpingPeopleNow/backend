package ports

import (
	"context"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

// SentimentScannerRepository abstracts the persistence operations needed
// by the background sentiment scanner. It is intentionally separate from
// DirectMessageRepository to keep the scanner's dependency surface small.
type SentimentScannerRepository interface {
	// FindEligibleConversations returns IDs of direct conversations that
	// are due for sentiment scoring. cooldown controls how long after a
	// previous score a row becomes eligible again.
	FindEligibleConversations(ctx context.Context, cooldown time.Duration, limit int) ([]string, error)

	// FetchMessages returns the most recent messages for a conversation,
	// oldest first, up to max messages.
	FetchMessages(ctx context.Context, conversationID string, max int) ([]core.DirectMessage, error)

	// WriteScore persists the sentiment score and reason for a conversation.
	WriteScore(ctx context.Context, conversationID string, score int16, reason string) error

	// ClearScore clears any previously stored sentiment score. Exposed for
	// tests and future reset paths; the production message-insert reset
	// happens inline inside SendMessage.
	ClearScore(ctx context.Context, conversationID string) error

	// FetchParticipantEmails returns the email addresses of both participants
	// in a direct conversation, looked up from the user table.
	FetchParticipantEmails(ctx context.Context, conversationID string) (emailA, emailB string, err error)
}
