package core

import (
	"time"

	"gorm.io/gorm"
)

const MaxDirectMessageLength = 4000

// DirectMessage is a single message in a direct conversation between two users.
// No role is stored — sender_id identifies the user who sent it.
type DirectMessage struct {
	ID             string          `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ConversationID string          `gorm:"type:uuid;not null;index" json:"conversation_id"`
	SenderID       string          `gorm:"type:text;not null" json:"sender_id"`
	Body           string          `gorm:"type:text;not null" json:"body"`
	CreatedAt      time.Time       `json:"created_at"`
	ReadAt         *time.Time      `json:"read_at,omitempty"`
	DeletedAt      gorm.DeletedAt  `gorm:"index" json:"deleted_at,omitempty"`
}

func (DirectMessage) TableName() string { return "direct_messages" }
