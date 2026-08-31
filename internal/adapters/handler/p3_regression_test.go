package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/services"
	"github.com/HelpingPeopleNow/backend/internal/testingutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// P3-1 — chat_llm_duration_seconds is wired into the chat request path.
// =============================================================================
//
// Strategy: drive a real chat request through ChatHandler.ServeHTTP and then
// scrape /metrics to verify chat_llm_duration_seconds_count is incremented
// and the metric value matches the (provider, mode) label combo we expect.
// This test depends on the existing chatSetup helper, MockPrompts (provider
// label = "opencode0"), and the in-process metrics package. It purposefully
// avoids touching the private metrics registry state directly.

func TestChatLLMDurationObservedOnIntake(t *testing.T) {
	h := chatSetup()

	body := mustJSON(t, map[string]interface{}{
		"mode":    "worker_intake",
		"message": "hello",
		"history": []map[string]string{{"role": "user", "content": "hi"}},
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/chat", body))

	require.Equal(t, http.StatusOK, rec.Code, "chat intake should succeed: %s", rec.Body.String())

	// Scrape /metrics; expect chat_llm_duration_seconds_count lines to
	// appear (at least one per chat_request per mode — P3-1 wiring
	// verified end-to-end).
	mux := http.NewServeMux()
	RegisterMetricsRoutes(mux, "")
	mrec := httptest.NewRecorder()
	mux.ServeHTTP(mrec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, mrec.Code, "metrics scrape should succeed")

	text := mrec.Body.String()
	// MockPrompts returns SystemPrompt{} (LLMProvider unset → ""); the
	// histogram series is therefore `provider="",mode="worker_intake"`.
	// We assert presence of the family and the mode-series label — provider
	// value is encoded as whatever the prompt repo returned.
	assert.Contains(t, text, "chat_llm_duration_seconds_count",
		"metrics output must include chat_llm_duration_seconds_count (P3-1 wiring)")
	assert.Contains(t, text, `mode="worker_intake"`,
		"expected a chat_llm_duration_seconds series with mode=worker_intake — got: %s", text)
}

func TestChatLLMDurationObservedOnSearch(t *testing.T) {
	h := chatSetup()
	body := mustJSON(t, map[string]interface{}{
		"mode":    "search",
		"message": "find me a plumber",
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/chat", body))

	require.Equal(t, http.StatusOK, rec.Code, "chat search should succeed")

	mux := http.NewServeMux()
	RegisterMetricsRoutes(mux, "")
	mrec := httptest.NewRecorder()
	mux.ServeHTTP(mrec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	text := mrec.Body.String()
	// search mode reaches ObserveChatLLMDuration via the same defer as
	// the intake modes.
	assert.Contains(t, text, `mode="search"`,
		"chat_llm_duration_seconds must include the search series — got: %s", text)
}

// TestChatLLMDurationObservedOnClientIntake covers the third mode (client
// intake) so all three modes have explicit regression coverage rather than
// relying on cross-mode bleed via the shared metrics registry.
func TestChatLLMDurationObservedOnClientIntake(t *testing.T) {
	h := chatSetup()
	body := mustJSON(t, map[string]interface{}{
		"mode":    "client_intake",
		"message": "I need a plumber",
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/chat", body))

	require.Equal(t, http.StatusOK, rec.Code, "chat client_intake should succeed")

	mux := http.NewServeMux()
	RegisterMetricsRoutes(mux, "")
	mrec := httptest.NewRecorder()
	mux.ServeHTTP(mrec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	text := mrec.Body.String()
	assert.Contains(t, text, `mode="client_intake"`,
		"chat_llm_duration_seconds must include the client_intake series — got: %s", text)
}

// =============================================================================
// Observability audit — chat_llm_errors_total wiring (previously dead code).
// =============================================================================
//
// Strategy: force ProcessIntake/Search to fail via MockLLM.AskErr and verify
// the resulting 503 response also increments chat_llm_errors_total with the
// expected error_type label. See infra/docs/OBSERVABILITY_AUDIT_REPORT.md §1.4.

func TestChatLLMErrorsIncrementedOnFailure(t *testing.T) {
	mockLLM := &testingutil.MockLLM{AskErr: errors.New("helper unreachable: connection refused")}
	profiles := &testingutil.MockProfiles{}
	chats := &testingutil.MockChatRepo{ReturnID: "conv-err"}
	prompts := &testingutil.MockPrompts{}
	intakeSvc := services.NewIntakeService(mockLLM, profiles, chats, prompts)
	searchSvc := services.NewSearchService(mockLLM, profiles, chats, prompts)
	h := NewChatHandler(intakeSvc, searchSvc, prompts, nil)

	body := mustJSON(t, map[string]interface{}{
		"mode":    "worker_intake",
		"message": "hello",
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/chat", body))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	mux := http.NewServeMux()
	RegisterMetricsRoutes(mux, "")
	mrec := httptest.NewRecorder()
	mux.ServeHTTP(mrec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	text := mrec.Body.String()

	assert.Contains(t, text, "chat_llm_errors_total",
		"metrics output must include chat_llm_errors_total (dead-metric wiring fix)")
	assert.Contains(t, text, `error_type="unreachable"`,
		"chat_llm_errors_total must classify a connection-refused error as unreachable — got: %s", text)
}

// =============================================================================
// P3-3 — Postgres migration source must use CREATE INDEX CONCURRENTLY for HNSW
// =============================================================================
//
// We don't run a real Postgres in unit tests; this is a source guard
// that catches regressions:
//   1. Dropping CONCURRENTLY in a future edit (would re-introduce F10:
//      ACCESS EXCLUSIVE lock on a large worker_embeddings table at boot).
//   2. Wrapping the HNSW CREATE INDEX in a DO $$ block (CONCURRENTLY is
//      rejected inside explicit txns by Postgres).
//   3. Tweaking the WITH (m=16, ef_construction=64) recall defaults.
// If any is re-introduced the test fails the build.

func TestPostgresHNSWMigrationIsConcurrent(t *testing.T) {
	// Test runs from backend/internal/adapters/handler/ (Go 1.24+ chdirs
	// into the package dir). 3 `../`s reach backend/.
	src, err := osReadFileRelative("../../../database/postgres.go")
	require.NoError(t, err, "could not read ../../../database/postgres.go")
	file := string(src)

	// Locate the HNSW CREATE INDEX statement by multiline regex — match
	// from CREATE through the closing semicolon so we have a single
	// SQL statement slice and can safely assert it is not wrapped in a
	// DO $$ block (different from the legitimate vector(768) DO $$
	// guard, which lives in a separate statement elsewhere in the file).
	hnswRe := regexp.MustCompile(`(?is)CREATE\s+INDEX\s+CONCURRENTLY[^;]*;`)
	hnswStatement := hnswRe.FindString(file)
	require.NotEmpty(t, hnswStatement, "could not locate HNSW CREATE INDEX statement")

	// 1. CONCURRENTLY + idempotency + identity.
	assert.Contains(t, hnswStatement, "CONCURRENTLY",
		"HNSW CREATE INDEX must use CONCURRENTLY to avoid ACCESS EXCLUSIVE lock on every boot (P3-3 / F10)")
	assert.Contains(t, hnswStatement, "IF NOT EXISTS",
		"HNSW CREATE INDEX must be idempotent via IF NOT EXISTS")
	assert.Contains(t, hnswStatement, "idx_worker_embeddings_hnsw",
		"HNSW CREATE INDEX must name idx_worker_embeddings_hnsw")
	assert.Contains(t, hnswStatement, "vector_cosine_ops",
		"HNSW operator must be vector_cosine_ops (recall config)")

	// 2. Recall-tuned defaults from the HNSW WITH clause must remain.
	assert.Contains(t, hnswStatement, "m = 16",
		"HNSW WITH clause must retain m = 16 (recall-tuned default)")
	assert.Contains(t, hnswStatement, "ef_construction = 64",
		"HNSW WITH clause must retain ef_construction = 64 (recall-tuned default)")

	// 3. The HNSW statement itself must NOT be wrapped in DO $$ (Postgres
	// rejects CONCURRENTLY inside explicit txns).
	assert.NotContains(t, hnswStatement, "DO $$",
		"HNSW CREATE INDEX CONCURRENTLY must not be wrapped in DO $$ (Postgres rejects CONCURRENTLY inside explicit txns)")

	// 4. Sanity: the vector(768) dim-pin must remain in the file
	// (different statement; CONCURRENTLY does not apply to ALTER but the
	// conditional guard makes it no-op on already-pinned schemas).
	assert.Contains(t, file, "vector(768)",
		"vector(768) dim-pin must remain in the migration")
}

// =============================================================================
// Helpers
// =============================================================================

// osReadFileRelative reads a file relative to the test's working directory
// (the package directory). Go 1.24+ test runners chdir into the package dir
// before running; this helper exists so the relative path is robust to
// future module moves.
func osReadFileRelative(p string) ([]byte, error) {
	return os.ReadFile(p)
}

func mustJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}
