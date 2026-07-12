package core

import (
	"fmt"
	"time"
)

// ValidCategories is the set of allowed feedback category values.
var ValidCategories = map[string]bool{
	"bug": true, "idea": true, "complaint": true, "general": true,
}

// ValidStatuses is the set of allowed feedback status values.
var ValidStatuses = map[string]bool{
	"open": true, "in_progress": true, "resolved": true, "dismissed": true,
}

// Feedback represents a user-submitted feedback entry.
type Feedback struct {
	ID        string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	UserID    string    `gorm:"type:text;not null;index:idx_feedback_user_id" json:"user_id"`
	PageURL   string    `gorm:"type:text;not null" json:"page_url"`
	Message   string    `gorm:"type:text;not null" json:"message"`
	Category  string    `gorm:"type:text;not null;default:'general'" json:"category"`
	Status    string    `gorm:"type:text;not null;default:'open';index:idx_feedback_status" json:"status"`
	AdminNote *string   `gorm:"type:text" json:"admin_note,omitempty"`
	Email     string    `gorm:"-" json:"-"`
	CreatedAt time.Time `gorm:"autoCreateTime;index:idx_feedback_created_at,priority:1" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (Feedback) TableName() string { return "feedback" }

// Validate checks that the Feedback fields are within allowed bounds.
func (f *Feedback) Validate() error {
	if len(f.Message) < 1 || len(f.Message) > 2000 {
		return fmt.Errorf("message must be 1–2000 chars")
	}
	if !ValidCategories[f.Category] {
		return fmt.Errorf("invalid category %q", f.Category)
	}
	if f.PageURL == "" || len(f.PageURL) > 2048 {
		return fmt.Errorf("page_url must be 1–2048 chars")
	}
	return nil
}
