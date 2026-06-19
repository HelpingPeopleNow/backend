package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
	"gorm.io/gorm"
)

type GormDirectMessageRepository struct {
	db *gorm.DB
}

func NewGormDirectMessageRepository(db *gorm.DB) ports.DirectMessageRepository {
	return &GormDirectMessageRepository{db: db}
}

// ── Conversations ────────────────────────────────────────────────────────────

func (r *GormDirectMessageRepository) GetOrCreateConversation(
	ctx context.Context, clientID, workerProfileID string,
) (*core.DirectConversation, bool, error) {
	var conv core.DirectConversation
	err := r.db.WithContext(ctx).
		Where("client_id = ? AND worker_profile_id = ?", clientID, workerProfileID).
		First(&conv).Error
	if err == nil {
		// Existing conversation: un-archive for client if archived
		if conv.ClientArchivedAt != nil {
			_ = r.db.WithContext(ctx).Model(&conv).
				Update("client_archived_at", nil).Error
			conv.ClientArchivedAt = nil
		}
		return &conv, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, fmt.Errorf("find conversation: %w", err)
	}

	// Create new conversation
	conv = core.DirectConversation{
		ClientID:        clientID,
		WorkerProfileID: workerProfileID,
		Status:          "active",
	}
	if err := r.db.WithContext(ctx).Create(&conv).Error; err != nil {
		return nil, false, fmt.Errorf("create conversation: %w", err)
	}
	slog.Info("dm: conversation created",
		"conv_id", conv.ID, "client_id", clientID, "worker_profile_id", workerProfileID)
	return &conv, true, nil
}

