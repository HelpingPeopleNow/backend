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
		Auth:        middleware.NewAuthMiddleware(os.Getenv("AUTH_SERVICE_URL"), db),
		Admin:       middleware.NewAdminMiddleware(os.Getenv("AUTH_SERVICE_URL")),
	}
}

func buildMux(d appDeps) *http.ServeMux {
	mux := http.NewServeMux()

	mux.Handle("/health", handler.NewHealthHandler(d.DB, d.LLM))
	mux.Handle("/livez", handler.NewHealthHandler(d.DB, d.LLM))

	mux.Handle("/api/v1/chat", middleware.CORS(d.Auth.Wrap(handler.NewChatHandler(d.Intake, d.Search, d.PromptRepo))))
	mux.Handle("/api/v1/worker/profile", middleware.CORS(d.Auth.Wrap(handler.NewWorkerHandler(d.ProfileRepo))))
	mux.Handle("/api/v1/client/profile", middleware.CORS(d.Auth.Wrap(handler.NewClientHandler(d.ProfileRepo))))
	mux.Handle("/api/v1/conversations", middleware.CORS(d.Auth.Wrap(handler.NewConversationHandler(d.ChatRepo))))
	mux.Handle("/api/v1/conversations/", middleware.CORS(d.Auth.Wrap(handler.NewConversationHandler(d.ChatRepo))))

	broker := realtime.NewSSEBroker()
	dmRateLimiter := ratelimit.NewRateLimiter(30, time.Minute)
	dmHandler := handler.NewDirectMessagingHandler(d.DMRepo, d.ProfileRepo, broker, dmRateLimiter)
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

	handler.RegisterMetricsRoutes(mux)
	return mux
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
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

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

	server := &http.Server{
		Addr:    ":" + port,
		Handler: middleware.Logging(mux),
	}

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
