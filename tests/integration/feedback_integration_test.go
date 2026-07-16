//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/adapters/repository"
	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Feedback admin CRUD integration tests ────────────────────────

func TestFeedbackAnonymousSubmit(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)

	body, _ := json.Marshal(map[string]interface{}{"message": "Anonymous feedback", "page_url": "/chat", "category": "general"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var fb map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fb))
	require.Equal(t, "Anonymous feedback", fb["message"])
	require.Equal(t, "", fb["user_id"])
}

func TestFeedbackSubmitsAndListsAsAdmin(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)
	feedRepo := repository.NewGormFeedbackRepository(db)

	// Submit two feedback entries directly via repo
	require.NoError(t, feedRepo.Create(&core.Feedback{
		Message:  "Submit button is broken",
		PageURL:  "/chat",
		Category: "bug",
		UserID:   "user-f1",
	}))
	require.NoError(t, feedRepo.Create(&core.Feedback{
		Message:  "Great service!",
		PageURL:  "/admin",
		Category: "praise",
		UserID:   "user-f2",
	}))
	// A third entry with different status
	require.NoError(t, feedRepo.Create(&core.Feedback{
		Message:  "Waiting too long",
		PageURL:  "/chat",
		Category: "complain",
		UserID:   "user-f3",
	}))

	// Admin list returns both
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/feedback", nil)
	req.AddCookie(MakeAdminSessionCookie("admin-f"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var rows []map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rows))
	require.GreaterOrEqual(t, len(rows), 2)
}

func TestFeedbackGetAndUpdateStatus(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)
	feedRepo := repository.NewGormFeedbackRepository(db)

	fb := &core.Feedback{
		Message:  "Need refund",
		PageURL:  "/chat",
		Category: "complain",
		UserID:   "user-get-f",
	}
	require.NoError(t, feedRepo.Create(fb))

	// GET by ID
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/feedback/"+fb.ID, nil)
	req.AddCookie(MakeAdminSessionCookie("admin-get-f"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var row map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &row))
	assert.Equal(t, fb.ID, row["id"])
	assert.Equal(t, "Need refund", row["message"])

	// PUT update status
	upd, _ := json.Marshal(map[string]interface{}{"status": "in_progress", "admin_note": "Looking into it"})
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/feedback/"+fb.ID, bytes.NewReader(upd))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(MakeAdminSessionCookie("admin-update-f"))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Verify persisted
	fb2, err := feedRepo.Get(fb.ID)
	require.NoError(t, err)
	require.NotNil(t, fb2)
	assert.Equal(t, "in_progress", fb2.Status)
	assert.Equal(t, "Looking into it", fb2.AdminNote)
}

func TestFeedbackDelete(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	llm := newFakeLLM("irrelevant")
	mux := buildIntegrationMux(t, db, llm)
	feedRepo := repository.NewGormFeedbackRepository(db)

	fb := &core.Feedback{
		Message:  "Delete me",
		PageURL:  "/chat",
		Category: "bug",
		UserID:   "user-del-f",
	}
	require.NoError(t, feedRepo.Create(fb))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/feedback/"+fb.ID, nil)
	req.AddCookie(MakeAdminSessionCookie("admin-del-f"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Verify gone
	_, err := feedRepo.Get(fb.ID)
	require.Error(t, err)
}

func TestFeedbackCountByStatusRepo(t *testing.T) {
	db := NewTestDB(t)
	migrateTestSchema(t, db)

	repo := repository.NewGormFeedbackRepository(db)
	ctx := t.Context()

	require.NoError(t, repo.Create(&core.Feedback{Message: "a", Category: "bug", PageURL: "/chat", UserID: "cf-1", Status: "open"}))
	require.NoError(t, repo.Create(&core.Feedback{Message: "b", Category: "bug", PageURL: "/chat", UserID: "cf-2", Status: "open"}))
	require.NoError(t, repo.Create(&core.Feedback{Message: "c", Category: "praise", PageURL: "/chat", UserID: "cf-3", Status: "closed"}))

	counts, err := repo.CountByStatus()
	require.NoError(t, err)
	assert.Equal(t, int64(2), counts["open"])
	assert.Equal(t, int64(1), counts["closed"])
}
