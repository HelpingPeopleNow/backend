//go:build integration

package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/adapters/handler"
	"github.com/HelpingPeopleNow/backend/internal/adapters/ratelimit"
	"github.com/HelpingPeopleNow/backend/internal/adapters/realtime"
	"github.com/HelpingPeopleNow/backend/internal/adapters/repository"
	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/HelpingPeopleNow/backend/internal/ports"
	"github.com/HelpingPeopleNow/backend/internal/services"
	"github.com/HelpingPeopleNow/backend/internal/testingutil"
	"gorm.io/gorm"
)

// buildIntegrationMux wires a full handler stack with real GORM repos and a fake
// LLM. It does NOT apply auth or admin middleware — callers that need
// authentication context should wrap the returned handler with fakeAuth.
func buildIntegrationMux(t *testing.T, db *gorm.DB, llm ports.LLMService) http.Handler {
	t.Helper()

	chatRepo := repository.NewGormChatRepository(db)
	profileRepo := repository.NewGormProfileRepository(db)
	promptRepo := repository.NewGormSystemPromptRepository(db)
	dmRepo := repository.NewGormDirectMessageRepository(db)

	intakeSvc := services.NewIntakeService(llm, profileRepo, chatRepo, promptRepo)
	searchSvc := services.NewSearchService(llm, profileRepo, chatRepo, promptRepo)

	broker := realtime.NewSSEBroker()
	dmLimiter := ratelimit.NewRateLimiter(30, time.Minute)

	chatHandler := handler.NewChatHandler(intakeSvc, searchSvc, promptRepo)
	workerHandler := handler.NewWorkerHandler(profileRepo)
	clientHandler := handler.NewClientHandler(profileRepo)
	convHandler := handler.NewConversationHandler(chatRepo)
	spHandler := handler.NewSystemPromptHandler(promptRepo)
	dmHandler := handler.NewDirectMessagingHandler(dmRepo, profileRepo, broker, dmLimiter)
	adminHandler := handler.NewAdminHandler(db)

	mux := http.NewServeMux()
	mux.Handle("/api/v1/chat", chatHandler)
	mux.Handle("/api/v1/worker/profile", workerHandler)
	mux.Handle("/api/v1/client/profile", clientHandler)
	mux.Handle("/api/v1/conversations", convHandler)
	mux.Handle("/api/v1/conversations/", convHandler)
	mux.Handle("/api/v1/system-prompts", spHandler)
	mux.Handle("/api/v1/system-prompts/", spHandler)
	mux.Handle("/api/v1/workers/", dmHandler)
	mux.Handle("/api/v1/direct-messages", dmHandler)
	mux.Handle("/api/v1/direct-messages/", dmHandler)
	mux.Handle("/api/v1/admin/", adminHandler)

	return mux
}

// fakeAuth returns middleware that injects a user ID into the request context
// without checking any session. Used by integration tests that bypass the real
// auth middleware and test the handler → service → repository → DB path.
func fakeAuth(userID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := contextkeys.SetUserID(r.Context(), userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// newFakeLLM creates a MockLLM with the given canned answer.
func newFakeLLM(answer string) *testingutil.MockLLM {
	return &testingutil.MockLLM{Answer: answer}
}
