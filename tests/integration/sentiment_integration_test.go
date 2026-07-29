package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/adapters/repository"
	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/services/sentiment"
	"github.com/HelpingPeopleNow/backend/internal/testingutil"
)

func TestSentimentScannerScoresConversation(t *testing.T) {
	db := NewTestDB(t)
	ctx := context.Background()

	clientID := "client-1"
	workerID := "worker-1"

	conv := core.DirectConversation{
		UserAID:       clientID,
		UserARole:     core.DirectMessageRoleClient,
		UserBID:       workerID,
		UserBRole:     core.DirectMessageRoleWorker,
		Status:        "active",
		LastMessageAt: sentimentPtrTime(time.Now().Add(-48 * time.Hour)),
	}
	if err := db.Create(&conv).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	for i := 0; i < 5; i++ {
		role := core.DirectMessageRoleClient
		if i%2 == 1 {
			role = core.DirectMessageRoleWorker
		}
		msg := core.DirectMessage{
			ConversationID: conv.ID,
			SenderID:       clientID,
			SenderRole:     role,
			Body:           fmt.Sprintf("message %d", i),
			CreatedAt:      time.Now().Add(-time.Duration(48-i) * time.Hour),
		}
		if err := db.Create(&msg).Error; err != nil {
			t.Fatalf("create message: %v", err)
		}
	}

	llm := &testingutil.MockLLM{Answer: `{"score": 6, "reason": "Professional"}`}
	repo := repository.NewGormSentimentScannerRepository(db)
	scanner := sentiment.NewScanner(repo, llm, nil, sentiment.Config{
		Interval:    24 * time.Hour,
		Cooldown:    24 * time.Hour,
		BatchSize:   50,
		MaxMessages: 20,
	})

	if err := scanner.TickOnce(ctx); err != nil {
		t.Fatalf("tick once: %v", err)
	}
	scanner.Drain()

	var updated core.DirectConversation
	if err := db.Where("id = ?", conv.ID).First(&updated).Error; err != nil {
		t.Fatalf("fetch conversation: %v", err)
	}

	if updated.SentimentScore == nil || *updated.SentimentScore != 6 {
		t.Fatalf("expected sentiment_score=6, got %v", updated.SentimentScore)
	}
	if updated.SentimentReason == nil || *updated.SentimentReason != "Professional" {
		t.Fatalf("expected sentiment_reason=Professional, got %v", updated.SentimentReason)
	}
	if updated.SentimentScoredAt == nil {
		t.Fatalf("expected sentiment_scored_at to be set")
	}
}

func TestSentimentScoreClearedOnNewMessage(t *testing.T) {
	db := NewTestDB(t)
	ctx := context.Background()

	clientID := "client-2"
	workerID := "worker-2"

	conv := core.DirectConversation{
		UserAID:           clientID,
		UserARole:         core.DirectMessageRoleClient,
		UserBID:           workerID,
		UserBRole:         core.DirectMessageRoleWorker,
		Status:            "active",
		LastMessageAt:     sentimentPtrTime(time.Now().Add(-48 * time.Hour)),
		SentimentScore:    sentimentPtrInt16(6),
		SentimentReason:   sentimentPtrString("Professional"),
		SentimentScoredAt: sentimentPtrTime(time.Now().Add(-49 * time.Hour)),
	}
	if err := db.Create(&conv).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	dmRepo := repository.NewGormDirectMessageRepository(db)
	msg := &core.DirectMessage{
		ConversationID: conv.ID,
		SenderID:       clientID,
		SenderRole:     core.DirectMessageRoleClient,
		Body:           "New message",
		CreatedAt:      time.Now(),
	}
	if err := dmRepo.SendMessage(ctx, msg); err != nil {
		t.Fatalf("send message: %v", err)
	}

	var updated core.DirectConversation
	if err := db.Where("id = ?", conv.ID).First(&updated).Error; err != nil {
		t.Fatalf("fetch conversation: %v", err)
	}

	if updated.SentimentScore != nil {
		t.Fatalf("expected sentiment_score to be cleared, got %v", updated.SentimentScore)
	}
	if updated.SentimentReason != nil {
		t.Fatalf("expected sentiment_reason to be cleared, got %v", updated.SentimentReason)
	}
	if updated.SentimentScoredAt != nil {
		t.Fatalf("expected sentiment_scored_at to be cleared, got %v", updated.SentimentScoredAt)
	}
}

