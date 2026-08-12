package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/HelpingPeopleNow/backend/database"
	"github.com/HelpingPeopleNow/backend/internal/adapters/handler"
	"github.com/HelpingPeopleNow/backend/internal/adapters/llm"
	"github.com/HelpingPeopleNow/backend/internal/adapters/middleware"
	"github.com/HelpingPeopleNow/backend/internal/adapters/notification"
	"github.com/HelpingPeopleNow/backend/internal/adapters/ratelimit"
	"github.com/HelpingPeopleNow/backend/internal/adapters/realtime"
	"github.com/HelpingPeopleNow/backend/internal/adapters/repository"
	"github.com/HelpingPeopleNow/backend/internal/metrics"
	"github.com/HelpingPeopleNow/backend/internal/ports"
	"github.com/HelpingPeopleNow/backend/internal/services"
	"github.com/HelpingPeopleNow/backend/internal/services/sentiment"
	"gorm.io/gorm"
)

type appDeps struct {
	DB           *gorm.DB
	ChatRepo     ports.ChatRepository
	ProfileRepo  ports.ProfileRepository
	PromptRepo   ports.SystemPromptRepository
	DMRepo       ports.DirectMessageRepository
	FeedbackRepo ports.FeedbackRepository
	Notifier     ports.Notifier
	LLM          ports.LLMService
	Intake       *services.IntakeService
	Search       *services.SearchService
	Seed         *services.SeedService
	Sentiment    *sentiment.Scanner
	Auth         *middleware.AuthMiddleware
	Admin        *middleware.AdminMiddleware
}

func buildDeps(db *gorm.DB) appDeps {
	chatRepo := repository.NewGormChatRepository(db)
	profileRepo := repository.NewGormProfileRepository(db)
	promptRepo := repository.NewGormSystemPromptRepository(db)
	llmSvc := llm.NewGRPCLLMService(os.Getenv("HELPER_GRPC_ADDR"), os.Getenv("HELPER_HEALTH_URL"))
	feedbackRepo := repository.NewGormFeedbackRepository(db)
	notifier := notification.NewTelegramNotifier(readSecretEnv("TELEGRAM_BOT_TOKEN", "TELEGRAM_BOT_TOKEN_FILE"), os.Getenv("TELEGRAM_CHAT_ID"), os.Getenv("FRONTEND_URL"))

	sentimentRepo := repository.NewGormSentimentScannerRepository(db)
	sentimentScanner := sentiment.NewScanner(sentimentRepo, llmSvc, notifier, sentiment.Config{
		Interval:       parseDurationEnv("SENTIMENT_SCANNER_INTERVAL", 5*time.Minute),
		Cooldown:       parseDurationEnv("SENTIMENT_SCORE_COOLDOWN", 24*time.Hour),
		BatchSize:      parseIntEnv("SENTIMENT_SCANNER_BATCH_SIZE", 50),
		MaxMessages:    parseIntEnv("SENTIMENT_SCORE_MAX_MESSAGES", 20),
		AlertThreshold: int16(parseIntEnv("SENTIMENT_ALERT_THRESHOLD", 4)),
	})

	return appDeps{
		DB:           db,
		ChatRepo:     chatRepo,
		ProfileRepo:  profileRepo,
		PromptRepo:   promptRepo,
		DMRepo:       repository.NewGormDirectMessageRepository(db),
		FeedbackRepo: feedbackRepo,
		Notifier:     notifier,
		LLM:          llmSvc,
		Intake:       services.NewIntakeService(llmSvc, profileRepo, chatRepo, promptRepo),
		Search:       services.NewSearchService(llmSvc, profileRepo, chatRepo, promptRepo),
		Seed:         services.NewSeedService(promptRepo),
		Sentiment:    sentimentScanner,
		// P2-3 (audit / F8): the third arg is BETTER_AUTH_SECRET. The
		// DB-fallback path verifies the cookie HMAC against this secret
		// before honoring the session token — without it, a cookie whose
		// signature has been stripped still resolves (the pre-audit
		// behaviour). Production MUST set BETTER_AUTH_SECRET.
		Auth:  middleware.NewAuthMiddleware(os.Getenv("AUTH_SERVICE_URL"), db, os.Getenv("BETTER_AUTH_SECRET")),
		Admin: middleware.NewAdminMiddleware(os.Getenv("AUTH_SERVICE_URL")),
	}
}

