package ports

import (
	"context"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

// DirectMessageRepository manages direct conversations and messages between users.
type DirectMessageRepository interface {
	// Conversations

	// GetOrCreateConversation finds or creates a conversation between two users.
	// userID1 and userID2 are sorted so the smaller ID becomes user_a_id.
	// userARole and userBRole are denormalized onto the conversation row
	// at creation time so SendMessage can resolve the per-message
	// sender_role in O(1) without per-send profile lookups (audit). On
	// the existing-conversation path the caller's roles are ignored —
	// a fresh contact can refresh them via UpdateConversationRoles.
	// Returns the conversation and a flag indicating whether it was newly created.
	GetOrCreateConversation(ctx context.Context, userID1, userARole, userID2, userBRole string) (*core.DirectConversation, bool, error)

	// UpdateConversationRoles patches (user_a_role, user_b_role) on an
	// existing conversation. Used when the caller wants to refresh the
	// cached roles (e.g., user flipped profile types or admin moderation
	// re-classifies a participant). No-op when the conversation is not
	// found.
	UpdateConversationRoles(ctx context.Context, conversationID, userARole, userBRole string) error

	// GetConversation returns a conversation by ID.
	GetConversation(ctx context.Context, conversationID string) (*core.DirectConversation, error)

	// ListConversations returns conversations for a user, ordered by last_message_at DESC.
	// Shows conversations where user is either user_a or user_b.
	// before is an optional cursor (last_message_at value); nil means fetch newest.
	ListConversations(ctx context.Context, userID string, status string, limit int, before *time.Time) ([]core.DirectConversation, error)

	// ArchiveConversation sets user_a_archived_at or user_b_archived_at for the calling user.
	ArchiveConversation(ctx context.Context, conversationID, userID string) error

	// BlockConversation sets status='blocked'.
	BlockConversation(ctx context.Context, conversationID string) error

	// Messages

	// GetMessages returns messages for a conversation, ordered by created_at DESC.
	// Cursor pagination: before is a message ID; if empty, returns newest messages.
	GetMessages(ctx context.Context, conversationID string, limit int, before string) ([]core.DirectMessage, error)

	// SendMessage inserts a message and updates the conversation's last_message fields.
	// Increments the OTHER party's unread count.
	SendMessage(ctx context.Context, msg *core.DirectMessage) error

	// MarkRead marks all unread messages in a conversation as read for the calling user.
	// Returns the count of messages that were marked read.
	MarkRead(ctx context.Context, conversationID, userID string) (int, error)

	// PollSince returns messages for a user that were created after the given time.
	// Used as polling fallback when SSE is unavailable.
	PollSince(ctx context.Context, userID string, since time.Time) ([]core.DirectMessage, error)

	// Helpers

	// IsParticipant checks whether the given user is a participant in a conversation.
	IsParticipant(ctx context.Context, convID, userID string) (bool, error)

	// Reports

	// CreateReport persists a report for a conversation.
	CreateReport(ctx context.Context, report *core.DirectMessageReport) error

	// ListReports returns all reports (for admin moderation).
	ListReports(ctx context.Context) ([]core.DirectMessageReport, error)
}
