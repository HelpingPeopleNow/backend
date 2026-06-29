package integration

import (
	"context"
	"testing"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/adapters/repository"
	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// migrateTestSchema runs AutoMigrate for all models against the test schema.
func migrateTestSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	err := db.AutoMigrate(
		&core.WorkerProfile{},
		&core.ClientProfile{},
		&core.Conversation{},
		&core.Message{},
		&core.SystemPrompt{},
		&core.DirectConversation{},
		&core.DirectMessage{},
	)
	require.NoError(t, err, "AutoMigrate failed")
}

// ── Worker Profile CRUD ──────────────────────────────────────────

func TestWorkerProfileCreateAndGet(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	repo := repository.NewGormProfileRepository(db)
	ctx := context.Background()

	fields := map[string]interface{}{
		"profession":  "Plumber",
		"city":        "Madrid",
		"hourly_rate": 45.0,
		"bio":         "10 years fixing pipes",
		"phone":       "+34600000001",
	}

	err := repo.UpsertWorkerProfile(ctx, "user-w1", fields)
	require.NoError(t, err)

	wp, err := repo.GetWorkerProfile(ctx, "user-w1")
	require.NoError(t, err)
	require.NotNil(t, wp)
	assert.Equal(t, "user-w1", wp.UserID)
	assert.Equal(t, "Plumber", wp.Profession)
	assert.Equal(t, "Madrid", wp.City)
	assert.Equal(t, 45.0, wp.HourlyRate)
	assert.Equal(t, "10 years fixing pipes", wp.Bio)
	assert.Equal(t, "+34600000001", wp.Phone)
}

func TestWorkerProfileUpdate(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	repo := repository.NewGormProfileRepository(db)
	ctx := context.Background()

	// Create
	err := repo.UpsertWorkerProfile(ctx, "user-w2", map[string]interface{}{
		"profession": "Electrician",
		"city":       "Barcelona",
	})
	require.NoError(t, err)

	// Update
	err = repo.UpsertWorkerProfile(ctx, "user-w2", map[string]interface{}{
		"hourly_rate": 55.0,
		"city":        "Valencia",
	})
	require.NoError(t, err)

	wp, err := repo.GetWorkerProfile(ctx, "user-w2")
	require.NoError(t, err)
	require.NotNil(t, wp)
	assert.Equal(t, "Electrician", wp.Profession) // preserved
	assert.Equal(t, "Valencia", wp.City)          // updated
	assert.Equal(t, 55.0, wp.HourlyRate)          // new
}

func TestWorkerProfileDelete(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	repo := repository.NewGormProfileRepository(db)
	ctx := context.Background()

	err := repo.UpsertWorkerProfile(ctx, "user-w3", map[string]interface{}{
		"profession": "Carpenter",
	})
	require.NoError(t, err)

	err = repo.DeleteWorkerProfile(ctx, "user-w3")
	require.NoError(t, err)

	wp, err := repo.GetWorkerProfile(ctx, "user-w3")
	require.NoError(t, err)
	assert.Nil(t, wp)
}

func TestWorkerProfileGetNonExistent(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	repo := repository.NewGormProfileRepository(db)
	ctx := context.Background()

	wp, err := repo.GetWorkerProfile(ctx, "does-not-exist")
	require.NoError(t, err)
	assert.Nil(t, wp)
}

// ── Client Profile CRUD ──────────────────────────────────────────

func TestClientProfileCreateAndGet(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	repo := repository.NewGormProfileRepository(db)
	ctx := context.Background()

	fields := map[string]interface{}{
		"full_name": "Alvaro Test",
		"city":      "Madrid",
		"phone":     "+34600000002",
		"bio":       "Need plumbing help",
	}

	err := repo.UpsertClientProfile(ctx, "user-c1", fields)
	require.NoError(t, err)

	cp, err := repo.GetClientProfile(ctx, "user-c1")
	require.NoError(t, err)
	require.NotNil(t, cp)
	assert.Equal(t, "user-c1", cp.UserID)
	assert.Equal(t, "Alvaro Test", cp.FullName)
	assert.Equal(t, "Madrid", cp.City)
}