// muxClosers aggregates the per-replica resources that need a Close on
// SIGTERM. Returned by buildMux so main() can release them after the
// sweeper drain completes. SPOF GAP A (broker) + GAP B (prompt
// invalidator) + GAP D (rate limiters).
type muxClosers struct {
	broker            func() error
	promptInvalidator func() error
	dmLimiter         func() error
	searchLimiter     func() error
	feedbackLimiter   func() error
}

func buildMux(d appDeps) (*http.ServeMux, *muxClosers) {
	mux := http.NewServeMux()

	healthHandler := handler.NewHealthHandler(d.DB, d.LLM)
	mux.Handle("/health", healthHandler)
	mux.Handle("/livez", http.HandlerFunc(healthHandler.Livez))
	mux.Handle("/readyz", handler.NewReadyzHandler(handler.ReadyFlag()))

	// SPOF GAP A (see infra/docs/FOLLOW_UP_SPOF_Backup_Replicas.md):
	// when REDIS_URL is set, the SSE broker fans out events across
	// replicas via Redis pub/sub so a /stream attached to replica B
	// receives events published on replica A. With an empty REDIS_URL
	// we fall back to the in-process broker (unchanged behaviour for
	// single-replica or dev). The redis client is closed by the
	// returned closer on SIGTERM.
	var broker ports.Broker
	closers := &muxClosers{}
	localBroker := realtime.NewSSEBroker()
	if rb, closer, err := realtime.NewRedisSSEBroker(os.Getenv("REDIS_URL"), localBroker); err != nil {
		slog.Error("sse: failed to construct redis broker; falling back to in-process",
			"error", err)
		broker = localBroker
		closers.broker = func() error { return nil }
	} else {
		broker = rb
		closers.broker = closer
	}

	// SPOF GAP B (see infra/docs/FOLLOW_UP_SPOF_Backup_Replicas.md): cross-
	// replica invalidation of system-prompt cache. The GORM repo already
	// refreshes its in-memory cache on every Update; this invalidator
	// publishes to Redis pub/sub on Update and starts a goroutine that
	// reloads the cache on receipt. Empty REDIS_URL returns a no-op
	// invalidator (single-replica / dev).
	if gr, ok := d.PromptRepo.(*repository.GormSystemPromptRepository); ok {
		inv, closer, err := realtime.NewPromptInvalidator(os.Getenv("REDIS_URL"), gr)
		if err != nil {
			slog.Error("prompt invalidator: failed to construct", "error", err)
			closers.promptInvalidator = func() error { return nil }
		} else {
			closers.promptInvalidator = closer
			gr.SetInvalidator(inv)
		}
	}

	// SPOF GAP D (see infra/docs/FOLLOW_UP_SPOF_Backup_Replicas.md): per-user
	// rate limiters live in Redis so N replicas share one quota. The
	// pre-fix in-process limiter became N× the configured cap when the
	// backend ran as 2+ replicas. Each limiter wraps an in-process
	// fallback (the pre-fix RateLimiter) that takes over on any Redis
	// error — graceful degradation so a Redis outage does not break the
	// auth/flood-protection contract entirely, just weakens it.
	redisURL := os.Getenv("REDIS_URL")
	var dmRateLimiter, searchRateLimiter, feedbackRateLimiter ratelimit.Limiter
	var err error
	dmRateLimiter, closers.dmLimiter, err = newSharedLimiter(redisURL, "dm-send", 30, time.Minute)
	if err != nil {
		slog.Error("rate limiter: failed to build dm-send limiter", "error", err)
		dmRateLimiter = ratelimit.NewRateLimiter(30, time.Minute)
		closers.dmLimiter = func() error { return nil }
	}
	searchRateLimiter, closers.searchLimiter, err = newSharedLimiter(redisURL, "chat", 10, time.Minute)
	if err != nil {
		slog.Error("rate limiter: failed to build chat limiter", "error", err)
		searchRateLimiter = ratelimit.NewRateLimiter(10, time.Minute)
		closers.searchLimiter = func() error { return nil }
	}
	feedbackRateLimiter, closers.feedbackLimiter, err = newSharedLimiter(redisURL, "feedback", 5, time.Minute)
	if err != nil {
		slog.Error("rate limiter: failed to build feedback limiter", "error", err)
		feedbackRateLimiter = ratelimit.NewRateLimiter(5, time.Minute)
		closers.feedbackLimiter = func() error { return nil }
	}
	dmHandler := handler.NewDirectMessagingHandler(d.DMRepo, d.ProfileRepo, broker, dmRateLimiter)
	mux.Handle("/api/v1/chat", middleware.CORS(d.Auth.Wrap(handler.NewChatHandler(d.Intake, d.Search, d.PromptRepo, searchRateLimiter))))
	// Wire the rate-limit fallback counter on every shared limiter so
	// each Redis error increments rate_limit_fallback_total{scope=...}.
	if sl, ok := dmRateLimiter.(*ratelimit.SharedRateLimiter); ok {
		sl.SetFallbackCounter(metrics.IncrRateLimitFallback)
	}
	if sl, ok := searchRateLimiter.(*ratelimit.SharedRateLimiter); ok {
		sl.SetFallbackCounter(metrics.IncrRateLimitFallback)
	}
	if sl, ok := feedbackRateLimiter.(*ratelimit.SharedRateLimiter); ok {
		sl.SetFallbackCounter(metrics.IncrRateLimitFallback)
	}
	mux.Handle("/api/v1/worker/profile", middleware.CORS(d.Auth.Wrap(handler.NewWorkerHandler(d.ProfileRepo))))
	mux.Handle("/api/v1/client/profile", middleware.CORS(d.Auth.Wrap(handler.NewClientHandler(d.ProfileRepo))))
	mux.Handle("/api/v1/conversations", middleware.CORS(d.Auth.Wrap(handler.NewConversationHandler(d.ChatRepo))))
	mux.Handle("/api/v1/conversations/", middleware.CORS(d.Auth.Wrap(handler.NewConversationHandler(d.ChatRepo))))

	mux.Handle("/api/v1/workers/", middleware.CORS(d.Auth.Wrap(dmHandler)))
	mux.Handle("/api/v1/direct-messages", middleware.CORS(d.Auth.Wrap(dmHandler)))
	mux.Handle("/api/v1/direct-messages/", middleware.CORS(d.Auth.Wrap(dmHandler)))

	mux.Handle("/api/v1/system-prompts", middleware.CORS(d.Auth.Wrap(handler.NewSystemPromptHandler(d.PromptRepo))))
	mux.Handle("/api/v1/system-prompts/", middleware.CORS(d.Auth.Wrap(d.Admin.Wrap(handler.NewSystemPromptHandler(d.PromptRepo)))))

	mux.Handle("/api/v1/admin/", middleware.CORS(d.Auth.Wrap(d.Admin.Wrap(handler.NewAdminHandler(d.DB)))))
	mux.Handle("/api/v1/admin/reembed", middleware.CORS(d.Auth.Wrap(d.Admin.Wrap(handler.NewReembedToggleHandler(d.Intake)))))

	// Feedback — user submission (logged-in or anonymous), admin CRUD via /api/v1/admin/feedback.
	feedbackHandler := handler.NewFeedbackHandler(d.FeedbackRepo, d.Notifier, feedbackRateLimiter)
	mux.Handle("/api/v1/feedback", middleware.CORS(d.Auth.WrapOptional(http.HandlerFunc(feedbackHandler.Submit))))

	// Public profiles — no auth middleware.
	publicProfileHandler := handler.NewPublicProfileHandler(d.ProfileRepo)
	mux.Handle("/api/v1/workers/public/latest", http.HandlerFunc(publicProfileHandler.LatestProfiles))
	mux.Handle("/api/v1/workers/public/", http.HandlerFunc(publicProfileHandler.ServeHTTP))

	// OpenAPI docs (spec-first contract, see infra/docs/SPEC.md §9).
	// Raw spec is session-auth'd; the interactive UI is admin-only so
	// the API contract stays internal.
	mux.Handle("/api/v1/openapi.yaml", middleware.CORS(d.Auth.Wrap(handler.OpenAPISpecHandler())))
	mux.Handle("/api/v1/docs", middleware.CORS(d.Admin.Wrap(handler.OpenAPIDocsHandler())))

	// P2-2 (audit / F9): protect /metrics behind METRICS_TOKEN. An empty
	// token falls back to unauthenticated with a logged warning so an
	// operator notices. Production must set METRICS_TOKEN.
	//
	// P2-1 (audit / F6): wireGaugeScrapeSources registers the dynamic
	// gauges (db_pool_in_use, db_pool_max, search_cache_size,
	// sse_active_connections) so /metrics returns up-to-the-moment
	// values from external state.
	handler.RegisterMetricsRoutes(mux, os.Getenv("METRICS_TOKEN"))
	wireGaugeScrapeSources(d.DB, d.Search, broker)
	return mux, closers
}

