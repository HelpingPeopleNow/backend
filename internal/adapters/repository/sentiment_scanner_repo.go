package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
	"gorm.io/gorm"
)

// GormSentimentScannerRepository implements ports.SentimentScannerRepository.
type GormSentimentScannerRepository struct {
	db *gorm.DB
}

// NewGormSentimentScannerRepository creates a new sentiment scanner repository.
func NewGormSentimentScannerRepository(db *gorm.DB) ports.SentimentScannerRepository {
	return &GormSentimentScannerRepository{db: db}
}

// FindEligibleConversations returns IDs of conversations due for scoring.
// A conversation is eligible when:
//   - status = 'active'
//   - last_message_at is older than 24 hours
//   - sentiment_scored_at is NULL or older than the cooldown
func (r *GormSentimentScannerRepository) FindEligibleConversations(ctx context.Context, cooldown time.Duration, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 50
	}

	var ids []string
	err := r.db.WithContext(ctx).
		Model(&core.DirectConversation{}).
		Select("id").
		Where("status = ?", "active").
		Where("last_message_at < NOW() - INTERVAL '24 hours'").
		Where("sentiment_scored_at IS NULL OR sentiment_scored_at < NOW() - (? * INTERVAL '1 second')", cooldown.Seconds()).
		Order("last_message_at ASC").
		Limit(limit).
		Pluck("id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("find eligible conversations: %w", err)
	}
	return ids, nil
}

// FetchMessages returns the most recent messages for a conversation, oldest first.
func (r *GormSentimentScannerRepository) FetchMessages(ctx context.Context, conversationID string, max int) ([]core.DirectMessage, error) {
	if max <= 0 {
		max = 20
	}

	var msgs []core.DirectMessage
	err := r.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Order("created_at DESC").
		Limit(max).
		Find(&msgs).Error
	if err != nil {
		return nil, fmt.Errorf("fetch messages: %w", err)
	}

	// Reverse so the transcript is oldest-first.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

// WriteScore persists the sentiment score and reason for a conversation.
func (r *GormSentimentScannerRepository) WriteScore(ctx context.Context, conversationID string, score int16, reason string) error {
	err := r.db.WithContext(ctx).
		Model(&core.DirectConversation{}).
		Where("id = ?", conversationID).
		Updates(map[string]interface{}{
			"sentiment_score":     score,
			"sentiment_reason":    reason,
			"sentiment_scored_at": time.Now(),
		}).Error
	if err != nil {
		return fmt.Errorf("write score: %w", err)
	}
	return nil
}

// ClearScore clears any previously stored sentiment score.
func (r *GormSentimentScannerRepository) ClearScore(ctx context.Context, conversationID string) error {
	err := r.db.WithContext(ctx).
		Model(&core.DirectConversation{}).
		Where("id = ?", conversationID).
		Updates(map[string]interface{}{
			"sentiment_score":     nil,
			"sentiment_reason":    nil,
			"sentiment_scored_at": nil,
		}).Error
	if err != nil {
		return fmt.Errorf("clear score: %w", err)
	}
	return nil
}

// FetchParticipantEmails returns the email addresses of both participants.
func (r *GormSentimentScannerRepository) FetchParticipantEmails(ctx context.Context, conversationID string) (string, string, error) {
	var conv core.DirectConversation
	if err := r.db.WithContext(ctx).
		Select("user_a_id", "user_b_id").
		Where("id = ?", conversationID).
		First(&conv).Error; err != nil {
		return "", "", fmt.Errorf("fetch conversation: %w", err)
	}

	type userRow struct {
		ID    string `gorm:"column:id"`
		Email string `gorm:"column:email"`
	}

	var users []userRow
	if err := r.db.WithContext(ctx).
		Table(`"user"`).
		Select("id", "email").
		Where("id IN ?", []string{conv.UserAID, conv.UserBID}).
		Find(&users).Error; err != nil {
		return "", "", fmt.Errorf("fetch emails: %w", err)
	}

	emailMap := make(map[string]string, len(users))
	for _, u := range users {
		emailMap[u.ID] = u.Email
	}

	return emailMap[conv.UserAID], emailMap[conv.UserBID], nil
}
