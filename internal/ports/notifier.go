package ports

import "github.com/HelpingPeopleNow/backend/internal/core"

// Notifier sends asynchronous notifications about system events.
// Implementations must be non-blocking — a failed notification
// must not prevent the caller from continuing.
type Notifier interface {
	SendFeedbackAlert(fb *core.Feedback) error
}