// wireGaugeScrapeSources registers the dynamic gauges driven by external
// state (P2-1 audit / F6). Each callback is a quick getter — it runs at
// every /metrics scrape with no long-lived mutex held by the metrics
// package.
func wireGaugeScrapeSources(db *gorm.DB, search *services.SearchService, broker ports.Broker) {
	// db_pool_in_use — current saturation gauge.
	handler.RegisterGaugeScrapeSource(
		"db_pool_in_use",
		"Active (*sql.DB).InUse connections — saturation gauge.",
		nil, nil,
		func() float64 {
			sqlDB, err := db.DB()
			if err != nil {
				return 0
			}
			return float64(sqlDB.Stats().InUse)
		},
	)
	// db_pool_max — companion to in_use so saturation alerts can compute
	// the in_use / max ratio (matches the §5 commented DBPoolSaturation
	// alert expression).
	handler.RegisterGaugeScrapeSource(
		"db_pool_max",
		"Configured (*sql.DB).MaxOpenConnections.",
		nil, nil,
		func() float64 {
			sqlDB, err := db.DB()
			if err != nil {
				return 0
			}
			return float64(sqlDB.Stats().MaxOpenConnections)
		},
	)
	// search_cache_size
	handler.RegisterGaugeScrapeSource(
		"search_cache_size",
		"Current entries in the in-process search cache.",
		nil, nil,
		func() float64 { return float64(search.SearchCacheSize()) },
	)
	// sse_active_connections
	handler.RegisterGaugeScrapeSource(
		"sse_active_connections",
		"Current in-process SSE subscribers across all users.",
		nil, nil,
		func() float64 { return float64(broker.ActiveConnections()) },
	)
}

