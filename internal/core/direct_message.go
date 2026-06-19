package core

import (
	"time"

	"gorm.io/gorm"
)

// DirectMessage is a single message in a client↔worker conversation.
type DirectMessage struct {
	ID             string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ConversationID string         `gorm:"type:uuid;not null;index" json:"conversation_id"`
	SenderID       string         `gorm:"type:text;not null" json:"sender_id"`
	SenderRole     string         `gorm:"type:varchar(10);not null" json:"sender_role"`
	Body           string         `gorm:"type:text;not null" json:"body"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
	ReadAt         *time.Time     `json:"read_at,omitempty"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (DirectMessage) TableName() string { return "direct_messages" }

const (
	MaxDirectMessageLength = 4000
	SenderRoleClient       = "client"
	SenderRoleWorker       = "worker"
)
