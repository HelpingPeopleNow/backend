package core

import "time"

// DirectConversation is a private 1:1 conversation between two users.
// One conversation per (user_a_id, user_b_id) pair — enforced by UNIQUE constraint.
// user_a_id and user_b_id are sorted so user_a_id < user_b_id for consistency.
//
// UserARole and UserBRole denormalize the sender's role at contact-creation
// time so SendMessage can resolve the per-message sender_role in O(1) from
// the conversation row that the handler already loads (no per-send profile
// lookups). Resolved once, snapshotted onto every message via SenderRole.
type DirectConversation struct {
	ID                 string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	UserAID            string     `gorm:"type:text;not null;index:idx_direct_conv_a,priority:1;column:user_a_id" json:"user_a_id"`
	UserARole          string     `gorm:"type:varchar(10);not null;default:'user';column:user_a_role" json:"user_a_role"`
	UserBID            string     `gorm:"type:text;not null;index:idx_direct_conv_b,priority:1;column:user_b_id" json:"user_b_id"`
	UserBRole          string     `gorm:"type:varchar(10);not null;default:'user';column:user_b_role" json:"user_b_role"`
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

// SenderRole returns the role for the given participant. The caller must
// guarantee userID is a participant (IsParticipant / user_a_id == userID /
// user_b_id == userID) — unknown participants return "user" as a safe
// fallback so we never poison the DB-side NOT NULL constraint.
//
// Audit (sender_role NOT NULL): this is the SOLE source of truth for the
// role snapshotted onto every new DirectMessage. Keeping the logic here
// prevents duplication across getOrCreateContact and sendMessage.
func (c DirectConversation) SenderRole(userID string) string {
	if c.UserAID == userID {
		if c.UserARole != "" {
			return c.UserARole
		}
		return DirectMessageRoleUser
	}
	if c.UserBID == userID {
		if c.UserBRole != "" {
			return c.UserBRole
		}
		return DirectMessageRoleUser
	}
	return DirectMessageRoleUser
}
