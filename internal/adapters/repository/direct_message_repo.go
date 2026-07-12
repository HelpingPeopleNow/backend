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

// sortUserIDs returns the two IDs AND their corresponding roles in sorted
// order (user_a_id < user_b_id). Roles follow their owning user so the
// caller can pass (userID1, role1, userID2, role2) in any order and the
// repo persists (user_a_id, user_a_role, user_b_id, user_b_role) sorted.
//
// Audit: this signature is the smallest possible refactor that preserves
// the "user_a < user_b" invariant while threading roles through.
func sortUserIDs(userID1, role1, userID2, role2 string) (a, aRole, b, bRole string) {
	if userID1 < userID2 {
		return userID1, role1, userID2, role2
	}
	return userID2, role2, userID1, role1
}

// ── Conversations ────────────────────────────────────────────────────────────

func (r *GormDirectMessageRepository) GetOrCreateConversation(
	ctx context.Context, userID1, userARole, userID2, userBRole string,
) (*core.DirectConversation, bool, error) {
	a, aRole, b, bRole := sortUserIDs(userID1, userARole, userID2, userBRole)
	if aRole == "" {
		aRole = core.DirectMessageRoleUser
	}
	if bRole == "" {
		bRole = core.DirectMessageRoleUser
	}

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
		UserAID:   a,
		UserARole: aRole,
		UserBID:   b,
		UserBRole: bRole,
		Status:    "active",
	}
	if err := r.db.WithContext(ctx).Create(&conv).Error; err != nil {
		return nil, false, fmt.Errorf("create conversation: %w", err)
	}
	slog.Info("dm: conversation created",
		"conv_id", conv.ID, "user_a", a, "user_b", b,
		"user_a_role", aRole, "user_b_role", bRole)
	return &conv, true, nil
}

// UpdateConversationRoles patches (user_a_role, user_b_role) on an existing
// conversation. No-op on miss (returns nil, no error). Audit: lets operators
// re-classify participants without touching messages.
func (r *GormDirectMessageRepository) UpdateConversationRoles(
	ctx context.Context, conversationID, userARole, userBRole string,
) error {
	if userARole == "" {
		userARole = core.DirectMessageRoleUser
	}
	if userBRole == "" {
		userBRole = core.DirectMessageRoleUser
	}
	res := r.db.WithContext(ctx).Model(&core.DirectConversation{}).
		Where("id = ?", conversationID).
		Updates(map[string]interface{}{
			"user_a_role": userARole,
			"user_b_role": userBRole,
		})
	if res.Error != nil {
		return fmt.Errorf("update conversation roles: %w", res.Error)
	}
	return nil
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

// SendMessage persists a message and updates the conversation's
// last_message_* fields. Audit (sender_role denormalization):
//
//	msg.SenderRole MUST be set by the handler from conv.SenderRole(userID)
//	before this call. We refuse the insert at the repo boundary so a
//	caller bug doesn't translate to a 500 NOT NULL violation.
//
// The transaction uses the defer+committed-flag pattern (idiomatic Go)
// so a panic between Begin and Commit will still roll back; Commit
// followed by Rollback is a no-op in GORM.
func (r *GormDirectMessageRepository) SendMessage(
	ctx context.Context, msg *core.DirectMessage,
) error {
	if len(msg.Body) == 0 || len(msg.Body) > core.MaxDirectMessageLength {
		return fmt.Errorf("body must be 1-%d characters", core.MaxDirectMessageLength)
	}

	if msg.SenderRole == "" {
		return fmt.Errorf("sender_role required (handler must populate from conv.SenderRole(userID)); conv_id=%s", msg.ConversationID)
	}

	// Determine which unread count to increment (the other party's)
	var conv core.DirectConversation
	if err := r.db.WithContext(ctx).
		Where("id = ?", msg.ConversationID).
		First(&conv).Error; err != nil {
		slog.Warn("dm: conversation not found for message", "conversation_id", msg.ConversationID, "error", err)
		return fmt.Errorf("conversation not found: %w", err)
	}

	unreadField := "user_a_unread_count"
	if msg.SenderID == conv.UserAID {
		unreadField = "user_b_unread_count"
	}

	tx := r.db.WithContext(ctx).Begin()
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := tx.Create(msg).Error; err != nil {
		slog.Warn("dm: create message failed", "conversation_id", msg.ConversationID, "error", err)
		return fmt.Errorf("create message: %w", err)
	}

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
		slog.Warn("dm: update conversation failed", "conversation_id", msg.ConversationID, "error", err)
		return fmt.Errorf("update conversation: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return err
	}
	committed = true
	return nil
}

func (r *GormDirectMessageRepository) MarkRead(
	ctx context.Context, conversationID, userID string,
) (int, error) {
	var conv core.DirectConversation
	if err := r.db.WithContext(ctx).
		Where("id = ?", conversationID).
		First(&conv).Error; err != nil {
		slog.Warn("dm: mark read conversation not found", "conversation_id", conversationID, "error", err)
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
		slog.Warn("dm: mark read failed", "conversation_id", conversationID, "user_id", userID, "error", result.Error)
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
		slog.Warn("dm: reset unread count failed", "conversation_id", conversationID, "user_id", userID, "error", err)
		return 0, fmt.Errorf("reset unread count: %w", err)
	}

	return int(result.RowsAffected), nil
}

func (r *GormDirectMessageRepository) PollSince(
	ctx context.Context, userID string, since time.Time,
) ([]core.DirectMessage, error) {
	joinsQuery := r.db.WithContext(ctx).
		Joins("JOIN direct_conversations ON direct_conversations.id = direct_messages.conversation_id").
		Where("(direct_conversations.user_a_id = ? OR direct_conversations.user_b_id = ?)", userID, userID).
		Where("direct_messages.sender_id != ?", userID).
		Where("direct_messages.created_at > ?", since).
		Order("direct_messages.created_at ASC")
	var msgs []core.DirectMessage
	if err := joinsQuery.Find(&msgs).Error; err != nil {
		slog.Warn("dm: poll since failed", "user_id", userID, "since", since, "error", err)
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
		slog.Warn("dm: check participant failed", "conv_id", convID, "user_id", userID, "error", err)
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