// runStalenessSweeper (VECTOR_SEARCH_PLAN §8.10 / Improvement #11).
//
// P2-2 audit: the previous implementation spawned one blocked goroutine
// per stale worker; at ~dozens that's fine, at thousands it leaks
// goroutines. We now use a bounded worklist channel with cap = sem
// size: workers drain the channel and call ReembedWorker, which itself
// acquires the semaphore. The loop never spawns more than `semCap`
// in-flight goroutines, the drain on shutdown still uses the wg, and
// the original NUM_PARALLEL=1 Ollama slot is still preserved (each
// worker holds one sem token for the duration of the embed).
//
// Loop semantics:
//   - On each tick: find stale IDs, send them into the worklist channel
//     (non-blocking; if the channel is full, the ID is logged and
//     dropped — they'll be picked up on the next tick, no data loss).
//   - N drain workers (cap = sem) read from the channel and call
//     ReembedWorker; pendingWG tracks them for clean shutdown.
//   - On ctx.Done: close the channel, drainers exit when empty, then
//     wg.Wait() to ensure all ReembedWorker calls have returned.
func runStalenessSweeper(
	ctx context.Context,
	intake *services.IntakeService,
	profileRepo ports.ProfileRepository,
	interval time.Duration,
) {
	tick := time.NewTicker(interval)
	defer tick.Stop()

	const semCap = 3
	worklist := make(chan string, semCap)
	var pendingWG sync.WaitGroup

	// Start drain workers.
	for i := 0; i < semCap; i++ {
		go func() {
			for uid := range worklist {
				pendingWG.Add(1)
				func(userID string) {
					defer pendingWG.Done()
					// Per-worker 60s deadline lives inside IntakeService.ReembedWorker.
					intake.ReembedWorker(userID)
				}(uid)
			}
		}()
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("sweeper: shutdown requested; closing worklist and draining in-flight re-embeds")
			close(worklist)
			pendingWG.Wait()
			slog.Info("sweeper: all in-flight re-embeds drained, exiting")
			return

		case <-tick.C:
			sweepCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			ids, err := profileRepo.FindStaleWorkerIDs(sweepCtx)
			if err != nil {
				slog.Warn("sweeper: FindStaleWorkerIDs failed", "error", err)
				cancel()
				continue
			}
			if len(ids) == 0 {
				slog.Info("sweeper: no stale workers")
				cancel()
				continue
			}
			slog.Info("sweeper: re-embedding stale workers", "count", len(ids))
			enqueued := 0
			dropped := 0
			for _, uid := range ids {
				select {
				case worklist <- uid:
					enqueued++
				default:
					dropped++
					slog.Warn("sweeper: worklist full; deferring stale worker to next tick", "user_id", uid)
				}
			}
			slog.Info("sweeper: tick complete", "enqueued", enqueued, "dropped", dropped)
			cancel()
		}
	}
}

