package core

import "time"

// DirectConversation is a private 1:1 conversation between two users.
// One conversation per (user_a_id, user_b_id) pair — enforced by UNIQUE constraint.
// user_a_id and user_b_id are sorted so user_a_id < user_b_id for consistency.
type DirectConversation struct {
	ID                 string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	UserAID            string     `gorm:"type:text;not null;index:idx_direct_conv_a,priority:1;column:user_a_id" json:"user_a_id"`
	UserBID            string     `gorm:"type:text;not null;index:idx_direct_conv_b,priority:1;column:user_b_id" json:"user_b_id"`
	Status             string     `gorm:"type:varchar(20);not null;default:'active'" json:"status"`
	UserAArchivedAt    *time.Time `json:"user_a_archived_at,omitempty"`
	UserBArchivedAt    *time.Time `json:"user_b_archived_at,omitempty"`
	UserAUnreadCount   int        `gorm:"not null;default:0;column:user_a_unread_count" json:"user_a_unread_count"`
	UserBUnreadCount   int        `gorm:"not null;default:0;column:user_b_unread_count" json:"user_b_unread_count"`
	LastMessageAt      *time.Time `json:"last_message_at,omitempty"`
	LastMessagePreview string     `gorm:"type:text" json:"last_message_preview"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func (DirectConversation) TableName() string { return "direct_conversations" }

func (c DirectConversation) IsActive() bool { return c.Status == "active" }

func (c DirectConversation) IsBlocked() bool { return c.Status == "blocked" }

// OtherUserID returns the other participant's user ID.
func (c DirectConversation) OtherUserID(userID string) string {
	if c.UserAID == userID {
		return c.UserBID
	}
	return c.UserAID
}

// IsUserA returns true if the given user is user_a in this conversation.
func (c DirectConversation) IsUserA(userID string) bool {
	return c.UserAID == userID
}
