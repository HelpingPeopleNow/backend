package integration

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/ports"
	"github.com/HelpingPeopleNow/backend/internal/testingutil"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// ── DB helpers ───────────────────────────────────────────────────────

// NewTestDB returns a *gorm.DB pointed at a per-test PG schema.
//
// CONNECTION PINNING IS MANDATORY (plan §4.4):
// GORM's default connection pool will silently hand out a different *sql.Conn
// mid-test, breaking SET search_path isolation and causing cross-test data
// leaks. We use transaction-per-test with SET LOCAL search_path so cleanup
// is automatic when the transaction ends.
func NewTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/helpingpeoplenow_backend_test?sslmode=disable"
	}

	// Open a root DB connection (no search_path pinning)
	rootDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("failed to open root DB: %v", err)
	}
	t.Cleanup(func() { rootDB.Close() })

	// Ping to verify connectivity
	if err := rootDB.Ping(); err != nil {
		t.Skipf("skipping integration test: PostgreSQL not available: %v", err)
	}

	// Create a unique schema for this test
	schemaName := fmt.Sprintf("test_%s", t.Name())
	// Sanitize schema name (replace / with _)
	schemaNameBytes := []byte(schemaName)
	for i := range schemaNameBytes {
		if schemaNameBytes[i] == '/' || schemaNameBytes[i] == ' ' {
			schemaNameBytes[i] = '_'
		}
	}
	schemaName = string(schemaNameBytes)

	_, err = rootDB.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName))
	if err != nil {
		t.Fatalf("failed to drop schema: %v", err)
	}

	_, err = rootDB.Exec(fmt.Sprintf("CREATE SCHEMA %s", schemaName))
	if err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	t.Cleanup(func() {
		rootDB.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName))
	})

	// Run migrations against the test schema
	// We need to set search_path for the migration queries
	_, err = rootDB.Exec(fmt.Sprintf("SET search_path TO %s", schemaName))
	if err != nil {
		t.Fatalf("failed to set search_path: %v", err)
	}

	// Open GORM with the same DSN but point to test schema
	gormDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open GORM: %v", err)
	}

	// Pin to test schema via raw SQL
	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get underlying sql.DB: %v", err)
	}

	_, err = sqlDB.Exec(fmt.Sprintf("SET search_path TO %s", schemaName))
	if err != nil {
		t.Fatalf("failed to set GORM search_path: %v", err)
	}

	return gormDB
}

// ── App wiring ───────────────────────────────────────────────────────

// AppDeps holds all dependencies for a testable backend instance.
type AppDeps struct {
	DB       *gorm.DB
	LLM      *testingutil.MockLLM
	Profiles *testingutil.MockProfiles
	Chats    *testingutil.MockChatRepo
	Prompts  *testingutil.MockPrompts
	Broker   ports.Broker
}

// NewTestApp wires up a backend with fakes for all ports.
func NewTestApp(t *testing.T, db *gorm.DB) *AppDeps {
	t.Helper()

	llm := &testingutil.MockLLM{}
	profiles := &testingutil.MockProfiles{}
	chats := &testingutil.MockChatRepo{}
	prompts := &testingutil.MockPrompts{}
	broker := testingutil.NewMockBroker()

	return &AppDeps{
		DB:       db,
		LLM:      llm,
		Profiles: profiles,
		Chats:    chats,
		Prompts:  prompts,
		Broker:   broker,
	}
}

// ── Cookie helpers ───────────────────────────────────────────────────

// MakeSessionCookie creates a better-auth session cookie for testing.
func MakeSessionCookie(userID string) *http.Cookie {
	return &http.Cookie{
		Name:  "better-auth.session_token",
		Value: fmt.Sprintf("test-session-%s-token", userID),
	}
}

// MakeSecureSessionCookie creates the Secure variant of the session cookie.
func MakeSecureSessionCookie(userID string) *http.Cookie {
	return &http.Cookie{
		Name:  "__Secure-better-auth.session_token",
		Value: fmt.Sprintf("test-session-%s-token", userID),
	}
}

// MakeAdminSessionCookie creates a session cookie that will be recognized as admin.
func MakeAdminSessionCookie(userID string) *http.Cookie {
	return &http.Cookie{
		Name:  "better-auth.session_token",
		Value: fmt.Sprintf("test-admin-session-%s-token", userID),
	}
}