// startHealthPoller keeps the health_status gauges (postgres, grpc_helper)
// live instead of stale. The gauges were previously only updated inside the
// /health handler, so after a transient dependency blip the gauge stayed at
// the failed value until the next /health call — producing false "down"
// readings in Grafana (and any future alert) long after the dependency
// recovered. Polling every 10s keeps the metrics truthful. It runs on the
// shared rootCtx/rootWG so it drains cleanly on SIGTERM.
// healthChecker is the minimal dependency startHealthPoller needs.
type healthChecker interface {
	Health(ctx context.Context) error
}

func startHealthPoller(ctx context.Context, wg *sync.WaitGroup, db *gorm.DB, llm healthChecker) {
	const interval = 10 * time.Second
	probe := func() {
		postgres := "ok"
		if sqlDB, err := db.DB(); err != nil {
			postgres = "down"
		} else if err := sqlDB.PingContext(ctx); err != nil {
			postgres = "down"
		}
		handler.SetHealthStatus("postgres", postgres == "ok")

		grpcHelper := "ok"
		if err := llm.Health(ctx); err != nil {
			grpcHelper = "down"
		}
		handler.SetHealthStatus("grpc_helper", grpcHelper == "ok")

		if postgres != "ok" || grpcHelper != "ok" {
			slog.Warn("health poller: degraded", "postgres", postgres, "grpc_helper", grpcHelper)
		}
	}

	// Probe once immediately so the gauges are correct from startup, not
	// after the first 10s tick.
	probe()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			probe()
		}
	}
}

