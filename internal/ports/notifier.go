package ports

import "github.com/HelpingPeopleNow/backend/internal/core"

// Notifier sends asynchronous notifications about system events.
// Implementations must be non-blocking — a failed notification
// must not prevent the caller from continuing.
type Notifier interface {
	SendFeedbackAlert(fb *core.Feedback) error
	// SendSentimentAlert fires when a direct-message conversation
	// receives a sentiment score at or below the alert threshold.
	// emailA and emailB are the participant email addresses.
	SendSentimentAlert(convID string, score int16, reason string, emailA, emailB string) error
}
