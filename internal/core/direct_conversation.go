package core

import "time"

// DirectConversation is a private 1:1 conversation between a client and a worker.
// One conversation per (client_id, worker_profile_id) pair — enforced by UNIQUE constraint.
type DirectConversation struct {
	ID                 string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ClientID           string     `gorm:"type:text;not null;index" json:"client_id"`
	WorkerProfileID    string     `gorm:"type:uuid;not null;index;column:worker_profile_id" json:"worker_profile_id"`
	Status             string     `gorm:"type:varchar(20);not null;default:'active'" json:"status"`
	ClientArchivedAt   *time.Time `json:"client_archived_at,omitempty"`
	WorkerArchivedAt   *time.Time `json:"worker_archived_at,omitempty"`
	LastMessageAt      *time.Time `json:"last_message_at,omitempty"`
	LastMessagePreview string     `gorm:"type:text" json:"last_message_preview"`
	ClientUnreadCount  int        `gorm:"not null;default:0" json:"client_unread_count"`
	WorkerUnreadCount  int        `gorm:"not null;default:0" json:"worker_unread_count"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func (DirectConversation) TableName() string { return "direct_conversations" }

// IsActive returns true when the conversation is not blocked or archived by both parties.
func (c DirectConversation) IsActive() bool { return c.Status == "active" }

// IsBlocked returns true when either party has blocked the conversation.
func (c DirectConversation) IsBlocked() bool { return c.Status == "blocked" }