func main() {
	// P3-4 (audit): wrap the slog default handler with a ContextHandler so
	// every log line emitted via slog.Default() automatically carries the
	// per-request correlation ID (P3-4 cross-service tracing). Tests that
	// need io.Discard replace slog.Default themselves and lose the
	// injection — that's fine because tests don't have request IDs.
	slog.SetDefault(slog.New(middleware.NewContextHandler(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}),
	)))

	requireEnv("DB_HOST")
	requireEnv("DB_USER")
	requireEnv("DB_PASSWORD")
	requireEnv("DB_NAME")
	requireEnv("AUTH_SERVICE_URL")
	requireEnv("HELPER_GRPC_ADDR")
	requireEnv("HELPER_HEALTH_URL")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	slog.Info("starting backend", "port", port)

	db, err := database.Connect()
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	slog.Info("database connected")

	deps := buildDeps(db)
	if err := deps.Seed.SeedSystemPrompts(context.Background()); err != nil {
		slog.Warn("seed system prompts failed", "error", err)
	} else {
		slog.Info("system prompts ready")
	}

	mux, closers := buildMux(deps)

	// P0-follow-up: /readyz gate. Flip on the readiness flag once the
	// startup critical path is complete (DB connected, system prompts
	// seeded, mux wired). The staleness sweeper is housekeeping and
	// starts a few lines further down — readiness does NOT block on it.
	// Traefik uses /readyz as the health-check in the multi-replica
	// deploy that resolves the single-replica SPOF (see
	// infra/docs/FOLLOW_UP_SPOF_Backup_Replicas.md Phase 2). Until the flag is true the
	// load-balancer should treat this replica as drained.
	handler.MarkReady()

	// VECTOR_SEARCH_PLAN §8.10 / Improvement #11: kick off the staleness
	// sweeper with a cancellable context, registered on rootWG so the
	// process waits for it on SIGTERM (Plan showstopper #3 — the
	// previous code allowed main to exit immediately after server.Shutdown
	// unblocked ListenAndServe, killing any mid-write ReembedWorker).
	rootCtx, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()

	var rootWG sync.WaitGroup
	rootWG.Add(1)
	go func() {
		defer rootWG.Done()
		runStalenessSweeper(rootCtx, deps.Intake, deps.ProfileRepo, 10*time.Minute)
	}()

	// Sentiment scanner background goroutine.
	if os.Getenv("SENTIMENT_SCANNER_ENABLED") != "false" {
		metrics.SetSentimentEnabled(true)
		rootWG.Add(1)
		go func() {
			defer rootWG.Done()
			deps.Sentiment.Run(rootCtx)
		}()
	} else {
		metrics.SetSentimentEnabled(false)
		slog.Warn("sentiment: scanner disabled via SENTIMENT_SCANNER_ENABLED=false")
	}

	// Background health poller — keeps health_status gauges live so Grafana
	// (and any future alert) reflects real dependency state, not the last
	// /health call (see startHealthPoller).
	rootWG.Add(1)
	go func() {
		defer rootWG.Done()
		startHealthPoller(rootCtx, &rootWG, db, deps.LLM)
	}()

	// P3-4 (audit): insert RequestID as the OUTERMOST middleware so
	// (a) the Logging middleware's "request started"/"request completed"
	//     lines carry the request_id attribute, AND
	// (b) the response always surfaces X-Request-ID back to the client.
	// The order RequestID → Logging → mux keeps all downstream handler
	// chain calls inside the same ctx with the ID bound.
	server := newServer(":"+port, middleware.RequestID(middleware.Logging(mux)))

	// Signal handler — SIGTERM/SIGINT triggers the coordinated
	// shutdown sequence (see runShutdownSequence below + the Phase 3
	// entry in infra/docs/FOLLOW_UP_SPOF_Backup_Replicas.md). The body is extracted
	// to a package-level function so it can be unit-tested with an
	// injected startShutdown recorder and a 50ms drainWait (no need
	// for real wall-clock 14s sleep in CI).
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigChan
		slog.Info("shutdown signal received", "signal", sig.String())
		runShutdownSequence(
			context.Background(),
			server.Shutdown,
			cancelRoot,
			shutdownDrainDur(),
		)
	}()

	slog.Info("listening", "addr", ":"+port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}

	// Bounded drain of the sweeper goroutine (Plan showstopper #3 fix).
	// 65s cap — slightly above ReembedWorker's 60s per-worker deadline so
	// a normal in-flight write completes cleanly. If something is truly
	// stuck, we log a warning and exit anyway rather than hang the process
	// forever (k8s SIGKILL after terminationGracePeriodSeconds is worse).
	slog.Info("server stopped cleanly; waiting for sweeper to drain")
	drainDone := make(chan struct{})
	go func() {
		rootWG.Wait()
		close(drainDone)
	}()
	select {
	case <-drainDone:
		slog.Info("sweeper drained cleanly; exiting")
	case <-time.After(65 * time.Second):
		slog.Warn("sweeper drain timed out after 65s; exiting anyway (in-flight ReembedWorker may have been killed)")
	}

	// SPOF GAP A: release the Redis pub/sub connection last so the
	// in-process pump goroutines have already unwound. The close
	// itself is non-blocking; failures are logged and ignored (we are
	// already exiting).
	if closers.broker != nil {
		if err := closers.broker(); err != nil {
			slog.Warn("sse: broker close error", "error", err)
		}
	}
	// SPOF GAP B: stop the prompt-invalidator subscription goroutine.
	// Same non-blocking close; failures are logged.
	if closers.promptInvalidator != nil {
		if err := closers.promptInvalidator(); err != nil {
			slog.Warn("prompt invalidator close error", "error", err)
		}
	}
	// SPOF GAP D: release the Redis-backed rate-limiter clients. Same
	// non-blocking close; failures are logged.
	for name, c := range map[string]func() error{
		"dm-send":  closers.dmLimiter,
		"chat":     closers.searchLimiter,
		"feedback": closers.feedbackLimiter,
	} {
		if c == nil {
			continue
		}
		if err := c(); err != nil {
			slog.Warn("rate limiter close error", "scope", name, "error", err)
		}
	}
}

