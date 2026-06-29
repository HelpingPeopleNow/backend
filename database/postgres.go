package database

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Connect initializes GORM, ensures the pgvector extension, and
// auto-migrates domain models (VECTOR_SEARCH_PLAN §9.1).
//
// Improvement #3 of the vector plan: AutoMigrate handles the
// worker_embeddings TABLE shape (column types); CREATE EXTENSION + HNSW
// index + updated_at trigger remain idempotent raw SQL since GORM
// can't model them.
func Connect() (*gorm.DB, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		host := getEnv("DB_HOST", "localhost")
		port := getEnv("DB_PORT", "5432")
		user := getEnv("DB_USER", "postgres")
		password := getEnv("DB_PASSWORD", "postgres")
		dbname := getEnv("DB_NAME", "helpingpeoplenow")
		sslmode := getEnv("DB_SSLMODE", "disable")

		dsn = fmt.Sprintf(
			"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
			host, port, user, password, dbname, sslmode,
		)
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// ── pgvector extension (must precede AutoMigrate for the
	//    worker_embeddings table to have vector(768) type available) ──
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS vector`).Error; err != nil {
		slog.Warn("migration: pgvector extension not available (vector search disabled)", "error", err)
	}

	// Domain models.
	if err := db.AutoMigrate(
		&core.SystemPrompt{},
		&core.WorkerProfile{},
		&core.ClientProfile{},
		&core.Conversation{},
		&core.Message{},
		&core.DirectConversation{},
		&core.DirectMessage{},
		&core.DirectMessageReport{},
		&core.WorkerEmbedding{},
	); err != nil {
		return nil, fmt.Errorf("failed to migrate: %w", err)
	}

	// Pin the embedding column to vector(768). AutoMigrate creates an
	// un-dim'd `vector` column because pgvector.Vector doesn't expose a
	// typed tag argument — the explicit ALTER adds the dim constraint for
	// DB-level safety. Conditional so:
	//   - green-field: takes the ACCESS EXCLUSIVE lock once, then no-ops
	//     on every subsequent startup (no per-boot table lock).
	//   - already pinned: the metadata check returns early, no ALTER.
	// WITHOUT this guard, the ALTER fires on every container restart and
	// blocks all reads/writes on a growing table.
	if err := db.Exec(`
DO $$
BEGIN
    IF (SELECT format_type(atttypid, atttypmod)
        FROM pg_attribute
        WHERE attrelid = 'worker_embeddings'::regclass
          AND attname = 'embedding') <> 'vector(768)' THEN
        ALTER TABLE worker_embeddings
            ALTER COLUMN embedding TYPE vector(768)
            USING embedding::vector(768);
    END IF;
END $$;
`).Error; err != nil {
		slog.Warn("migration: failed to pin worker_embeddings.embedding to vector(768)", "error", err)
	}

	// HNSW index (Pitfall #5 Phase A equivalent): cosine distance, m=16,
	// ef_construction=64. §6.3 defaults. CONCURRENTLY would be safer on
	// large populations, but startup runs are small (we ship ~6 workers);
	// if HNSW creation later blocks a hot start, switch to CONCURRENTLY
	// in a separate migration that runs at idle.
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_worker_embeddings_hnsw
			ON worker_embeddings
			USING hnsw (embedding vector_cosine_ops)
			WITH (m = 16, ef_construction = 64)
	`).Error; err != nil {
		slog.Warn("migration: failed to create HNSW index on worker_embeddings", "error", err)
	}

	// updated_at trigger — §6.4. Timestamps are timestamptz to align with
	// worker_profiles.updated_at in the FindStaleWorkerIDs comparison
	// (Plan showstopper #2 — int64 epoch vs timestamptz would break the
	// `wp.updated_at > MAX(we.updated_at)` predicate). Idempotent
	// (CREATE OR REPLACE).
	if err := db.Exec(`
		CREATE OR REPLACE FUNCTION update_worker_embedding_timestamp()
		RETURNS TRIGGER AS $$
		BEGIN
			NEW.updated_at = NOW();
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql
	`).Error; err != nil {
		slog.Warn("migration: failed to create update_worker_embedding_timestamp()", "error", err)
	}
	if err := db.Exec(`
		DROP TRIGGER IF EXISTS trg_worker_embeddings_updated ON worker_embeddings
	`).Error; err != nil {
		slog.Warn("migration: failed to drop existing trigger on worker_embeddings", "error", err)
	}
	if err := db.Exec(`
		CREATE TRIGGER trg_worker_embeddings_updated
			BEFORE UPDATE ON worker_embeddings
			FOR EACH ROW
			EXECUTE FUNCTION update_worker_embedding_timestamp()
	`).Error; err != nil {
		slog.Warn("migration: failed to create trg_worker_embeddings_updated", "error", err)
	}

	// ── Existing prompt column migrations (preserved for parity) ──
	if err := db.Exec(`ALTER TABLE system_prompts ADD COLUMN IF NOT EXISTS client_profile_prompt TEXT NOT NULL DEFAULT ''`).Error; err != nil {
		slog.Warn("migration: failed to add client_profile_prompt column", "error", err)
	}
	if err := db.Exec(`ALTER TABLE system_prompts ADD COLUMN IF NOT EXISTS find_trader_search_prompt TEXT NOT NULL DEFAULT ''`).Error; err != nil {
		slog.Warn("migration: failed to add find_trader_search_prompt column", "error", err)
	}
	if err := db.Exec(`ALTER TABLE system_prompts ADD COLUMN IF NOT EXISTS find_trader_presentation_prompt TEXT NOT NULL DEFAULT ''`).Error; err != nil {
		slog.Warn("migration: failed to add find_trader_presentation_prompt column", "error", err)
	}

	// Direct messaging migration — AutoMigrate creates tables if they don't exist
	// and adds missing columns/indexes on existing ones. No DROP so data persists
	// across restarts. The old role-based schema (client_id/worker_profile_id) was
	// migrated away from in June 2026 — the code below is the current schema.
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`).Error; err != nil {
		slog.Warn("migration: pgcrypto extension not available", "error", err)
	}

	if err := db.AutoMigrate(
		&core.DirectConversation{},
		&core.DirectMessage{},
		&core.DirectMessageReport{},
	); err != nil {
		slog.Warn("migration: failed to auto-migrate DM tables", "error", err)
	}

	// Drop old conversation_status enum type if it still exists (pre-June 2026 legacy)
	if err := db.Exec(`DROP TYPE IF EXISTS conversation_status`).Error; err != nil {
		slog.Warn("migration: failed to drop conversation_status type", "error", err)
	}

	// User-to-user indexes
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_direct_conv_a
			ON direct_conversations(user_a_id, last_message_at DESC)
			WHERE status = 'active'
	`).Error; err != nil {
		slog.Warn("migration: failed to create idx_direct_conv_a", "error", err)
	}

	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_direct_conv_b
			ON direct_conversations(user_b_id, last_message_at DESC)
			WHERE status = 'active'
	`).Error; err != nil {
		slog.Warn("migration: failed to create idx_direct_conv_b", "error", err)
	}

	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_direct_msg_conv_created
			ON direct_messages(conversation_id, created_at DESC)
	`).Error; err != nil {
		slog.Warn("migration: failed to create idx_direct_msg_conv_created", "error", err)
	}

	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_direct_msg_unread
			ON direct_messages(conversation_id, read_at)
			WHERE read_at IS NULL
	`).Error; err != nil {
		slog.Warn("migration: failed to create idx_direct_msg_unread", "error", err)
	}

	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_direct_conv_users
			ON direct_conversations(user_a_id, user_b_id)
	`).Error; err != nil {
		slog.Warn("migration: failed to create idx_direct_conv_users", "error", err)
	}

	if err := db.Exec(`
		DO $$ BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = 'fk_dm_sender'
			) THEN
				ALTER TABLE direct_messages
					ADD CONSTRAINT fk_dm_sender
					FOREIGN KEY (sender_id) REFERENCES "user"(id) ON DELETE CASCADE;
			END IF;
		END $$;
	`).Error; err != nil {
		slog.Warn("migration: failed to add fk_dm_sender constraint", "error", err)
	}

	if err := db.Exec(`DO $$ BEGIN
		IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='system_prompts' AND column_name='id' AND data_type='uuid') THEN
			ALTER TABLE system_prompts ADD COLUMN id_new integer DEFAULT 1;
			UPDATE system_prompts SET id_new = 1;
			ALTER TABLE system_prompts DROP CONSTRAINT IF EXISTS system_prompts_pkey;
			ALTER TABLE system_prompts DROP COLUMN id;
			ALTER TABLE system_prompts RENAME COLUMN id_new TO id;
			ALTER TABLE system_prompts ADD PRIMARY KEY (id);
		END IF;
	END $$;`).Error; err != nil {
		slog.Warn("migration: uuid-to-integer migration failed (may be expected on fresh DB)", "error", err)
	}

	// ── Slug backfill ──────────────────────────────────────────────────
	// Generate slugs for existing rows that have empty/null slugs.
	// Each slug includes a short UUID prefix (from the row's own id),
	// so collisions are structurally impossible.
	slog.Info("migration: backfilling empty worker slugs")
	if err := db.Exec(`
		UPDATE worker_profiles
		SET slug = LOWER(REGEXP_REPLACE(
			COALESCE(NULLIF(business_name, ''), NULLIF(profession, ''), 'worker') || '-' ||
			COALESCE(NULLIF(city, ''), 'unknown') || '-' ||
			SUBSTRING(REPLACE(id::text, '-', ''), 1, 8),
			'[^a-z0-9-]+', '-', 'gi'
		))
		WHERE slug IS NULL OR slug = ''
	`).Error; err != nil {
		slog.Warn("migration: failed to backfill worker slugs", "error", err)
	}

	// Unique partial index on slug — prevents future collision bugs at the DB level.
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_worker_profiles_slug_unique
		ON worker_profiles(slug) WHERE slug IS NOT NULL AND slug != ''
	`).Error; err != nil {
		slog.Warn("migration: failed to create unique slug index", "error", err)
	}

	return db, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
