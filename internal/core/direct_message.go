package core

import (
	"time"

	"gorm.io/gorm"
)

const MaxDirectMessageLength = 4000

// DirectMessage roles — fixed enum (audit: matches direct_conversations.user_a_role /
// user_b_role columns which are VARCHAR(10) NOT NULL DEFAULT 'user').
// Resolution is O(1) at send-time via DirectConversation.SenderRole(userID).
const (
	DirectMessageRoleUser   = "user"
	DirectMessageRoleClient = "client"
	DirectMessageRoleWorker = "worker"
)

// DirectMessage is a single message in a direct conversation between two users.
//
// SenderID identifies the sender by Better Auth user ID.
//
// SenderRole is denormalized onto the message at insert time so the
// DB-side NOT NULL constraint can be satisfied without per-send profile
// lookups. Source of truth at contact-creation time is the conversation
// row's (UserARole, UserBRole) pair, which the handler resolves via
// profile lookups; per-message values are snapshotted FROM that pair
// in DirectMessagingHandler.sendMessage so the role cannot drift
// independently over time.
type DirectMessage struct {
	ID             string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ConversationID string         `gorm:"type:uuid;not null;index" json:"conversation_id"`
	SenderID       string         `gorm:"type:text;not null" json:"sender_id"`
	SenderRole     string         `gorm:"type:varchar(10);not null;default:'user'" json:"sender_role"`
	Body           string         `gorm:"type:text;not null" json:"body"`
	CreatedAt      time.Time      `json:"created_at"`
	ReadAt         *time.Time     `json:"read_at,omitempty"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (DirectMessage) TableName() string { return "direct_messages" }

// IsValidSenderRole returns true for the closed set of roles wired into
// the audit. Centralizes the validation surface so future enum additions
// (e.g., "admin", "system") only need to extend this predicate.
func IsValidSenderRole(role string) bool {
	switch role {
	case DirectMessageRoleUser, DirectMessageRoleClient, DirectMessageRoleWorker:
		return true
	default:
		return false
	}
}