// newSharedLimiter builds a Redis-backed SharedRateLimiter that falls
// back to an in-process RateLimiter on Redis errors. Returns the
// concrete Limiter (one of *SharedRateLimiter or *RateLimiter) plus a
// closer that releases the underlying Redis client on shutdown.
//
// An empty redisURL returns the in-process limiter with a no-op
// closer — single-replica / dev behaviour, unchanged from pre-fix.
func newSharedLimiter(redisURL, scope string, max int, period time.Duration) (ratelimit.Limiter, func() error, error) {
	fallback := ratelimit.NewRateLimiter(max, period)
	if redisURL == "" {
		return fallback, func() error { return nil }, nil
	}
	sl, err := ratelimit.NewSharedRateLimiter(redisURL, scope, max, period, fallback)
	if err != nil {
		return fallback, func() error { return nil }, err
	}
	return sl, sl.Close, nil
}

func parseDurationEnv(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		slog.Warn("invalid duration env, falling back to default", "key", key, "value", raw, "default", fallback)
		return fallback
	}
	return d
}

func parseIntEnv(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		slog.Warn("invalid int env, falling back to default", "key", key, "value", raw, "default", fallback)
		return fallback
	}
	return n
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("missing required environment variable", "key", key)
		os.Exit(1)
	}
	return v
}

// newServer constructs the production *http.Server with slowloris /
// idle-connection hardening (P0-2 audit, F1). Extracted so the timeout
// configuration can be unit-tested in main_test.go.
//
// No WriteTimeout: the SSE /stream endpoint holds the response open
// indefinitely and manages its own lifecycle via request context + a
// 25s heartbeat (with a 15-minute max-stream-duration cap, P2-6).
func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// shutdownDrainDur reads the SHUTDOWN_DRAIN_WAIT env var (Go duration
// format, e.g. "14s") and returns the parsed duration, or 14s default
// if unset / unparseable / negative.
//
// 14s matches the Phase 2 Traefik LB health-check worst-case ceiling:
// 10s interval + 3s timeout + 1s slack. Operators can override via
// env if their Traefik setup uses longer intervals, or drop it to
// 0s in local dev for snappy rebuilds (mirrors the
// maxSSEStreamDuration() pattern from direct_messaging_handler.go).
//
// OPERATIONAL NOTE: Docker's default stop_grace_period is 10s and
// Kubernetes' default terminationGracePeriodSeconds is 30s — BOTH
// below the 14s drain. The infra/docker-compose.yml backend service
// MUST stay at stop_grace_period: 120s (15s drain + 30s Shutdown +
// 65s sweeper + slack) so SIGKILL doesn't preempt the drain.
func shutdownDrainDur() time.Duration {
	const defaultDur = 14 * time.Second
	raw := os.Getenv("SHUTDOWN_DRAIN_WAIT")
	if raw == "" {
		return defaultDur
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return defaultDur
	}
	return d
}

