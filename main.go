package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/HelpingPeopleNow/backend/database"
	"github.com/HelpingPeopleNow/backend/internal/adapters/handler"
	"github.com/HelpingPeopleNow/backend/internal/adapters/llm"
	"github.com/HelpingPeopleNow/backend/internal/adapters/middleware"
	"github.com/HelpingPeopleNow/backend/internal/adapters/ratelimit"
	"github.com/HelpingPeopleNow/backend/internal/adapters/realtime"
	"github.com/HelpingPeopleNow/backend/internal/adapters/repository"
	"github.com/HelpingPeopleNow/backend/internal/ports"
	"github.com/HelpingPeopleNow/backend/internal/services"
	"gorm.io/gorm"
)

type appDeps struct {
	DB          *gorm.DB
	ChatRepo    ports.ChatRepository
	ProfileRepo ports.ProfileRepository
	PromptRepo  ports.SystemPromptRepository
	DMRepo      ports.DirectMessageRepository
	LLM         ports.LLMService
	Intake      *services.IntakeService
	Search      *services.SearchService
	Seed        *services.SeedService
	Auth        *middleware.AuthMiddleware
	Admin       *middleware.AdminMiddleware
}

func buildDeps(db *gorm.DB) appDeps {
	chatRepo := repository.NewGormChatRepository(db)
	profileRepo := repository.NewGormProfileRepository(db)
	promptRepo := repository.NewGormSystemPromptRepository(db)
	llmSvc := llm.NewGRPCLLMService(os.Getenv("HELPER_GRPC_ADDR"), os.Getenv("HELPER_HEALTH_URL"))

	return appDeps{
		DB:          db,
		ChatRepo:    chatRepo,
		ProfileRepo: profileRepo,
		PromptRepo:  promptRepo,
		DMRepo:      repository.NewGormDirectMessageRepository(db),
		LLM:         llmSvc,
		Intake:      services.NewIntakeService(llmSvc, profileRepo, chatRepo, promptRepo),
		Search:      services.NewSearchService(llmSvc, profileRepo, chatRepo, promptRepo),
		Seed:        services.NewSeedService(promptRepo),
		// P2-3 (audit / F8): the third arg is BETTER_AUTH_SECRET. The
		// DB-fallback path verifies the cookie HMAC against this secret
		// before honoring the session token — without it, a cookie whose
		// signature has been stripped still resolves (the pre-audit
		// behaviour). Production MUST set BETTER_AUTH_SECRET.
		Auth:  middleware.NewAuthMiddleware(os.Getenv("AUTH_SERVICE_URL"), db, os.Getenv("BETTER_AUTH_SECRET")),
		Admin: middleware.NewAdminMiddleware(os.Getenv("AUTH_SERVICE_URL")),
	}
}

func buildMux(d appDeps) *http.ServeMux {
	mux := http.NewServeMux()

	mux.Handle("/health", handler.NewHealthHandler(d.DB, d.LLM))
	mux.Handle("/livez", handler.NewHealthHandler(d.DB, d.LLM))
	mux.Handle("/readyz", handler.NewReadyzHandler(handler.ReadyFlag()))

	broker := realtime.NewSSEBroker()
	dmRateLimiter := ratelimit.NewRateLimiter(30, time.Minute)
	searchRateLimiter := ratelimit.NewRateLimiter(10, time.Minute)
	dmHandler := handler.NewDirectMessagingHandler(d.DMRepo, d.ProfileRepo, broker, dmRateLimiter)
	mux.Handle("/api/v1/chat", middleware.CORS(d.Auth.Wrap(handler.NewChatHandler(d.Intake, d.Search, d.PromptRepo, searchRateLimiter))))
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

	// Public profiles — no auth middleware.
	publicProfileHandler := handler.NewPublicProfileHandler(d.ProfileRepo)
	mux.Handle("/api/v1/workers/public/latest", http.HandlerFunc(publicProfileHandler.LatestProfiles))
	mux.Handle("/api/v1/workers/public/", http.HandlerFunc(publicProfileHandler.ServeHTTP))

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
	return mux
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

	mux := buildMux(deps)

	// P0-follow-up: /readyz gate. Flip on the readiness flag once the
	// startup critical path is complete (DB connected, system prompts
	// seeded, mux wired). The staleness sweeper is housekeeping and
	// starts a few lines further down — readiness does NOT block on it.
	// Traefik uses /readyz as the health-check in the multi-replica
	// deploy that resolves the single-replica SPOF (see
	// infra/docs/FOLLOW_UP_SPOF.md Phase 2). Until the flag is true the
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

	// P3-4 (audit): insert RequestID as the OUTERMOST middleware so
	// (a) the Logging middleware's "request started"/"request completed"
	//     lines carry the request_id attribute, AND
	// (b) the response always surfaces X-Request-ID back to the client.
	// The order RequestID → Logging → mux keeps all downstream handler
	// chain calls inside the same ctx with the ID bound.
	server := newServer(":"+port, middleware.RequestID(middleware.Logging(mux)))

	// Signal handler — SIGTERM/SIGINT triggers a coordinated shutdown.
	// cancelRoot() runs FIRST so the sweeper's inner pendingWG.Wait()
	// (which can hold for up to 60s waiting on an in-flight ReembedWorker)
	// starts draining in parallel with HTTP Shutdown. listen goroutine
	// unblocks as soon as Shutdown starts.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigChan
		slog.Info("shutdown signal received", "signal", sig.String())
		cancelRoot() // signal sweeper FIRST so its drain can race with HTTP Shutdown
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelShutdown()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("server shutdown error", "error", err)
		}
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