func TestSentimentPreservesScoreOnLLMError(t *testing.T) {
	db := NewTestDB(t)
	ctx := context.Background()

	clientID := "client-3"
	workerID := "worker-3"

	conv := core.DirectConversation{
		UserAID:           clientID,
		UserARole:         core.DirectMessageRoleClient,
		UserBID:           workerID,
		UserBRole:         core.DirectMessageRoleWorker,
		Status:            "active",
		LastMessageAt:     sentimentPtrTime(time.Now().Add(-48 * time.Hour)),
		SentimentScore:    sentimentPtrInt16(8),
		SentimentReason:   sentimentPtrString("Good"),
		SentimentScoredAt: sentimentPtrTime(time.Now().Add(-49 * time.Hour)),
	}
	if err := db.Create(&conv).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	msg := core.DirectMessage{
		ConversationID: conv.ID,
		SenderID:       clientID,
		SenderRole:     core.DirectMessageRoleClient,
		Body:           "Hello",
		CreatedAt:      time.Now().Add(-47 * time.Hour),
	}
	if err := db.Create(&msg).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}

	llm := &testingutil.MockLLM{AskErr: fmt.Errorf("simulated LLM failure")}
	repo := repository.NewGormSentimentScannerRepository(db)
	scanner := sentiment.NewScanner(repo, llm, nil, sentiment.Config{
		Interval:    24 * time.Hour,
		Cooldown:    24 * time.Hour,
		BatchSize:   50,
		MaxMessages: 20,
	})

	if err := scanner.TickOnce(ctx); err != nil {
		t.Fatalf("tick once: %v", err)
	}
	scanner.Drain()

	var updated core.DirectConversation
	if err := db.Where("id = ?", conv.ID).First(&updated).Error; err != nil {
		t.Fatalf("fetch conversation: %v", err)
	}

	if updated.SentimentScore == nil || *updated.SentimentScore != 8 {
		t.Fatalf("expected sentiment_score to remain 8, got %v", updated.SentimentScore)
	}
}

// TestSentimentSkipsStaleConversation verifies that a conversation whose
// last_message_at is older than sentiment_scored_at (i.e. no new messages
// since the last score) is NOT re-eligible for scoring. Without this guard,
// the scanner would re-score the same transcript every cooldown window and
// re-fire low-sentiment alerts indefinitely.
func TestSentimentSkipsStaleConversation(t *testing.T) {
	db := NewTestDB(t)
	ctx := context.Background()

	clientID := "client-stale"
	workerID := "worker-stale"

	// Conversation: last message 48h ago, but sentiment was already scored 25h ago.
	// Without the new guard, the cooldown check (scored_at older than 24h) would
	// make it eligible. With the guard, last_message_at > sentiment_scored_at
	// fails, so it's skipped.
	conv := core.DirectConversation{
		UserAID:           clientID,
		UserARole:         core.DirectMessageRoleClient,
		UserBID:           workerID,
		UserBRole:         core.DirectMessageRoleWorker,
		Status:            "active",
		LastMessageAt:     sentimentPtrTime(time.Now().Add(-48 * time.Hour)),
		SentimentScore:    sentimentPtrInt16(3),
		SentimentReason:   sentimentPtrString("Tense"),
		SentimentScoredAt: sentimentPtrTime(time.Now().Add(-25 * time.Hour)),
	}
	if err := db.Create(&conv).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	repo := repository.NewGormSentimentScannerRepository(db)
	ids, err := repo.FindEligibleConversations(ctx, 24*time.Hour, 50)
	if err != nil {
		t.Fatalf("find eligible: %v", err)
	}
	for _, id := range ids {
		if id == conv.ID {
			t.Fatalf("stale conversation should NOT be eligible for re-scoring (last_message_at older than sentiment_scored_at)")
		}
	}
}

func sentimentPtrTime(t time.Time) *time.Time { return &t }
func sentimentPtrInt16(i int16) *int16        { return &i }
func sentimentPtrString(s string) *string     { return &s }
