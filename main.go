package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
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

	mux.Handle("/api/v1/chat", middleware.CORS(d.Auth.Wrap(handler.NewChatHandler(d.Intake, d.Search, d.PromptRepo))))
	mux.Handle("/api/v1/worker/profile", middleware.CORS(d.Auth.Wrap(handler.NewWorkerHandler(d.ProfileRepo))))
	mux.Handle("/api/v1/client/profile", middleware.CORS(d.Auth.Wrap(handler.NewClientHandler(d.ProfileRepo))))
	mux.Handle("/api/v1/conversations", middleware.CORS(d.Auth.Wrap(handler.NewConversationHandler(d.ChatRepo))))
	mux.Handle("/api/v1/conversations/", middleware.CORS(d.Auth.Wrap(handler.NewConversationHandler(d.ChatRepo))))

	// Direct messaging routes
	broker := realtime.NewSSEBroker()
	dmRateLimiter := ratelimit.NewRateLimiter(30, time.Minute)
	dmHandler := handler.NewDirectMessagingHandler(d.DMRepo, d.ProfileRepo, broker, dmRateLimiter)
	mux.Handle("/api/v1/workers/", middleware.CORS(d.Auth.Wrap(dmHandler)))
	mux.Handle("/api/v1/direct-messages", middleware.CORS(d.Auth.Wrap(dmHandler)))
	mux.Handle("/api/v1/direct-messages/", middleware.CORS(d.Auth.Wrap(dmHandler)))

	mux.Handle("/api/v1/system-prompts", middleware.CORS(d.Auth.Wrap(handler.NewSystemPromptHandler(d.PromptRepo))))
	mux.Handle("/api/v1/system-prompts/", middleware.CORS(d.Auth.Wrap(d.Admin.Wrap(handler.NewSystemPromptHandler(d.PromptRepo)))))

	mux.Handle("/api/v1/admin/", middleware.CORS(d.Auth.Wrap(d.Admin.Wrap(handler.NewAdminHandler(d.DB)))))

	handler.RegisterMetricsRoutes(mux)
	return mux
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

	slog.Info("listening", "addr", ":"+port)
	if err := http.ListenAndServe(":"+port, middleware.Logging(mux)); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
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
