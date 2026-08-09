package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// openIsolatedSchemaDB opens DATABASE_URL, pins the pool to a single
// connection, and switches it to a private schema (cascade_test_<pid>) so the
// test never touches public tables and cannot collide with other packages'
// tests sharing the same database. t.Cleanup resets search_path to public and
// drops the schema.
func openIsolatedSchemaDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping DB-backed cascade test")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err, "gorm.Open(DATABASE_URL)")

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1) // search_path must stick to the one pooled conn

	schema := fmt.Sprintf("cascade_test_%d", os.Getpid())
	require.NoError(t, db.Exec("DROP SCHEMA IF EXISTS "+schema+" CASCADE").Error)
	require.NoError(t, db.Exec("CREATE SCHEMA "+schema).Error)
	require.NoError(t, db.Exec("SET search_path TO "+schema).Error)

	t.Cleanup(func() {
		_ = db.Exec("SET search_path TO public").Error
		_ = db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error
		_ = sqlDB.Close()
	})
	return db
}

// TestDeleteUserCascade is a DB-backed integration test for CMP-01 (audit):
// DELETE /api/v1/admin/users/<id> must remove the user AND every referencing
// row (profiles, embeddings, conversations, messages, DMs, reports, feedback)
// in one transaction — leaving zero orphans. Soft-deleted direct_messages
// (gorm.DeletedAt tombstones) must be physically purged too: a GDPR erasure
// cannot leave PII behind. Requires DATABASE_URL (CI provides a postgres:16
// service container); skips gracefully otherwise.
func TestDeleteUserCascade(t *testing.T) {
	db := openIsolatedSchemaDB(t)

	// Minimal schema — only the columns the cascade touches. Deliberately NOT
	// the full production models (pgvector extension etc. would force setup).
	for _, stmt := range []string{
		`CREATE TABLE "user" (id text PRIMARY KEY)`,
		`CREATE TABLE worker_profiles (id uuid PRIMARY KEY, user_id text)`,
		`CREATE TABLE client_profiles (id uuid PRIMARY KEY, user_id text)`,
		`CREATE TABLE worker_embeddings (user_id text PRIMARY KEY)`,
		`CREATE TABLE conversations (id uuid PRIMARY KEY, user_id text)`,
		`CREATE TABLE messages (id uuid PRIMARY KEY, conversation_id uuid)`,
		`CREATE TABLE direct_conversations (id uuid PRIMARY KEY, user_a_id text, user_b_id text)`,
		`CREATE TABLE direct_messages (id uuid PRIMARY KEY, conversation_id uuid, sender_id text, deleted_at timestamptz)`,
		`CREATE TABLE direct_message_reports (id uuid PRIMARY KEY, conversation_id uuid, reported_by text)`,
		`CREATE TABLE feedback (id uuid PRIMARY KEY, user_id text)`,
	} {
		require.NoError(t, db.Exec(stmt).Error, "create table: %s", stmt)
	}

	const victim = "u-victim"
	const other = "u-other"

	seed := []string{
		`INSERT INTO "user" (id) VALUES ('` + victim + `'), ('` + other + `')`,
		`INSERT INTO worker_profiles (id, user_id) VALUES (gen_random_uuid(), '` + victim + `'), (gen_random_uuid(), '` + other + `')`,
		`INSERT INTO client_profiles (id, user_id) VALUES (gen_random_uuid(), '` + victim + `')`,
		`INSERT INTO worker_embeddings (user_id) VALUES ('` + victim + `')`,
		`INSERT INTO feedback (id, user_id) VALUES (gen_random_uuid(), '` + victim + `'), (gen_random_uuid(), '` + other + `')`,
		// victim's chat conversations + messages
		`INSERT INTO conversations (id, user_id) VALUES (gen_random_uuid(), '` + victim + `'), (gen_random_uuid(), '` + victim + `'), (gen_random_uuid(), '` + other + `')`,
		`INSERT INTO messages (id, conversation_id) SELECT gen_random_uuid(), id FROM conversations WHERE user_id = '` + victim + `'`,
		// victim's DMs: one conversation as user_a, one as user_b (live + soft-deleted messages)
		`INSERT INTO direct_conversations (id, user_a_id, user_b_id) VALUES (gen_random_uuid(), '` + victim + `', 'x-other-a'), (gen_random_uuid(), 'x-other-b', '` + victim + `')`,
		`INSERT INTO direct_messages (id, conversation_id, sender_id, deleted_at) SELECT gen_random_uuid(), id, '` + victim + `', NULL FROM direct_conversations WHERE user_a_id = '` + victim + `'`,
		`INSERT INTO direct_messages (id, conversation_id, sender_id, deleted_at) SELECT gen_random_uuid(), id, 'x-other-b', now() FROM direct_conversations WHERE user_b_id = '` + victim + `'`,
		`INSERT INTO direct_message_reports (id, conversation_id, reported_by) SELECT gen_random_uuid(), id, '` + victim + `' FROM direct_conversations WHERE user_a_id = '` + victim + `'`,
	}
	for _, stmt := range seed {
		require.NoError(t, db.Exec(stmt).Error, "seed: %s", stmt)
	}

	h := NewAdminHandler(db)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/"+victim, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "cascade delete should succeed: %s", rec.Body.String())

	counts := map[string]string{
		`"user"`:                 "SELECT count(*) FROM \"user\" WHERE id = '" + victim + "'",
		"worker_profiles":        "SELECT count(*) FROM worker_profiles WHERE user_id = '" + victim + "'",
		"client_profiles":        "SELECT count(*) FROM client_profiles WHERE user_id = '" + victim + "'",
		"worker_embeddings":      "SELECT count(*) FROM worker_embeddings WHERE user_id = '" + victim + "'",
		"conversations":          "SELECT count(*) FROM conversations WHERE user_id = '" + victim + "'",
		"messages":               "SELECT count(*) FROM messages WHERE conversation_id IN (SELECT id FROM conversations WHERE user_id = '" + victim + "')",
		"direct_conversations":   "SELECT count(*) FROM direct_conversations WHERE user_a_id = '" + victim + "' OR user_b_id = '" + victim + "'",
		"direct_messages":        "SELECT count(*) FROM direct_messages WHERE conversation_id IN (SELECT id FROM direct_conversations WHERE user_a_id = '" + victim + "' OR user_b_id = '" + victim + "')",
		"direct_message_reports": "SELECT count(*) FROM direct_message_reports WHERE reported_by = '" + victim + "'",
		"feedback":               "SELECT count(*) FROM feedback WHERE user_id = '" + victim + "'",
	}
	for table, q := range counts {
		var n int64
		require.NoError(t, db.Raw(q).Scan(&n).Error, "count %s", table)
		assert.Zero(t, n, "orphan rows left in %s after user cascade delete", table)
	}

	// Control: the other user and their rows must survive untouched.
	var otherUser int64
	require.NoError(t, db.Raw("SELECT count(*) FROM \"user\" WHERE id = '"+other+"'").Scan(&otherUser).Error)
	assert.Equal(t, int64(1), otherUser, "control user must survive")
	var otherProfiles, otherFeedback, otherConvs int64
	require.NoError(t, db.Raw("SELECT count(*) FROM worker_profiles WHERE user_id = '"+other+"'").Scan(&otherProfiles).Error)
	require.NoError(t, db.Raw("SELECT count(*) FROM feedback WHERE user_id = '"+other+"'").Scan(&otherFeedback).Error)
	require.NoError(t, db.Raw("SELECT count(*) FROM conversations WHERE user_id = '"+other+"'").Scan(&otherConvs).Error)
	assert.Equal(t, int64(1), otherProfiles, "control worker profile must survive")
	assert.Equal(t, int64(1), otherFeedback, "control feedback must survive")
	assert.Equal(t, int64(1), otherConvs, "control conversation must survive")
}

// TestDeleteUserCascadeMissingUser: deleting a non-existent user must 404
// (same contract as the plain deleteRow path).
func TestDeleteUserCascadeMissingUser(t *testing.T) {
	db := openIsolatedSchemaDB(t)

	require.NoError(t, db.Exec(`CREATE TABLE "user" (id text PRIMARY KEY)`).Error)

	h := NewAdminHandler(db)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/no-such-user", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
