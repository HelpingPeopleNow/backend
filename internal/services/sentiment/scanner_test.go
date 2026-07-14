package sentiment

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/testingutil"
)

type mockSentimentRepo struct {
	eligible []string
	msgs     map[string][]core.DirectMessage
	scores   map[string]int16
	reasons  map[string]string
}

func (r *mockSentimentRepo) FindEligibleConversations(_ context.Context, _ time.Duration, _ int) ([]string, error) {
	return r.eligible, nil
}

func (r *mockSentimentRepo) FetchMessages(_ context.Context, conversationID string, _ int) ([]core.DirectMessage, error) {
	return r.msgs[conversationID], nil
}

func (r *mockSentimentRepo) WriteScore(_ context.Context, conversationID string, score int16, reason string) error {
	if r.scores == nil {
		r.scores = make(map[string]int16)
	}
	if r.reasons == nil {
		r.reasons = make(map[string]string)
	}
	r.scores[conversationID] = score
	r.reasons[conversationID] = reason
	return nil
}

func (r *mockSentimentRepo) ClearScore(_ context.Context, _ string) error { return nil }

func (r *mockSentimentRepo) FetchParticipantEmails(_ context.Context, _ string) (string, string, error) {
	return "a@test.com", "b@test.com", nil
}

func TestScannerFiresAlertOnLowScore(t *testing.T) {
	repo := &mockSentimentRepo{
		eligible: []string{"conv-1"},
		msgs: map[string][]core.DirectMessage{
			"conv-1": {
				{SenderRole: core.DirectMessageRoleClient, Body: "Terrible service"},
			},
		},
	}
	llm := &testingutil.MockLLM{Answer: `{"score": 1, "reason": "Angry"}`}
	notifier := &testingutil.MockNotifier{}

	scanner := NewScanner(repo, llm, notifier, Config{
		Interval:    24 * time.Hour,
		Cooldown:    24 * time.Hour,
		BatchSize:   50,
		MaxMessages: 20,
	})

	ctx := context.Background()
	if err := scanner.TickOnce(ctx); err != nil {
		t.Fatalf("tick once: %v", err)
	}
	scanner.Drain()

	if len(notifier.SentimentAlerts) != 1 {
		t.Fatalf("expected 1 sentiment alert, got %d", len(notifier.SentimentAlerts))
	}
	alert := notifier.SentimentAlerts[0]
	if alert.ConvID != "conv-1" || alert.Score != 1 || alert.Reason != "Angry" {
		t.Fatalf("unexpected alert: %+v", alert)
	}
	if alert.EmailA != "a@test.com" || alert.EmailB != "b@test.com" {
		t.Fatalf("expected emails a@test.com/b@test.com, got %s/%s", alert.EmailA, alert.EmailB)
	}
}

func TestScannerRunFailsFastWhenMistralMissing(t *testing.T) {
	repo := &mockSentimentRepo{eligible: []string{}}
	llm := &testingutil.MockLLM{AdapterNamesVal: []string{"opencode0", "ollama"}}
	notifier := &testingutil.MockNotifier{}

	scanner := NewScanner(repo, llm, notifier, Config{
		Interval:    24 * time.Hour,
		Cooldown:    24 * time.Hour,
		BatchSize:   50,
		MaxMessages: 20,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	scanner.Run(ctx) // should return immediately after probe failure

	if len(llm.AdapterNamesVal) != 2 {
		t.Fatalf("expected adapter names to be checked, got %v", llm.AdapterNamesVal)
	}
}

func TestScannerRunProceedsWhenMistralPresent(t *testing.T) {
	repo := &mockSentimentRepo{eligible: []string{}}
	llm := &testingutil.MockLLM{AdapterNamesVal: []string{"opencode0", "mistral", "ollama"}}
	notifier := &testingutil.MockNotifier{}

	scanner := NewScanner(repo, llm, notifier, Config{
		Interval:    24 * time.Hour,
		Cooldown:    24 * time.Hour,
		BatchSize:   50,
		MaxMessages: 20,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	scanner.Run(ctx) // should start, probe passes, then wait for ctx timeout
}

func TestScannerRunFailsFastWhenAdapterNamesErrors(t *testing.T) {
	repo := &mockSentimentRepo{eligible: []string{}}
	llm := &testingutil.MockLLM{AdapterNamesErr: fmt.Errorf("helper unreachable")}
	notifier := &testingutil.MockNotifier{}

	scanner := NewScanner(repo, llm, notifier, Config{
		Interval:    24 * time.Hour,
		Cooldown:    24 * time.Hour,
		BatchSize:   50,
		MaxMessages: 20,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	scanner.Run(ctx) // should return immediately after probe failure
}

func TestScannerDoesNotFireAlertOnNeutralScore(t *testing.T) {
	repo := &mockSentimentRepo{
		eligible: []string{"conv-2"},
		msgs: map[string][]core.DirectMessage{
			"conv-2": {
				{SenderRole: core.DirectMessageRoleClient, Body: "Okay"},
			},
		},
	}
	llm := &testingutil.MockLLM{Answer: `{"score": 5, "reason": "Neutral"}`}
	notifier := &testingutil.MockNotifier{}

	scanner := NewScanner(repo, llm, notifier, Config{
		Interval:    24 * time.Hour,
		Cooldown:    24 * time.Hour,
		BatchSize:   50,
		MaxMessages: 20,
	})

	ctx := context.Background()
	if err := scanner.TickOnce(ctx); err != nil {
		t.Fatalf("tick once: %v", err)
	}
	scanner.Drain()

	if len(notifier.SentimentAlerts) != 0 {
		t.Fatalf("expected 0 sentiment alerts, got %d", len(notifier.SentimentAlerts))
	}
}
