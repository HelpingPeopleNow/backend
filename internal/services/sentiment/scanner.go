package sentiment

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/metrics"
	"github.com/HelpingPeopleNow/backend/internal/ports"
)

// LLMProvider is hardcoded to mistral for sentiment analysis.
const LLMProvider = "mistral" // Scanner periodically scores direct-message conversations.

// AlertClaimLease is the duration a replica's CAS on alert_claim_at is
// considered valid. The SPOF GAP C fix — see
// infra/docs/FOLLOW_UP_SPOF_Backup_Replicas.md — uses a CAS row that is
// stamped at dispatch and cleared on success/failure. A 60s lease gives
// the Telegram API call (long-tail latency in practice ~1–5s, but
// defence-in-depth for cold starts) enough time to complete while
// keeping the auto-recovery window short if a replica crashes mid-send.
//
// SENTIMENT_SCANNER_INTERVAL defaults to 5m so a 60s lease is ~5x
// smaller than the typical retry gap; any replica that holds the lease
// past 60s loses it on the next scan without operator intervention.
const AlertClaimLease = 60 * time.Second

type Scanner struct {
	repo      ports.SentimentScannerRepository
	llm       ports.LLMService
	notifier  ports.Notifier
	cfg       Config
	worklist  chan string
	closeCh   chan struct{}
	pendingWG sync.WaitGroup
	alertWG   sync.WaitGroup
	started   bool
	closed    bool
	mu        sync.Mutex
}

// Config controls scanner behaviour.
type Config struct {
	Interval       time.Duration
	Cooldown       time.Duration
	BatchSize      int
	MaxMessages    int
	AlertThreshold int16
}

// NewScanner builds a Scanner. It does NOT start the background goroutine;
// call Run for that.
func NewScanner(repo ports.SentimentScannerRepository, llm ports.LLMService, notifier ports.Notifier, cfg Config) *Scanner {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	if cfg.MaxMessages <= 0 {
		cfg.MaxMessages = 20
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Minute
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = 24 * time.Hour
	}
	if cfg.AlertThreshold == 0 {
		cfg.AlertThreshold = 4
	}
	return &Scanner{
		repo:     repo,
		llm:      llm,
		notifier: notifier,
		cfg:      cfg,
		worklist: make(chan string, 3),
		closeCh:  make(chan struct{}),
	}
}

// Run starts the background tick loop. It blocks until ctx is cancelled.
func (s *Scanner) Run(ctx context.Context) {
	if err := s.probeMistral(ctx); err != nil {
		slog.Error("sentiment: startup probe failed; scanner exiting", "error", err)
		return
	}

	tick := time.NewTicker(s.cfg.Interval)
	defer tick.Stop()

	s.startWorkers(ctx)

	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			if !s.closed {
				s.closed = true
				close(s.closeCh)
			}
			s.mu.Unlock()
			slog.Info("sentiment: shutdown requested; draining in-flight scores")
			s.pendingWG.Wait()
			s.alertWG.Wait()
			slog.Info("sentiment: all in-flight scores and alerts drained, exiting")
			return

		case <-tick.C:
			if err := s.TickOnce(ctx); err != nil {
				slog.Warn("sentiment: tick failed", "error", err)
			}
		}
	}
}

func (s *Scanner) probeMistral(ctx context.Context) error {
	names, err := s.llm.AdapterNames(ctx)
	if err != nil {
		return fmt.Errorf("probe helper adapters: %w", err)
	}
	for _, name := range names {
		if name == LLMProvider {
			slog.Info("sentiment: mistral adapter registered", "adapters", names)
			return nil
		}
	}
	return fmt.Errorf("helper adapter registry does not include required provider %q (loaded: %v)", LLMProvider, names)
}

func (s *Scanner) startWorkers(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return
	}
	s.started = true
	for i := 0; i < 3; i++ {
		go func() {
			for {
				select {
				case convID := <-s.worklist:
					func(id string) {
						defer s.pendingWG.Done()
						s.scoreOne(ctx, id)
					}(convID)
				case <-s.closeCh:
					// Drain any buffered work before exiting.
					for {
						select {
						case convID := <-s.worklist:
							func(id string) {
								defer s.pendingWG.Done()
								s.scoreOne(ctx, id)
							}(convID)
						default:
							return
						}
					}
				}
			}
		}()
	}
}

// TickOnce runs a single scoring tick. Exported for integration tests.
func (s *Scanner) TickOnce(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("sentiment: scanner is closed")
	}
	s.mu.Unlock()

	start := time.Now()
	s.startWorkers(ctx)
	slog.Debug("sentiment: tick started", "cooldown", s.cfg.Cooldown, "batch_size", s.cfg.BatchSize)

	ids, err := s.repo.FindEligibleConversations(ctx, s.cfg.Cooldown, s.cfg.BatchSize)
	if err != nil {
		return fmt.Errorf("find eligible conversations: %w", err)
	}

	enqueued := 0
	skipped := 0
	for _, id := range ids {
		// Count the item before sending so Drain() can wait for it even
		// while it sits in the buffered worklist.
		s.pendingWG.Add(1)
		select {
		case s.worklist <- id:
			enqueued++
		case <-s.closeCh:
			s.pendingWG.Done()
			slog.Info("sentiment: scanner closed during tick; aborting enqueue", "conv_id", id)
			return nil
		default:
			s.pendingWG.Done()
			skipped++
			slog.Warn("sentiment: worklist full; deferring conversation to next tick", "conv_id", id)
		}
	}

	slog.Info("sentiment: tick done", "eligible", len(ids), "enqueued", enqueued, "deferred", skipped, "duration_ms", time.Since(start).Milliseconds())
	return nil
}