func TestClientProfileUpdate(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	repo := repository.NewGormProfileRepository(db)
	ctx := context.Background()

	err := repo.UpsertClientProfile(ctx, "user-c2", map[string]interface{}{
		"full_name": "Old Name",
		"city":      "Sevilla",
	})
	require.NoError(t, err)

	err = repo.UpsertClientProfile(ctx, "user-c2", map[string]interface{}{
		"full_name": "New Name",
	})
	require.NoError(t, err)

	cp, err := repo.GetClientProfile(ctx, "user-c2")
	require.NoError(t, err)
	require.NotNil(t, cp)
	assert.Equal(t, "New Name", cp.FullName) // updated
	assert.Equal(t, "Sevilla", cp.City)      // preserved
}

func TestClientProfileDelete(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	repo := repository.NewGormProfileRepository(db)
	ctx := context.Background()

	err := repo.UpsertClientProfile(ctx, "user-c3", map[string]interface{}{
		"full_name": "Delete Me",
	})
	require.NoError(t, err)

	err = repo.DeleteClientProfile(ctx, "user-c3")
	require.NoError(t, err)

	cp, err := repo.GetClientProfile(ctx, "user-c3")
	require.NoError(t, err)
	assert.Nil(t, cp)
}

// ── FindWorkers ──────────────────────────────────────────────────

func TestFindWorkersILIKE(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	repo := repository.NewGormProfileRepository(db)
	ctx := context.Background()

	// Create two workers
	require.NoError(t, repo.UpsertWorkerProfile(ctx, "w-plumber", map[string]interface{}{
		"profession": "Plumber",
		"city":       "Madrid",
	}))
	require.NoError(t, repo.UpsertWorkerProfile(ctx, "w-electrician", map[string]interface{}{
		"profession": "Electrician",
		"city":       "Barcelona",
	}))

	// Search by profession
	result, err := repo.FindWorkers(ctx, core.WorkerSearchFilters{Profession: "plumber"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result.Workers), 1)
	assert.Equal(t, "ilike", result.Branch)

	// Search by city
	result, err = repo.FindWorkers(ctx, core.WorkerSearchFilters{City: "Barcelona"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result.Workers), 1)

	// Boolean filter
	require.NoError(t, repo.UpsertWorkerProfile(ctx, "w-emergency", map[string]interface{}{
		"profession":        "Plumber",
		"emergency_service": true,
	}))

	result, err = repo.FindWorkers(ctx, core.WorkerSearchFilters{
		EmergencyOnly: true,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result.Workers), 1)
	for _, w := range result.Workers {
		assert.True(t, w.EmergencyService)
	}
}

func TestFindWorkersOrderByCity(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	repo := repository.NewGormProfileRepository(db)
	ctx := context.Background()

	require.NoError(t, repo.UpsertWorkerProfile(ctx, "w1", map[string]interface{}{
		"profession": "Plumber", "city": "Madrid",
	}))
	require.NoError(t, repo.UpsertWorkerProfile(ctx, "w2", map[string]interface{}{
		"profession": "Plumber", "city": "Valencia",
	}))

	result, err := repo.FindWorkers(ctx, core.WorkerSearchFilters{City: "Madrid"})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result.Workers), 1)
	// Madrid worker should come first
	assert.Equal(t, "Madrid", result.Workers[0].City)
}

// ── System Prompt CRUD ──────────────────────────────────────────

func TestSystemPromptGetOrCreate(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	repo := repository.NewGormSystemPromptRepository(db)
	ctx := context.Background()

	// First Get should create defaults
	sp, err := repo.Get(ctx)
	require.NoError(t, err)
	require.NotNil(t, sp)
	assert.Equal(t, uint(1), sp.ID)
	// Defaults may be empty or from migration
}

