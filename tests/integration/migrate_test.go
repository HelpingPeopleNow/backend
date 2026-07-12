//go:build integration

package integration

import (
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/adapters/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ── Migration / schema integration tests ────────────────────────────────
// Verify schema has expected tables after AutoMigrate.

// expectedTables lists the tables that the backend schema should have.
var expectedTables = []string{
	"worker_profiles",
	"client_profiles",
	"conversations",
	"messages",
	"direct_conversations",
	"direct_messages",
	"system_prompts",
	"worker_embeddings",
	"session",
}

func TestSchemaHasExpectedTables(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	// Also migrate worker_embeddings (not in migrateTestSchema)
	err := db.AutoMigrate(&WorkerEmbeddingForTest{})
	// It's OK if this fails — the table might already exist or pgvector
	// might not be available in the test DB. The key assertions below
	// check for table existence.
	_ = err

	for _, table := range expectedTables {
		t.Run(table, func(t *testing.T) {
			assert.True(t, db.Migrator().HasTable(table),
				"expected table %q to exist", table)
		})
	}
}

func TestSchemaWorkerProfileColumns(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	require.True(t, db.Migrator().HasTable("worker_profiles"))

	// Verify key columns exist by inserting and reading
	profileRepo := repository.NewGormProfileRepository(db)
	err := profileRepo.UpsertWorkerProfile(t.Context(), "schema-test-w1", map[string]interface{}{
		"profession":  "Plumber",
		"city":        "Madrid",
		"hourly_rate": 50.0,
	})
	require.NoError(t, err)

	wp, err := profileRepo.GetWorkerProfile(t.Context(), "schema-test-w1")
	require.NoError(t, err)
	require.NotNil(t, wp)
	assert.NotEmpty(t, wp.ID, "worker_profiles should have UUID primary key")
}

func TestSchemaConversationUUID(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	chatRepo := repository.NewGormChatRepository(db)
	convID, err := chatRepo.SaveMessages(t.Context(), "schema-test-conv", "worker", "test", "reply", "", nil, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, convID, "conversations should generate UUID")

	// Verify the conversation was persisted
	conv, err := chatRepo.GetConversation(t.Context(), "schema-test-conv", convID)
	require.NoError(t, err)
	require.NotNil(t, conv)
}

func TestSchemaDirectConversationUnique(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	profileRepo := repository.NewGormProfileRepository(db)
	require.NoError(t, profileRepo.UpsertWorkerProfile(t.Context(), "schema-dm-w1", map[string]interface{}{
		"profession": "Plumber",
	}))
	wp, err := profileRepo.GetWorkerProfile(t.Context(), "schema-dm-w1")
	require.NoError(t, err)

	dmRepo := repository.NewGormDirectMessageRepository(db)

	// Create first conversation
	_, isNew, err := dmRepo.GetOrCreateConversation(t.Context(), "schema-dm-c1", "user", wp.ID, "user")
	require.NoError(t, err)
	assert.True(t, isNew)

	// Create second with same pair — should not be new
	_, isNew2, err := dmRepo.GetOrCreateConversation(t.Context(), "schema-dm-c1", "user", wp.ID, "user")
	require.NoError(t, err)
	assert.False(t, isNew2)
}

func TestSchemaSystemPromptSingleton(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	promptRepo := repository.NewGormSystemPromptRepository(db)
	sp, err := promptRepo.Get(t.Context())
	require.NoError(t, err)
	require.NotNil(t, sp)
	assert.Equal(t, uint(1), sp.ID, "system_prompts should have singleton row with id=1")

	// Update should not create a new row
	_, err = promptRepo.Update(t.Context(), "worker_profile_prompt", "test prompt")
	require.NoError(t, err)

	sp2, err := promptRepo.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, uint(1), sp2.ID)
}

// WorkerEmbeddingForTest is a minimal struct to trigger GORM AutoMigrate
// for the worker_embeddings table. It mirrors core.WorkerEmbedding but
// avoids importing pgvector in the test file.
type WorkerEmbeddingForTest struct {
	UserID    string `gorm:"type:text;primaryKey;not null"`
	FieldName string `gorm:"type:text;primaryKey;not null"`
	Embedding []byte `gorm:"type:vector;not null"`
	Model     string `gorm:"type:text;not null;default:'granite-embedding:278m'"`
	TextHash  string `gorm:"type:text;not null"`
}

func (WorkerEmbeddingForTest) TableName() string { return "worker_embeddings" }

// Ensure gorm.DB is referenced (used in NewTestDB).
var _ *gorm.DB
