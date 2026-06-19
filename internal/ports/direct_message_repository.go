package ports

import (
	"context"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

// DirectMessageRepository manages direct conversations and messages.
type DirectMessageRepository interface {
	// Conversations

	// GetOrCreateConversation finds or creates a conversation between a client and worker.
	// Returns the conversation and a flag indicating whether it was newly created.
	GetOrCreateConversation(ctx context.Context, clientID, workerProfileID string) (*core.DirectConversation, bool, error)

	// GetConversation returns a conversation by ID.
	GetConversation(ctx context.Context, conversationID string) (*core.DirectConversation, error)

	// ListConversations returns conversations for a user, ordered by last_message_at DESC.
	// The caller specifies whether to filter by client_id or worker_id via userID + role.
	// before is an optional cursor (last_message_at value); nil means fetch newest.
	ListConversations(ctx context.Context, userID string, role string, status string, limit int, before *time.Time) ([]core.DirectConversation, error)

	// ArchiveConversation sets client_archived_at or worker_archived_at for the calling user.
	ArchiveConversation(ctx context.Context, conversationID, userID, role string) error

	// BlockConversation sets status='blocked'.
	BlockConversation(ctx context.Context, conversationID string) error

	// Messages

	// GetMessages returns messages for a conversation, ordered by created_at DESC.
	// Cursor pagination: before is a message ID; if empty, returns newest messages.
	GetMessages(ctx context.Context, conversationID string, limit int, before string) ([]core.DirectMessage, error)

	// SendMessage inserts a message and updates the conversation's last_message fields.
	// Increments the OTHER party's unread count.
	SendMessage(ctx context.Context, msg *core.DirectMessage) error

	// MarkRead marks all unread messages in a conversation as read for the given reader role.
	// Returns the count of messages that were marked read.
	MarkRead(ctx context.Context, conversationID, readerRole string) (int, error)

	// PollSince returns messages for a user that were created after the given time.
	// Used as polling fallback when SSE is unavailable.
	PollSince(ctx context.Context, userID string, since time.Time) ([]core.DirectMessage, error)

	// Helpers

	// GetWorkerByProfileID loads a worker profile by its UUID (worker_profiles.id).
	GetWorkerByProfileID(ctx context.Context, profileID string) (*core.WorkerProfile, error)

	// IsParticipant checks whether the given user is a participant in a conversation.
	// Returns (isParticipant, role, error).
	IsParticipant(ctx context.Context, convID, userID string) (bool, string, error)
}