// runShutdownSequence is the SIGTERM/SIGINT shutdown body, extracted
// as a package-level function for testability. main()'s signal
// goroutine delegates to it (sees above).
//
// Sequence (Phase 3 of the SPOF remediation — see
// infra/docs/FOLLOW_UP_SPOF_Backup_Replicas.md):
//
//  1. cancelRoot() — stop the staleness sweeper and start its 60s
//     drain. Firing it BEFORE the LB drain parallelises the two waits
//     so total shutdown is shorter than 14s + 30s + 65s serialised.
//
//  2. handler.MarkUnready() — flip /readyz to 503. Phase 2 Traefik LB
//     health-check (10s interval, 3s timeout) sees the 5xx on the
//     next tick and removes this replica from the routing pool. New
//     requests route to siblings in multi-replica, or get a 502 in
//     single-replica (acceptable during drain).
//
//  3. Sleep drainWait — covers the worst-case Traefik detection
//     latency. Without this, startShutdown would tear down accept
//     listeners in-flight, dropping the existing requests that
//     Traefik hasn't yet (visibly) aborted.
//
//  4. startShutdown(ctx) — TCP-level listener close + wait for
//     in-flight handlers to return (30s budget).
//
// startShutdown is injected as a function so the regression test can
// substitute a recorder and assert the event order with a tiny
// drainWait (~50ms in CI).
func runShutdownSequence(
	ctx context.Context,
	startShutdown func(ctx context.Context) error,
	cancelRoot func(),
	drainWait time.Duration,
) {
	slog.Info("shutdown sequence: starting")

	if cancelRoot != nil {
		cancelRoot()
		slog.Info("shutdown sequence: signaled staleness sweeper to drain")
	}

	handler.MarkUnready()
	slog.Info("shutdown sequence: /readyz flipped to 503; awaiting Traefik LB health-check to drain",
		"drain_wait", drainWait)

	// Plain time.Sleep is intentional — the drain window is bounded by
	// the orchestrator's SIGKILL grace (Docker stop_grace_period /
	// Kubernetes terminationGracePeriodSeconds, both set to >=120s for
	// the backend), not by ctx cancellation. Wiring ctx.Done() here
	// would only shorten failsafe margins without any benefit.
	time.Sleep(drainWait)
	slog.Info("shutdown sequence: LB drain window elapsed")

	shutdownCtx, cancelShutdown := context.WithTimeout(ctx, 30*time.Second)
	defer cancelShutdown()
	if err := startShutdown(shutdownCtx); err != nil {
		slog.Error("shutdown sequence: HTTP shutdown error", "error", err)
	}
	slog.Info("shutdown sequence: HTTP shutdown complete")
}

// readSecretEnv resolves a secret by preferring a *_FILE env var
// (Docker-secret-style: path to a file containing only the secret value)
// over the plain env var. If fileEnv points to a readable file, its
// trimmed contents are returned; otherwise the value of directEnv is
// returned. Either may be empty.
func readSecretEnv(directEnv, fileEnv string) string {
	if path := strings.TrimSpace(os.Getenv(fileEnv)); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("secret file unreadable, falling back to env", "file_env", fileEnv, "path", path, "error", err)
		} else {
			return strings.TrimSpace(string(data))
		}
	}
	return os.Getenv(directEnv)
}