// Drain waits until the worklist is empty and all in-flight scoring
// and alert goroutines have returned. Useful in tests after calling TickOnce.
func (s *Scanner) Drain() {
	s.pendingWG.Wait()
	s.alertWG.Wait()
}

func (s *Scanner) scoreOne(ctx context.Context, convID string) {
	start := time.Now()
	msgs, err := s.repo.FetchMessages(ctx, convID, s.cfg.MaxMessages)
	if err != nil {
		slog.Warn("sentiment: fetch messages failed", "conv_id", convID, "error", err)
		metrics.IncrSentimentScored("error")
		return
	}
	if len(msgs) == 0 {
		slog.Debug("sentiment: no messages to score", "conv_id", convID)
		metrics.IncrSentimentScored("error")
		return
	}

	transcript := FormatTranscript(msgs)
	userMsg := FormatUserMessage(transcript)

	resp, err := s.llm.Ask(ctx, SystemPrompt, userMsg, nil, LLMProvider)
	if err != nil {
		slog.Warn("sentiment: llm ask failed", "conv_id", convID, "error", err)
		metrics.IncrSentimentScored("error")
		return
	}

	score, reason, err := ParseScore(resp.Answer)
	if err != nil {
		slog.Warn("sentiment: parse failed", "conv_id", convID, "error", err, "raw", resp.Answer)
		metrics.IncrSentimentScored("error")
		return
	}

	if err := s.repo.WriteScore(ctx, convID, score, reason); err != nil {
		slog.Warn("sentiment: write score failed", "conv_id", convID, "error", err)
		metrics.IncrSentimentScored("error")
		return
	}

	if score <= s.cfg.AlertThreshold && s.notifier != nil {
		// SPOF GAP C fix: atomic CAS BEFORE we fire the goroutine.
		// Two replicas scanning the same conversation now race on a
		// single SQL UPDATE that sets alert_claim_at iff the row is
		// unclaimed (or its claim is older than AlertClaimLease).
		// Exactly one replica's UPDATE affects 1 row; only that
		// replica proceeds. The others see claimed=false and skip,
		// closing the duplicate-alert TOCTOU that the pre-fix flow
		// had (FetchLastAlertSentAt → send → MarkAlertSent was a
		// non-atomic check-then-act).
		claimed, err := s.repo.ClaimAlert(ctx, convID, AlertClaimLease)
		if err != nil {
			slog.Warn("sentiment: claim alert failed", "conv_id", convID, "error", err)
		}
		switch {
		case !claimed:
			// Another replica is mid-dispatch. Skip the alert path
			// entirely; scoring and metric updates below still run.
			slog.Debug("sentiment: alert already claimed by another replica; skipping", "conv_id", convID)
		default:
			// Long-form cooldown check (default 24h). FetchLastAlertSentAt
			// only runs on the claim winner — losers skip both this read
			// and the alert entirely.
			lastAlert, err := s.repo.FetchLastAlertSentAt(ctx, convID)
			if err != nil {
				slog.Warn("sentiment: fetch last alert sent failed", "conv_id", convID, "error", err)
			}
			if lastAlert != nil && time.Since(*lastAlert) < s.cfg.Cooldown {
				// Another replica already alerted within cooldown — release
				// the claim so we don't block future ticks. The cooldown
				// still applies (a different replica's MarkAlertSent is
				// already in last_alert_sent_at).
				slog.Debug("sentiment: skipping duplicate alert (cooldown)", "conv_id", convID, "last_alert", *lastAlert)
				if err := s.repo.ReleaseAlertClaim(ctx, convID); err != nil {
					slog.Warn("sentiment: release alert claim failed", "conv_id", convID, "error", err)
				}
				break
			}
			emailA, emailB, err := s.repo.FetchParticipantEmails(ctx, convID)
			if err != nil {
				slog.Warn("sentiment: fetch participant emails failed", "conv_id", convID, "error", err)
				emailA, emailB = convID, "(unknown)"
			}
			s.alertWG.Add(1)
			go func(id string, sc int16, r string, eA, eB string) {
				defer s.alertWG.Done()
				if err := s.notifier.SendSentimentAlert(id, sc, r, eA, eB); err != nil {
					// Send failed: release the claim so a subsequent tick
					// can retry (within the long-form cooldown). Without
					// this, the lease would block retries until it
					// expired and the alert would be effectively lost.
					slog.Warn("sentiment: alert failed; releasing claim for retry", "conv_id", id, "error", err)
					metrics.IncrSentimentAlertFailure("send_error")
					if relErr := s.repo.ReleaseAlertClaim(context.Background(), id); relErr != nil {
						slog.Warn("sentiment: release alert claim after failure failed", "conv_id", id, "error", relErr)
					}
					return
				}
				// Success: persist the cooldown stamp AND clear the claim.
				// Clearing keeps the row tidy — the lease would auto-expire
				// anyway but a NULL column signals "no in-flight send".
				if err := s.repo.MarkAlertSent(context.Background(), id); err != nil {
					slog.Warn("sentiment: mark alert sent failed", "conv_id", id, "error", err)
				}
				if relErr := s.repo.ReleaseAlertClaim(context.Background(), id); relErr != nil {
					slog.Warn("sentiment: release alert claim after success failed", "conv_id", id, "error", relErr)
				}
			}(convID, score, reason, emailA, emailB)
		}
	}

	latency := time.Since(start)
	metrics.ObserveSentimentLatency(latency)
	metrics.IncrSentimentScored("ok")
	slog.Info("sentiment: scored", "conv_id", convID, "score", score, "latency_ms", latency.Milliseconds(), "provider", LLMProvider)
}
