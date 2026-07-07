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

// ── F14/F15: Distance sorting + service_radius_km filtering ──────

func TestFindWorkersServiceRadiusExclusion(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	repo := repository.NewGormProfileRepository(db)
	ctx := context.Background()

	// Create workers with different service radii.
	// All near Madrid center (40.4168, -3.7038).
	madridLat := 40.4168
	madridLng := -3.7038

	// Worker A: close to Madrid, large radius (covers the search origin)
	nearbyLatA := 40.4200
	nearbyLngA := -3.7100
	fieldsA := map[string]interface{}{
		"profession":        "Plumber",
		"city":              "Madrid",
		"service_radius_km": 50,
		"latitude":          nearbyLatA,
		"longitude":         nearbyLngA,
	}
	err := repo.UpsertWorkerProfile(ctx, "user-radius-a", fieldsA)
	require.NoError(t, err)

	// Worker B: close to Madrid, tiny radius (does NOT cover the search origin)
	nearbyLatB := 40.4180
	nearbyLngB := -3.7050
	fieldsB := map[string]interface{}{
		"profession":        "Plumber",
		"city":              "Madrid",
		"service_radius_km": 1, // 1 km — search origin is ~0.3 km away, OK
		"latitude":          nearbyLatB,
		"longitude":         nearbyLngB,
	}
	err = repo.UpsertWorkerProfile(ctx, "user-radius-b", fieldsB)
	require.NoError(t, err)

	// Worker C: far from Madrid (Barcelona), small radius
	farLat := 41.3874
	farLng := 2.1686
	fieldsC := map[string]interface{}{
		"profession":        "Plumber",
		"city":              "Barcelona",
		"service_radius_km": 10,
		"latitude":          farLat,
		"longitude":         farLng,
	}
	err = repo.UpsertWorkerProfile(ctx, "user-radius-c", fieldsC)
	require.NoError(t, err)

	// Search from Madrid center with GPS coords.
	lat := madridLat
	lng := madridLng
	filters := core.WorkerSearchFilters{
		Profession: "Plumber",
		Latitude:   &lat,
		Longitude:  &lng,
	}

	result, err := repo.FindWorkers(ctx, filters)
	require.NoError(t, err)

	// Worker A (large radius) should be included — its radius covers the search origin.
	// Worker B (1 km radius) should also be included — the search origin is ~0.3 km away.
	// Worker C (Barcelona, 10 km radius) should be EXCLUDED — ~505 km from Madrid.
	names := make(map[string]bool)
	for _, w := range result.Workers {
		names[w.UserID] = true
	}
	assert.True(t, names["user-radius-a"], "F15: worker A (large radius, nearby) must be included")
	assert.True(t, names["user-radius-b"], "F15: worker B (small radius, within) must be included")
	assert.False(t, names["user-radius-c"], "F15: worker C (Barcelona, outside radius) must be excluded")
}

func TestFindWorkersDistanceSortOrder(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	repo := repository.NewGormProfileRepository(db)
	ctx := context.Background()

	// Create three workers at increasing distances from Madrid center.
	madridLat := 40.4168
	madridLng := -3.7038

	// Worker at ~5 km
	lat1 := 40.4500
	lng1 := -3.6900
	fields1 := map[string]interface{}{
		"profession": "Plumber",
		"city":       "Madrid",
		"latitude":   lat1,
		"longitude":  lng1,
	}
	err := repo.UpsertWorkerProfile(ctx, "user-sort-1", fields1)
	require.NoError(t, err)

	// Worker at ~20 km
	lat2 := 40.5500
	lng2 := -3.6000
	fields2 := map[string]interface{}{
		"profession": "Plumber",
		"city":       "Madrid",
		"latitude":   lat2,
		"longitude":  lng2,
	}
	err = repo.UpsertWorkerProfile(ctx, "user-sort-2", fields2)
	require.NoError(t, err)

	// Worker at ~50 km
	lat3 := 40.7000
	lng3 := -3.4000
	fields3 := map[string]interface{}{
		"profession": "Plumber",
		"city":       "Madrid",
		"latitude":   lat3,
		"longitude":  lng3,
	}
	err = repo.UpsertWorkerProfile(ctx, "user-sort-3", fields3)
	require.NoError(t, err)

	// Search from Madrid center.
	lat := madridLat
	lng := madridLng
	filters := core.WorkerSearchFilters{
		Profession: "Plumber",
		Latitude:   &lat,
		Longitude:  &lng,
	}

	result, err := repo.FindWorkers(ctx, filters)
	require.NoError(t, err)

	// Verify DistanceKm is populated and sorted nearest-first.
	require.NotEmpty(t, result.Workers)
	for _, w := range result.Workers {
		require.NotNil(t, w.DistanceKm, "F14: DistanceKm must be populated for workers with coords")
	}

	// Check sort order: distances should be non-decreasing.
	for i := 1; i < len(result.Workers); i++ {
		if result.Workers[i].DistanceKm != nil && result.Workers[i-1].DistanceKm != nil {
			assert.LessOrEqual(t, *result.Workers[i-1].DistanceKm, *result.Workers[i].DistanceKm,
				"F14: workers must be sorted by distance (nearest first)")
		}
	}
}

func TestFindWorkersMaxDistanceFilter(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)
	repo := repository.NewGormProfileRepository(db)
	ctx := context.Background()

	// Create a worker 505 km away (Barcelona from Madrid).
	fields := map[string]interface{}{
		"profession": "Plumber",
		"city":       "Barcelona",
		"latitude":   41.3874,
		"longitude":  2.1686,
	}
	err := repo.UpsertWorkerProfile(ctx, "user-maxdist", fields)
	require.NoError(t, err)

	// Search from Madrid with MaxDistanceKm=100 (should NOT find Barcelona worker).
	lat := 40.4168
	lng := -3.7038
	maxDist := 100
	filters := core.WorkerSearchFilters{
		Profession:    "Plumber",
		Latitude:      &lat,
		Longitude:     &lng,
		MaxDistanceKm: &maxDist,
	}

	result, err := repo.FindWorkers(ctx, filters)
	require.NoError(t, err)

	for _, w := range result.Workers {
		assert.NotEqual(t, "user-maxdist", w.UserID,
			"F14: Barcelona worker must be excluded when MaxDistanceKm=100")
	}
}
