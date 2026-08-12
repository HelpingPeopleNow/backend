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

	// FetchLastAlertSentAt returns the last time an alert was sent for this
	// conversation, or nil if never. Used to enforce the long-form cooldown
	// (default 24h) between alerts on the same conversation; see also
	// ClaimAlert / ReleaseAlertClaim for the short-term lease that prevents
	// duplicate concurrent dispatch.
	FetchLastAlertSentAt(ctx context.Context, conversationID string) (*time.Time, error)

	// MarkAlertSent records that an alert was sent for this conversation.
	// MUST only be called after a successful SendSentimentAlert return and
	// inside the same logical "I am dispatching" window as a prior
	// ClaimAlert success.
	MarkAlertSent(ctx context.Context, conversationID string) error

	// ClaimAlert performs an atomic UPDATE that sets alert_claim_at = NOW()
	// on the target conversation iff the row is unclaimed OR its current
	// claim is older than `lease` ago (lease expiry lets a crashed
	// replica's claim auto-recover). Returns claimed=true if THIS call is
	// the one that took the lease — only that caller should dispatch the
	// alert. Two replicas racing on the same conversation see exactly one
	// claimed=true result; the other gets claimed=false and must skip.
	//
	// SPOF GAP C fix (see infra/docs/FOLLOW_UP_SPOF_Backup_Replicas.md):
	// this is the atomic-CAS that closes the duplicate-alert TOCTOU when
	// the backend runs as N >= 2 concurrent instances.
	ClaimAlert(ctx context.Context, conversationID string, lease time.Duration) (claimed bool, err error)

	// ReleaseAlertClaim clears alert_claim_at so a subsequent tick can
	// retry the send. Called when SendSentimentAlert fails so the alert
	// is not lost (a MarkAlertSent-on-failure would lock the conversation
	// out of retries until the lease expires).
	ReleaseAlertClaim(ctx context.Context, conversationID string) error
}
