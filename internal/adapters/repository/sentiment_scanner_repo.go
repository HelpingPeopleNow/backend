package repository

import (
	"context"
	"fmt"
	"log/slog"
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
//   - last_message_at is newer than sentiment_scored_at (only rescore when there
//     is something new to evaluate; otherwise stale conversations re-trigger
//     alerts indefinitely with the same transcript)
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
		Where("sentiment_scored_at IS NULL OR last_message_at > sentiment_scored_at").
		Order("last_message_at ASC").
		Limit(limit).
		Pluck("id", &ids).Error
	if err != nil {
		slog.Error("sentiment-repo: find eligible conversations failed", "error", err)
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
		slog.Error("sentiment-repo: fetch messages failed", "conv_id", conversationID, "error", err)
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
		slog.Error("sentiment-repo: write score failed", "conv_id", conversationID, "score", score, "error", err)
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
		slog.Error("sentiment-repo: clear score failed", "conv_id", conversationID, "error", err)
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
		slog.Error("sentiment-repo: fetch conversation for emails failed", "conv_id", conversationID, "error", err)
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
		slog.Error("sentiment-repo: fetch participant emails failed", "conv_id", conversationID, "error", err)
		return "", "", fmt.Errorf("fetch emails: %w", err)
	}

	emailMap := make(map[string]string, len(users))
	for _, u := range users {
		emailMap[u.ID] = u.Email
	}

	return emailMap[conv.UserAID], emailMap[conv.UserBID], nil
}

// FetchLastAlertSentAt returns the last time an alert was sent for this conversation.
func (r *GormSentimentScannerRepository) FetchLastAlertSentAt(ctx context.Context, conversationID string) (*time.Time, error) {
	var lastAlert *time.Time
	err := r.db.WithContext(ctx).
		Model(&core.DirectConversation{}).
		Select("last_alert_sent_at").
		Where("id = ?", conversationID).
		Scan(&lastAlert).Error
	if err != nil {
		slog.Error("sentiment-repo: fetch last alert sent failed", "conv_id", conversationID, "error", err)
		return nil, fmt.Errorf("fetch last alert sent: %w", err)
	}
	return lastAlert, nil
}

// MarkAlertSent records that an alert was sent for this conversation.
func (r *GormSentimentScannerRepository) MarkAlertSent(ctx context.Context, conversationID string) error {
	err := r.db.WithContext(ctx).
		Model(&core.DirectConversation{}).
		Where("id = ?", conversationID).
		Update("last_alert_sent_at", time.Now()).Error
	if err != nil {
		slog.Error("sentiment-repo: mark alert sent failed", "conv_id", conversationID, "error", err)
		return fmt.Errorf("mark alert sent: %w", err)
	}
	return nil
}

// ClaimAlert performs an atomic CAS on alert_claim_at so exactly one
// concurrent caller wins the right to dispatch a sentiment alert for
// this conversation. SPOF GAP C fix — see
// infra/docs/FOLLOW_UP_SPOF_Backup_Replicas.md.
//
// The CAS predicate is "row is unclaimed OR its current claim is older
// than `lease` ago" so a crashed replica's stale claim auto-recovers
// after the lease window without operator intervention.
//
// IMPORTANT: This is the FIRST call in the dispatch path — callers must
// dispatch only when claimed=true, then MarkAlertSent on success or
// ReleaseAlertClaim on send failure. Reusing last_alert_sent_at as the
// claim field was rejected because it would lose alerts on send
// failure (see the GAP C note in FOLLOW_UP_SPOF_Backup_Replicas.md).
func (r *GormSentimentScannerRepository) ClaimAlert(ctx context.Context, conversationID string, lease time.Duration) (bool, error) {
	if lease <= 0 {
		lease = 60 * time.Second
	}
	res := r.db.WithContext(ctx).
		Model(&core.DirectConversation{}).
		Where("id = ?", conversationID).
		Where("alert_claim_at IS NULL OR alert_claim_at < NOW() - (? * INTERVAL '1 second')", lease.Seconds()).
		Update("alert_claim_at", time.Now())
	if res.Error != nil {
		slog.Error("sentiment-repo: claim alert failed", "conv_id", conversationID, "error", res.Error)
		return false, fmt.Errorf("claim alert: %w", res.Error)
	}
	return res.RowsAffected == 1, nil
}

// ReleaseAlertClaim clears alert_claim_at for a conversation whose
// dispatch failed. Called after a SendSentimentAlert error so the next
// scanner tick can re-claim and retry the send (the long-form cooldown
// in last_alert_sent_at still applies, so an infinitely-failing
// conversation does NOT spam alerts at the SENTIMENT_SCANNER_INTERVAL
// rate — it caps at one attempt per cooldown).
func (r *GormSentimentScannerRepository) ReleaseAlertClaim(ctx context.Context, conversationID string) error {
	res := r.db.WithContext(ctx).
		Model(&core.DirectConversation{}).
		Where("id = ?", conversationID).
		Update("alert_claim_at", nil)
	if res.Error != nil {
		slog.Error("sentiment-repo: release alert claim failed", "conv_id", conversationID, "error", res.Error)
		return fmt.Errorf("release alert claim: %w", res.Error)
	}
	return nil
}
