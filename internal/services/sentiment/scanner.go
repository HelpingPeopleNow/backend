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
		// Check if we should send an alert (score <= threshold AND no recent alert).
		lastAlert, err := s.repo.FetchLastAlertSentAt(ctx, convID)
		if err != nil {
			slog.Warn("sentiment: fetch last alert sent failed", "conv_id", convID, "error", err)
		}
		if lastAlert != nil && time.Since(*lastAlert) < s.cfg.Cooldown {
			slog.Debug("sentiment: skipping duplicate alert", "conv_id", convID, "last_alert", *lastAlert)
		} else {
			emailA, emailB, err := s.repo.FetchParticipantEmails(ctx, convID)
			if err != nil {
				slog.Warn("sentiment: fetch participant emails failed", "conv_id", convID, "error", err)
				emailA, emailB = convID, "(unknown)"
			}
			s.alertWG.Add(1)
			go func(id string, sc int16, r string, eA, eB string) {
				defer s.alertWG.Done()
				if err := s.notifier.SendSentimentAlert(id, sc, r, eA, eB); err != nil {
					slog.Warn("sentiment: alert failed", "conv_id", id, "error", err)
				}
			}(convID, score, reason, emailA, emailB)
			if err := s.repo.MarkAlertSent(ctx, convID); err != nil {
				slog.Warn("sentiment: mark alert sent failed", "conv_id", convID, "error", err)
			}
		}
	}

	latency := time.Since(start)
	metrics.ObserveSentimentLatency(latency)
	metrics.IncrSentimentScored("ok")
	slog.Info("sentiment: scored", "conv_id", convID, "score", score, "latency_ms", latency.Milliseconds(), "provider", LLMProvider)
}
