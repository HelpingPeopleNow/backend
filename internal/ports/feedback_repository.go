package ports

import "github.com/HelpingPeopleNow/backend/internal/core"

// FeedbackRepository persists feedback submissions.
type FeedbackRepository interface {
	Create(fb *core.Feedback) error
	List(status string, limit, offset int) ([]core.Feedback, int64, error)
	UpdateStatus(id string, status, adminNote string) error
	CountByStatus() (map[string]int64, error)
	// GetUserEmail returns the email for a given user ID, or "" if not found.
	GetUserEmail(userID string) (string, error)
}
