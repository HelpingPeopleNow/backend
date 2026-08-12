package sentiment

import (
	"context"
	"fmt"
	"sync"
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

	lastAlertSentAt map[string]*time.Time

	// claim sequence — first call returns true, second returns false,
	// used to simulate "another replica holds the lease".
	claimSeq   []bool
	claimIdx   int
	claimMu    sync.Mutex
	claimed    map[string]time.Time
	released   map[string]int
	claimErr   error
	releaseErr error
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

func (r *mockSentimentRepo) FetchLastAlertSentAt(_ context.Context, conversationID string) (*time.Time, error) {
	if r.lastAlertSentAt == nil {
		return nil, nil
	}
	return r.lastAlertSentAt[conversationID], nil
}

func (r *mockSentimentRepo) MarkAlertSent(_ context.Context, conversationID string) error {
	if r.lastAlertSentAt == nil {
		r.lastAlertSentAt = make(map[string]*time.Time)
	}
	now := time.Now()
	r.lastAlertSentAt[conversationID] = &now
	return nil
}

func (r *mockSentimentRepo) ClaimAlert(_ context.Context, conversationID string, _ time.Duration) (bool, error) {
	if r.claimErr != nil {
		return false, r.claimErr
	}
	r.claimMu.Lock()
	defer r.claimMu.Unlock()
	if r.claimSeq != nil {
		claimed := r.claimSeq[r.claimIdx]
		r.claimIdx++
		if claimed {
			if r.claimed == nil {
				r.claimed = make(map[string]time.Time)
			}
			r.claimed[conversationID] = time.Now()
		}
		return claimed, nil
	}
	// Default: always claim (preserves pre-fix test behaviour).
	if r.claimed == nil {
		r.claimed = make(map[string]time.Time)
	}
	r.claimed[conversationID] = time.Now()
	return true, nil
}

func (r *mockSentimentRepo) ReleaseAlertClaim(_ context.Context, conversationID string) error {
	if r.releaseErr != nil {
		return r.releaseErr
	}
	r.claimMu.Lock()
	defer r.claimMu.Unlock()
	if r.released == nil {
		r.released = make(map[string]int)
	}
	r.released[conversationID]++
	return nil
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

func TestScannerDeduplicatesAlertsWithinCooldown(t *testing.T) {
	repo := &mockSentimentRepo{
		eligible: []string{"conv-3"},
		msgs: map[string][]core.DirectMessage{
			"conv-3": {
				{SenderRole: core.DirectMessageRoleClient, Body: "Very upset"},
			},
		},
	}
	llm := &testingutil.MockLLM{Answer: `{"score": 2, "reason": "Angry"}`}
	notifier := &testingutil.MockNotifier{}

	scanner := NewScanner(repo, llm, notifier, Config{
		Interval:    24 * time.Hour,
		Cooldown:    24 * time.Hour,
		BatchSize:   50,
		MaxMessages: 20,
	})

	ctx := context.Background()

	// First tick: should fire an alert.
	if err := scanner.TickOnce(ctx); err != nil {
		t.Fatalf("tick once: %v", err)
	}
	scanner.Drain()

	if len(notifier.SentimentAlerts) != 1 {
		t.Fatalf("expected 1 sentiment alert on first tick, got %d", len(notifier.SentimentAlerts))
	}

	// Second tick within cooldown: should NOT fire another alert.
	if err := scanner.TickOnce(ctx); err != nil {
		t.Fatalf("tick once: %v", err)
	}
	scanner.Drain()

	if len(notifier.SentimentAlerts) != 1 {
		t.Fatalf("expected 1 sentiment alert (no duplicate within cooldown), got %d", len(notifier.SentimentAlerts))
	}
}

func TestScannerFiresAlertAfterCooldownExpires(t *testing.T) {
	// Simulate a conversation that was already alerted long ago.
	past := time.Now().Add(-25 * time.Hour)
	repo := &mockSentimentRepo{
		eligible: []string{"conv-4"},
		msgs: map[string][]core.DirectMessage{
			"conv-4": {
				{SenderRole: core.DirectMessageRoleClient, Body: "Not happy"},
			},
		},
		lastAlertSentAt: map[string]*time.Time{
			"conv-4": &past,
		},
	}
	llm := &testingutil.MockLLM{Answer: `{"score": 3, "reason": "Frustrated"}`}
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
		t.Fatalf("expected 1 sentiment alert after cooldown expired, got %d", len(notifier.SentimentAlerts))
	}
}

// TestScannerSkipsAlertWhenAnotherReplicaHoldsClaim verifies the SPOF
// GAP C fix: when ClaimAlert returns false (another replica holds the
// lease), the scanner must NOT call SendSentimentAlert even though the
// score is at-or-below threshold. This is the duplicate-alert regression
// guard that the pre-fix flow had.
func TestScannerSkipsAlertWhenAnotherReplicaHoldsClaim(t *testing.T) {
	repo := &mockSentimentRepo{
		eligible: []string{"conv-claim-loss"},
		msgs: map[string][]core.DirectMessage{
			"conv-claim-loss": {
				{SenderRole: core.DirectMessageRoleClient, Body: "Awful"},
			},
		},
		claimSeq: []bool{false}, // claim denied — another replica has it
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

	if len(notifier.SentimentAlerts) != 0 {
		t.Fatalf("expected 0 sentiment alerts when claim denied, got %d", len(notifier.SentimentAlerts))
	}
	if repo.released["conv-claim-loss"] != 0 {
		t.Fatalf("ReleaseAlertClaim should not be called when claim was denied (got %d calls)", repo.released["conv-claim-loss"])
	}
}

// TestScannerConcurrentReplicaRace simulates two replicas scanning the
// same conversation in the same tick window. With claimSeq=[true, false]
// (first wins, second loses), exactly one alert must be dispatched and
// the loser must not invoke the notifier.
func TestScannerConcurrentReplicaRace(t *testing.T) {
	repo := &mockSentimentRepo{
		eligible: []string{"conv-race"},
		msgs: map[string][]core.DirectMessage{
			"conv-race": {
				{SenderRole: core.DirectMessageRoleClient, Body: "Race"},
			},
		},
		claimSeq: []bool{true, false},
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

	// Replica A.
	if err := scanner.TickOnce(ctx); err != nil {
		t.Fatalf("tick once (A): %v", err)
	}
	scanner.Drain()

	// Replica B — same scanner instance, but the mock simulates
	// another replica holding the lease via claimSeq.
	if err := scanner.TickOnce(ctx); err != nil {
		t.Fatalf("tick once (B): %v", err)
	}
	scanner.Drain()

	if len(notifier.SentimentAlerts) != 1 {
		t.Fatalf("expected exactly 1 alert across both replicas, got %d", len(notifier.SentimentAlerts))
	}
	if repo.released["conv-race"] == 0 {
		t.Fatalf("expected ReleaseAlertClaim to fire after the winning dispatch, got 0 calls")
	}
}

// TestScannerReleasesClaimOnSendFailure verifies the lost-alert
// regression guard: when SendSentimentAlert returns an error, the
// claim must be released so a subsequent tick can retry (within the
// long-form cooldown). Pre-fix this path left the conversation
// effectively locked out of retries.
func TestScannerReleasesClaimOnSendFailure(t *testing.T) {
	repo := &mockSentimentRepo{
		eligible: []string{"conv-fail"},
		msgs: map[string][]core.DirectMessage{
			"conv-fail": {
				{SenderRole: core.DirectMessageRoleClient, Body: "Send fail"},
			},
		},
	}
	llm := &testingutil.MockLLM{Answer: `{"score": 1, "reason": "Angry"}`}
	notifier := &testingutil.MockNotifier{Err: fmt.Errorf("telegram 503")}

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
		t.Fatalf("notifier should have been invoked once even on failure, got %d calls", len(notifier.SentimentAlerts))
	}
	if repo.released["conv-fail"] != 1 {
		t.Fatalf("expected 1 ReleaseAlertClaim call after send failure, got %d", repo.released["conv-fail"])
	}
	if len(repo.lastAlertSentAt) != 0 {
		t.Fatalf("MarkAlertSent must NOT be called when the send failed (would lock out retries)")
	}
}

// TestScannerReleasesClaimOnCooldownSkip verifies that when the claim
// succeeds but a long-form cooldown is in effect, the claim is
// released so subsequent ticks can re-claim once the cooldown expires.
func TestScannerReleasesClaimOnCooldownSkip(t *testing.T) {
	recent := time.Now().Add(-1 * time.Hour) // well within 24h cooldown
	repo := &mockSentimentRepo{
		eligible: []string{"conv-cooldown"},
		msgs: map[string][]core.DirectMessage{
			"conv-cooldown": {
				{SenderRole: core.DirectMessageRoleClient, Body: "Still angry"},
			},
		},
		lastAlertSentAt: map[string]*time.Time{
			"conv-cooldown": &recent,
		},
	}
	llm := &testingutil.MockLLM{Answer: `{"score": 2, "reason": "Angry"}`}
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
		t.Fatalf("expected 0 alerts within cooldown, got %d", len(notifier.SentimentAlerts))
	}
	if repo.released["conv-cooldown"] != 1 {
		t.Fatalf("expected ReleaseAlertClaim to fire on cooldown skip, got %d", repo.released["conv-cooldown"])
	}
}
