//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/adapters/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ── Auth resolution integration tests ───────────────────────────────────
// Auth middleware with mock auth service returning userId, fallback to DB
// session.

// TestAuthFallbackToDB verifies that when the auth service is unreachable,
// the middleware falls back to DB session lookup.
func TestAuthFallbackToDB(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	// Insert a session row into the "session" table (better-auth schema).
	insertTestSession(t, db, "test-user-1", "test-session-token-abc")

	// Auth middleware with empty auth service URL (simulates unreachable)
	auth := middleware.NewAuthMiddleware("", db)

	// Simple handler that echoes the user ID
	echoHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value("user_id")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"user_id": userID,
		})
	})

	wrapped := auth.Wrap(echoHandler)

	// Request with session cookie
	req := httptest.NewRequest(http.MethodGet, "/api/v1/worker/profile", nil)
	req.AddCookie(&http.Cookie{
		Name:  "better-auth.session_token",
		Value: "test-session-token-abc",
	})
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "test-user-1", resp["user_id"])
}

// TestAuthNoCookieReturns401 verifies that without a session cookie, the
// middleware returns 401.
func TestAuthNoCookieReturns401(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	auth := middleware.NewAuthMiddleware("", db)
	echoHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := auth.Wrap(echoHandler)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/worker/profile", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestAuthExpiredSessionReturns401 verifies that an expired session is rejected.
func TestAuthExpiredSessionReturns401(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	// Insert an expired session
	insertExpiredTestSession(t, db, "test-user-expired", "expired-token-xyz")

	auth := middleware.NewAuthMiddleware("", db)
	echoHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := auth.Wrap(echoHandler)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  "better-auth.session_token",
		Value: "expired-token-xyz",
	})
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestAuthSecureCookieFallback verifies the middleware checks both
// __Secure-better-auth.session_token and better-auth.session_token.
func TestAuthSecureCookieFallback(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	insertTestSession(t, db, "test-user-secure", "secure-token-123")

	auth := middleware.NewAuthMiddleware("", db)
	echoHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value("user_id")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"user_id": userID,
		})
	})

	wrapped := auth.Wrap(echoHandler)

	// Use the Secure cookie variant
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  "__Secure-better-auth.session_token",
		Value: "secure-token-123",
	})
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "test-user-secure", resp["user_id"])
}

// TestAuthWithRealMux verifies auth works end-to-end with the real mux
// handler stack.
func TestAuthWithRealMux(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	insertTestSession(t, db, "mux-user-1", "mux-session-token")

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	// Wrap the mux with auth middleware
	auth := middleware.NewAuthMiddleware("", db)
	wrappedMux := auth.Wrap(mux)

	// Request with valid session — should get through to handler
	req := httptest.NewRequest(http.MethodGet, "/api/v1/worker/profile", nil)
	req.AddCookie(&http.Cookie{
		Name:  "better-auth.session_token",
		Value: "mux-session-token",
	})
	w := httptest.NewRecorder()
	wrappedMux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Request without session — should get 401
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/worker/profile", nil)
	w2 := httptest.NewRecorder()
	wrappedMux.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusUnauthorized, w2.Code)
}

// ── Test helpers ────────────────────────────────────────────────────────

// insertTestSession creates a session row in the "session" table with a
// far-future expiry so the DB fallback resolves it.
func insertTestSession(t *testing.T, db *gorm.DB, userID, token string) {
	t.Helper()
	err := db.Exec(`
		INSERT INTO "session" (id, "userId", token, "expiresAt", "createdAt", "updatedAt")
		VALUES (gen_random_uuid(), ?, ?, NOW() + INTERVAL '1 hour', NOW(), NOW())
	`, userID, token).Error
	require.NoError(t, err, "insert test session")
}

// insertExpiredTestSession creates an expired session row.
func insertExpiredTestSession(t *testing.T, db *gorm.DB, userID, token string) {
	t.Helper()
	err := db.Exec(`
		INSERT INTO "session" (id, "userId", token, "expiresAt", "createdAt", "updatedAt")
		VALUES (gen_random_uuid(), ?, ?, NOW() - INTERVAL '1 hour', NOW() - INTERVAL '2 hours', NOW() - INTERVAL '2 hours')
	`, userID, token).Error
	require.NoError(t, err, "insert expired test session")
}
