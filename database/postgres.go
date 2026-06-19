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

// Connect initializes GORM and auto-migrates domain models.
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

	if err := db.AutoMigrate(&core.SystemPrompt{}, &core.WorkerProfile{}, &core.ClientProfile{}, &core.Conversation{}, &core.Message{}, &core.DirectConversation{}, &core.DirectMessage{}); err != nil {
		return nil, fmt.Errorf("failed to migrate: %w", err)
	}

	// Ensure client_profile_prompt column exists (for existing DBs that pre-date this column)
	if err := db.Exec(`ALTER TABLE system_prompts ADD COLUMN IF NOT EXISTS client_profile_prompt TEXT NOT NULL DEFAULT ''`).Error; err != nil {
		slog.Warn("migration: failed to add client_profile_prompt column", "error", err)
	}
	// Ensure find_trader_search_prompt column exists (for existing DBs that pre-date this column)
	if err := db.Exec(`ALTER TABLE system_prompts ADD COLUMN IF NOT EXISTS find_trader_search_prompt TEXT NOT NULL DEFAULT ''`).Error; err != nil {
		slog.Warn("migration: failed to add find_trader_search_prompt column", "error", err)
	}
	// Ensure find_trader_presentation_prompt column exists (for existing DBs that pre-date this column)
	if err := db.Exec(`ALTER TABLE system_prompts ADD COLUMN IF NOT EXISTS find_trader_presentation_prompt TEXT NOT NULL DEFAULT ''`).Error; err != nil {
		slog.Warn("migration: failed to add find_trader_presentation_prompt column", "error", err)
	}

	// Direct messaging migrations (idempotent)
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`).Error; err != nil {
		slog.Warn("migration: pgcrypto extension not available", "error", err)
	}

	if err := db.Exec(`
		DO $$ BEGIN
			CREATE TYPE conversation_status AS ENUM ('active', 'archived', 'blocked');
		EXCEPTION WHEN duplicate_object THEN NULL;
		END $$;
	`).Error; err != nil {
		slog.Warn("migration: failed to create conversation_status enum", "error", err)
	}

	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_direct_conv_client
			ON direct_conversations(client_id, last_message_at DESC)
			WHERE status = 'active'
	`).Error; err != nil {
		slog.Warn("migration: failed to create idx_direct_conv_client", "error", err)
	}

	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_direct_conv_worker
			ON direct_conversations(worker_profile_id, last_message_at DESC)
			WHERE status = 'active'
	`).Error; err != nil {
		slog.Warn("migration: failed to create idx_direct_conv_worker", "error", err)
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
		DO $$ BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = 'unique_client_worker'
			) THEN
				ALTER TABLE direct_conversations
					ADD CONSTRAINT unique_client_worker
					UNIQUE (client_id, worker_profile_id);
			END IF;
		END $$;
	`).Error; err != nil {
		slog.Warn("migration: failed to add unique_client_worker constraint", "error", err)
	}

	if err := db.Exec(`
		DO $$ BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = 'fk_sender_user'
			) THEN
				ALTER TABLE direct_messages
					ADD CONSTRAINT fk_sender_user
					FOREIGN KEY (sender_id) REFERENCES "user"(id) ON DELETE CASCADE;
			END IF;
		END $$;
	`).Error; err != nil {
		slog.Warn("migration: failed to add fk_sender_user constraint", "error", err)
	}

	// Migrate system_prompts id from uuid to integer singleton (id=1)
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

	return db, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
