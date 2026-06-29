package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

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

// sortUserIDs returns the two IDs in sorted order (user_a_id < user_b_id).
func sortUserIDs(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

// ── Conversations ────────────────────────────────────────────────────────────

func (r *GormDirectMessageRepository) GetOrCreateConversation(
	ctx context.Context, userID1, userID2 string,
) (*core.DirectConversation, bool, error) {
	a, b := sortUserIDs(userID1, userID2)

	var conv core.DirectConversation
	err := r.db.WithContext(ctx).
		Where("user_a_id = ? AND user_b_id = ?", a, b).
		First(&conv).Error
	if err == nil {
		// Un-archive for the caller if archived
		if userID1 == a && conv.UserAArchivedAt != nil {
			_ = r.db.WithContext(ctx).Model(&conv).
				Update("user_a_archived_at", nil).Error
			conv.UserAArchivedAt = nil
		} else if userID1 == b && conv.UserBArchivedAt != nil {
			_ = r.db.WithContext(ctx).Model(&conv).
				Update("user_b_archived_at", nil).Error
			conv.UserBArchivedAt = nil
		}
		return &conv, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, fmt.Errorf("find conversation: %w", err)
	}

	conv = core.DirectConversation{
		UserAID: a,
		UserBID: b,
		Status:  "active",
	}
	if err := r.db.WithContext(ctx).Create(&conv).Error; err != nil {
		return nil, false, fmt.Errorf("create conversation: %w", err)
	}
	slog.Info("dm: conversation created",
		"conv_id", conv.ID, "user_a", a, "user_b", b)
	return &conv, true, nil
}

func (r *GormDirectMessageRepository) GetConversation(
	ctx context.Context, conversationID string,
) (*core.DirectConversation, error) {
	var conv core.DirectConversation
	err := r.db.WithContext(ctx).
		Where("id = ?", conversationID).
		First(&conv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get conversation: %w", err)
	}
	return &conv, nil
}

func (r *GormDirectMessageRepository) ListConversations(
	ctx context.Context, userID string, status string, limit int, before *time.Time,
) ([]core.DirectConversation, error) {
	if status == "" {
		status = "active"
	}

	q := r.db.WithContext(ctx).
		Where("(user_a_id = ? OR user_b_id = ?) AND status = ?", userID, userID, status)

	if before != nil {
		q = q.Where("last_message_at < ?", before)
	}

	var convs []core.DirectConversation
	if err := q.
		Order("last_message_at DESC NULLS LAST").
		Limit(limit).
		Find(&convs).Error; err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	return convs, nil
}

func (r *GormDirectMessageRepository) ArchiveConversation(
	ctx context.Context, conversationID, userID string,
) error {
	field := "user_a_archived_at"
	// Determine which side the user is on
	var count int64
	r.db.WithContext(ctx).Model(&core.DirectConversation{}).
		Where("id = ? AND user_b_id = ?", conversationID, userID).
		Count(&count)
	if count > 0 {
		field = "user_b_archived_at"
	}

	result := r.db.WithContext(ctx).Model(&core.DirectConversation{}).
		Where("id = ? AND (user_a_id = ? OR user_b_id = ?)", conversationID, userID, userID).
		Update(field, time.Now())
	if result.Error != nil {
		return fmt.Errorf("archive conversation: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("conversation not found or not a participant")
	}
	return nil
}

func (r *GormDirectMessageRepository) BlockConversation(
	ctx context.Context, conversationID string,
) error {
	result := r.db.WithContext(ctx).Model(&core.DirectConversation{}).
		Where("id = ?", conversationID).
		Update("status", "blocked")
	if result.Error != nil {
		return fmt.Errorf("block conversation: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("conversation not found")
	}
	return nil
}

// ── Messages ─────────────────────────────────────────────────────────────────

func (r *GormDirectMessageRepository) GetMessages(
	ctx context.Context, conversationID string, limit int, before string,
) ([]core.DirectMessage, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	q := r.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID)
	if before != "" {
		q = q.Where("id < (SELECT created_at FROM direct_messages WHERE id = ?)", before)
	}

	var msgs []core.DirectMessage
	if err := q.
		Order("created_at DESC").
		Limit(limit).
		Find(&msgs).Error; err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	return msgs, nil
}

func (r *GormDirectMessageRepository) SendMessage(
	ctx context.Context, msg *core.DirectMessage,
) error {
	if len(msg.Body) == 0 || len(msg.Body) > core.MaxDirectMessageLength {
		return fmt.Errorf("body must be 1-%d characters", core.MaxDirectMessageLength)
	}

	// Determine which unread count to increment (the other party's)
	var conv core.DirectConversation
	if err := r.db.WithContext(ctx).
		Where("id = ?", msg.ConversationID).
		First(&conv).Error; err != nil {
		return fmt.Errorf("conversation not found: %w", err)
	}

	unreadField := "user_a_unread_count"
	if msg.SenderID == conv.UserAID {
		unreadField = "user_b_unread_count"
	}

	tx := r.db.WithContext(ctx).Begin()

	// Create the message
	if err := tx.Create(msg).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("create message: %w", err)
	}

	// Update conversation metadata
	preview := msg.Body
	if utf8.RuneCountInString(preview) > 120 {
		preview = string([]rune(preview)[:120])
	}
	if err := tx.Model(&core.DirectConversation{}).
		Where("id = ?", msg.ConversationID).
		Updates(map[string]interface{}{
			"last_message_at":      msg.CreatedAt,
			"last_message_preview": preview,
			unreadField:            gorm.Expr("GREATEST(0, " + unreadField + " + 1)"),
		}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("update conversation: %w", err)
	}

	return tx.Commit().Error
}

func (r *GormDirectMessageRepository) MarkRead(
	ctx context.Context, conversationID, userID string,
) (int, error) {
	var conv core.DirectConversation
	if err := r.db.WithContext(ctx).
		Where("id = ?", conversationID).
		First(&conv).Error; err != nil {
		return 0, fmt.Errorf("conversation not found: %w", err)
	}

	// The other party is the one who sent the unread messages
	otherID := conv.UserAID
	if otherID == userID {
		otherID = conv.UserBID
	}

	// Mark messages from the other party as read
	result := r.db.WithContext(ctx).Model(&core.DirectMessage{}).
		Where("conversation_id = ? AND sender_id = ? AND read_at IS NULL", conversationID, otherID).
		Update("read_at", time.Now())

	if result.Error != nil {
		return 0, fmt.Errorf("mark read: %w", result.Error)
	}

	// Reset the caller's unread count
	unreadField := "user_a_unread_count"
	if conv.UserBID == userID {
		unreadField = "user_b_unread_count"
	}
	if err := r.db.WithContext(ctx).Model(&core.DirectConversation{}).
		Where("id = ?", conversationID).
		Update(unreadField, 0).Error; err != nil {
		return 0, fmt.Errorf("reset unread count: %w", err)
	}

	return int(result.RowsAffected), nil
}

func (r *GormDirectMessageRepository) PollSince(
	ctx context.Context, userID string, since time.Time,
) ([]core.DirectMessage, error) {
	var msgs []core.DirectMessage
	err := r.db.WithContext(ctx).
		Joins("JOIN direct_conversations ON direct_conversations.id = direct_messages.conversation_id").
		Where("(direct_conversations.user_a_id = ? OR direct_conversations.user_b_id = ?)", userID, userID).
		Where("direct_messages.sender_id != ?", userID).
		Where("direct_messages.created_at > ?", since).
		Order("direct_messages.created_at ASC").
		Find(&msgs).Error
	if err != nil {
		return nil, fmt.Errorf("poll since: %w", err)
	}
	return msgs, nil
}

func (r *GormDirectMessageRepository) IsParticipant(
	ctx context.Context, convID, userID string,
) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&core.DirectConversation{}).
		Where("id = ? AND (user_a_id = ? OR user_b_id = ?)", convID, userID, userID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("check participant: %w", err)
	}
	return count > 0, nil
}

// ── Reports ──────────────────────────────────────────────────────────────────

func (r *GormDirectMessageRepository) CreateReport(
	ctx context.Context, report *core.DirectMessageReport,
) error {
	if err := r.db.WithContext(ctx).Create(report).Error; err != nil {
		return fmt.Errorf("create report: %w", err)
	}
	slog.Warn("dm: report created",
		"report_id", report.ID,
		"conv_id", report.ConversationID,
		"reported_by", report.ReportedBy,
		"reason", report.Reason,
	)
	return nil
}

func (r *GormDirectMessageRepository) ListReports(
	ctx context.Context,
) ([]core.DirectMessageReport, error) {
	var reports []core.DirectMessageReport
	if err := r.db.WithContext(ctx).
		Order("created_at DESC").
		Limit(100).
		Find(&reports).Error; err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	return reports, nil
}