func TestSystemPromptUpdate(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	repo := repository.NewGormSystemPromptRepository(db)
	ctx := context.Background()

	// Ensure row exists
	_, err := repo.Get(ctx)
	require.NoError(t, err)

	// Update a field
	sp, err := repo.Update(ctx, "worker_profile_prompt", "You are a worker intake assistant.")
	require.NoError(t, err)
	require.NotNil(t, sp)
	assert.Equal(t, "You are a worker intake assistant.", sp.WorkerProfilePrompt)

	// Verify persistence
	sp2, err := repo.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, "You are a worker intake assistant.", sp2.WorkerProfilePrompt)
}

// ── Chat Conversation CRUD ──────────────────────────────────────

func TestSaveAndLoadConversation(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	repo := repository.NewGormChatRepository(db)
	ctx := context.Background()

	// Save first message (creates conversation)
	convID, err := repo.SaveMessages(ctx, "user-chat1", "main", "I need a plumber", "Sure, what's your address?", "", nil, nil, "")
	require.NoError(t, err)
	assert.NotEmpty(t, convID)

	// Load conversation
	conv, err := repo.LoadConversation(ctx, "user-chat1", "main")
	require.NoError(t, err)
	require.NotNil(t, conv)
	assert.Equal(t, "user-chat1", conv.UserID)
	assert.Equal(t, "main", conv.Type)

	// Get messages
	msgs, err := repo.GetMessages(ctx, convID)
	require.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "assistant", msgs[1].Role)
}

func TestListConversations(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	repo := repository.NewGormChatRepository(db)
	ctx := context.Background()

	// Create two conversations
	_, err := repo.SaveMessages(ctx, "user-list", "main", "msg1", "reply1", "", nil, nil, "")
	require.NoError(t, err)

	_, err = repo.SaveMessages(ctx, "user-list", "search", "msg2", "reply2", "", nil, nil, "")
	require.NoError(t, err)

	convs, total, err := repo.ListConversations(ctx, "user-list", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, convs, 2)
}

func TestGetConversation(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	repo := repository.NewGormChatRepository(db)
	ctx := context.Background()

	convID, err := repo.SaveMessages(ctx, "user-get", "main", "hello", "hi", "", nil, nil, "")
	require.NoError(t, err)

	conv, err := repo.GetConversation(ctx, "user-get", convID)
	require.NoError(t, err)
	require.NotNil(t, conv)
	assert.Equal(t, convID, conv.ID)
}

// ── Direct Messages ─────────────────────────────────────────────

func TestDirectMessageFlow(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	repo := repository.NewGormDirectMessageRepository(db)
	ctx := context.Background()

	// Create DM conversation between two users
	conv, isNew, err := repo.GetOrCreateConversation(ctx, "user-a-dm1", "user-b-dm1")
	require.NoError(t, err)
	assert.True(t, isNew)
	require.NotNil(t, conv)

	// Get same conversation (not new)
	conv2, isNew2, err := repo.GetOrCreateConversation(ctx, "user-a-dm1", "user-b-dm1")
	require.NoError(t, err)
	assert.False(t, isNew2)
	assert.Equal(t, conv.ID, conv2.ID)

	// Send a message
	msg := &core.DirectMessage{
		ConversationID: conv.ID,
		SenderID:       "user-a-dm1",
		Body:           "Hi!",
		CreatedAt:      time.Now(),
	}
	err = repo.SendMessage(ctx, msg)
	require.NoError(t, err)
	assert.NotEmpty(t, msg.ID)

	// Get messages
	msgs, err := repo.GetMessages(ctx, conv.ID, 10, "")
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "Hi!", msgs[0].Body)
	assert.Equal(t, "user-a-dm1", msgs[0].SenderID)

	// Mark as read (user-b marks messages from user-a as read)
	count, err := repo.MarkRead(ctx, conv.ID, "user-b-dm1")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}
