package database

import (
	"fmt"
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

	if err := db.AutoMigrate(&core.SystemPrompt{}, &core.WorkerProfile{}, &core.ClientProfile{}, &core.Conversation{}, &core.Message{}); err != nil {
		return nil, fmt.Errorf("failed to migrate: %w", err)
	}

	// Ensure client_profile_prompt column exists (for existing DBs that pre-date this column)
	db.Exec(`ALTER TABLE system_prompts ADD COLUMN IF NOT EXISTS client_profile_prompt TEXT NOT NULL DEFAULT ''`)

	// Drop the old messages JSONB column (moved to separate messages table)
	db.Exec(`ALTER TABLE conversations DROP COLUMN IF EXISTS messages;`)
	// Drop the old title column (unused)
	db.Exec(`ALTER TABLE conversations DROP COLUMN IF EXISTS title;`)

	return db, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