func (r *GormDirectMessageRepository) GetConversation(
	ctx context.Context, conversationID string,
) (*core.DirectConversation, error) {
	var conv core.DirectConversation
	err := r.db.WithContext(ctx).Where("id = ?", conversationID).First(&conv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &conv, nil
}

func (r *GormDirectMessageRepository) ListConversations(
	ctx context.Context, userID string, role string, status string,
	limit int, before *time.Time,
) ([]core.DirectConversation, error) {
	query := r.db.WithContext(ctx).Model(&core.DirectConversation{})

	// Filter by user role
	switch role {
	case core.SenderRoleClient:
		query = query.Where("client_id = ?", userID)
	case core.SenderRoleWorker:
		query = query.Where("worker_profile_id IN (SELECT id FROM worker_profiles WHERE user_id = ?)", userID)
	default:
		return nil, fmt.Errorf("invalid role: %s", role)
	}

	if status != "" && status != "all" {
		query = query.Where("status = ?", status)
	}

	if before != nil {
		query = query.Where("last_message_at < ?", before)
	}

	query = query.Order("last_message_at DESC NULLS LAST").Limit(limit)

	var convs []core.DirectConversation
	if err := query.Find(&convs).Error; err != nil {
		return nil, err
	}
	return convs, nil
}

func (r *GormDirectMessageRepository) ArchiveConversation(
	ctx context.Context, conversationID, userID, role string,
) error {
	conv, err := r.GetConversation(ctx, conversationID)
	if err != nil || conv == nil {
		return fmt.Errorf("conversation not found: %s", conversationID)
	}

	now := time.Now()
	switch role {
	case core.SenderRoleClient:
		if conv.ClientID != userID {
			return fmt.Errorf("not participant")
		}
		return r.db.WithContext(ctx).Model(conv).
			Update("client_archived_at", now).Error
	case core.SenderRoleWorker:
		return r.db.WithContext(ctx).Model(conv).
			Update("worker_archived_at", now).Error
	default:
		return fmt.Errorf("invalid role: %s", role)
	}
}

func (r *GormDirectMessageRepository) BlockConversation(
	ctx context.Context, conversationID string,
) error {
	return r.db.WithContext(ctx).
		Model(&core.DirectConversation{}).
		Where("id = ?", conversationID).
		Update("status", "blocked").Error
}

// ── Messages ─────────────────────────────────────────────────────────────────

func (r *GormDirectMessageRepository) GetMessages(
	ctx context.Context, conversationID string, limit int, before string,
) ([]core.DirectMessage, error) {
	query := r.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Order("created_at DESC").
		Limit(limit)

	if before != "" {
		query = query.Where("created_at < (SELECT created_at FROM direct_messages WHERE id = ?)", before)
	}

	var msgs []core.DirectMessage
	if err := query.Find(&msgs).Error; err != nil {
		return nil, err
	}
	return msgs, nil
}

func (r *GormDirectMessageRepository) SendMessage(
	ctx context.Context, msg *core.DirectMessage,
) error {
	// Validate
	if len(msg.Body) == 0 || len(msg.Body) > core.MaxDirectMessageLength {
		return fmt.Errorf("body must be 1-%d characters", core.MaxDirectMessageLength)
	}
	if msg.SenderRole != core.SenderRoleClient && msg.SenderRole != core.SenderRoleWorker {
		return fmt.Errorf("invalid sender_role: %s", msg.SenderRole)
	}

	// Insert message
	if err := r.db.WithContext(ctx).Create(msg).Error; err != nil {
		return fmt.Errorf("insert message: %w", err)
	}

	// Update conversation: last_message_at, last_message_preview
	preview := msg.Body
	if len(preview) > 120 {
		preview = preview[:120]
	}
	updates := map[string]interface{}{
		"last_message_at":      msg.CreatedAt,
		"last_message_preview": preview,
	}

	// Increment the OTHER party's unread count
	if msg.SenderRole == core.SenderRoleClient {
		updates["worker_unread_count"] = gorm.Expr("worker_unread_count + 1")
	} else {
		updates["client_unread_count"] = gorm.Expr("client_unread_count + 1")
	}

	return r.db.WithContext(ctx).
		Model(&core.DirectConversation{}).
		Where("id = ?", msg.ConversationID).
		Updates(updates).Error
}

func (r *GormDirectMessageRepository) MarkRead(
	ctx context.Context, conversationID, readerRole string,
) (int, error) {
	// Determine which role is the OTHER party (whose messages we're marking as read)
	otherRole := core.SenderRoleClient
	if readerRole == core.SenderRoleClient {
		otherRole = core.SenderRoleWorker
	}

	now := time.Now()
	result := r.db.WithContext(ctx).
		Model(&core.DirectMessage{}).
		Where("conversation_id = ? AND sender_role = ? AND read_at IS NULL", conversationID, otherRole).
		Update("read_at", now)

	if result.Error != nil {
		return 0, result.Error
	}

	count := int(result.RowsAffected)
	if count > 0 {
		// Reset unread counter for the reader
		unreadCol := "client_unread_count"
		if readerRole == core.SenderRoleWorker {
			unreadCol = "worker_unread_count"
		}
		_ = r.db.WithContext(ctx).
			Model(&core.DirectConversation{}).
			Where("id = ?", conversationID).
			Update(unreadCol, 0).Error
	}

	return count, nil
}

func (r *GormDirectMessageRepository) PollSince(
	ctx context.Context, userID string, since time.Time,
) ([]core.DirectMessage, error) {
	// Messages where the user is a participant and created after the given time
	var msgs []core.DirectMessage
	err := r.db.WithContext(ctx).
		Model(&core.DirectMessage{}).
		Joins("JOIN direct_conversations ON direct_conversations.id = direct_messages.conversation_id").
		Where("direct_messages.created_at > ?", since).
		Where(
			"(direct_conversations.client_id = ? OR direct_conversations.worker_profile_id IN (SELECT id FROM worker_profiles WHERE user_id = ?))",
			userID, userID,
		).
		Where("direct_messages.sender_id != ?", userID). // only other party's messages
		Order("direct_messages.created_at ASC").
		Find(&msgs).Error
	if err != nil {
		return nil, err
	}
	return msgs, nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// GetWorkerByProfileID loads a worker profile by its UUID (worker_profiles.id).
func (r *GormDirectMessageRepository) GetWorkerByProfileID(
	ctx context.Context, profileID string,
) (*core.WorkerProfile, error) {
	var wp core.WorkerProfile
	err := r.db.WithContext(ctx).Where("id = ?", profileID).First(&wp).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &wp, nil
}

// IsParticipant checks whether the given user is a participant in a conversation.
func (r *GormDirectMessageRepository) IsParticipant(
	ctx context.Context, convID, userID string,
) (bool, string, error) {
	// role is "client" or "worker"
	var conv core.DirectConversation
	err := r.db.WithContext(ctx).Where("id = ?", convID).First(&conv).Error
	if err != nil {
		return false, "", err
	}
	if conv.ClientID == userID {
		return true, core.SenderRoleClient, nil
	}
	// Check if user is the worker
	var wp core.WorkerProfile
	err = r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", conv.WorkerProfileID, userID).
		First(&wp).Error
	if err == nil {
		return true, core.SenderRoleWorker, nil
	}
	return false, "", nil
}

// Ensure DirectMessageRepository interface is satisfied.
var _ ports.DirectMessageRepository = (*GormDirectMessageRepository)(nil)
