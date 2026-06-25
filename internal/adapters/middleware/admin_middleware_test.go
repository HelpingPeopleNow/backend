package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/stretchr/testify/assert"
)

func TestAdminWrapNoAuthURL(t *testing.T) {
	m := NewAdminMiddleware("")
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	})
	handler := m.Wrap(inner)

	req := httptest.NewRequest("GET", "/admin/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "admin auth not configured")
}

func TestAdminWrapNoUserID(t *testing.T) {
	m := NewAdminMiddleware("http://fake-auth")
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	})
	handler := m.Wrap(inner)

	req := httptest.NewRequest("GET", "/admin/test", nil)
	// No user ID in context
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "unauthorized")
}

func TestAdminWrapAuthServiceDown(t *testing.T) {
	// Start a server that's immediately closed
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // close immediately

	m := NewAdminMiddleware(srv.URL)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	})
	handler := m.Wrap(inner)

	req := httptest.NewRequest("GET", "/admin/test", nil)
	req = req.WithContext(contextkeys.SetUserID(req.Context(), "user-1"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestAdminWrapNonAdminRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"user": map[string]interface{}{
				"id":       "user-1",
				"is_admin": false,
			},
		})
	}))
	defer srv.Close()

	m := NewAdminMiddleware(srv.URL)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	})
	handler := m.Wrap(inner)

	req := httptest.NewRequest("GET", "/admin/test", nil)
	req = req.WithContext(contextkeys.SetUserID(req.Context(), "user-1"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "admin access required")
}

func TestAdminWrapAdminAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"user": map[string]interface{}{
				"id":       "admin-1",
				"is_admin": true,
			},
		})
	}))
	defer srv.Close()

	m := NewAdminMiddleware(srv.URL)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check that IsAdmin is set in context
		assert.True(t, contextkeys.GetIsAdmin(r.Context()))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	handler := m.Wrap(inner)

	req := httptest.NewRequest("GET", "/admin/test", nil)
	req = req.WithContext(contextkeys.SetUserID(req.Context(), "admin-1"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestAdminWrapAuthReturns401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	m := NewAdminMiddleware(srv.URL)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	})
	handler := m.Wrap(inner)

	req := httptest.NewRequest("GET", "/admin/test", nil)
	req = req.WithContext(contextkeys.SetUserID(req.Context(), "user-1"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
