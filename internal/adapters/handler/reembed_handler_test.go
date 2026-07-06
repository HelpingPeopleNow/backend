package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockToggler implements ReembedToggler for testing.
type mockToggler struct {
	enabled bool
}

func (m *mockToggler) SetReembedEnabled(enabled bool) { m.enabled = enabled }
func (m *mockToggler) IsReembedEnabled() bool         { return m.enabled }

func TestReembedToggleGetReturnsState(t *testing.T) {
	mock := &mockToggler{enabled: true}
	h := NewReembedToggleHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reembed", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var state reembedState
	if err := json.NewDecoder(w.Body).Decode(&state); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !state.Enabled {
		t.Fatal("expected enabled=true")
	}
}

func TestReembedTogglePostEnablesAndDisables(t *testing.T) {
	mock := &mockToggler{enabled: false}
	h := NewReembedToggleHandler(mock)

	// Enable
	body, _ := json.Marshal(reembedState{Enabled: true})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reembed", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !mock.enabled {
		t.Fatal("expected mock to be enabled after POST")
	}

	// Verify via GET
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/reembed", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var state reembedState
	json.NewDecoder(w.Body).Decode(&state)
	if !state.Enabled {
		t.Fatal("GET should return enabled=true")
	}

	// Disable
	body, _ = json.Marshal(reembedState{Enabled: false})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/reembed", bytes.NewReader(body))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if mock.enabled {
		t.Fatal("expected mock to be disabled after second POST")
	}
}

func TestReembedToggleMethodNotAllowed(t *testing.T) {
	mock := &mockToggler{}
	h := NewReembedToggleHandler(mock)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/reembed", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestReembedToggleBadJSON(t *testing.T) {
	mock := &mockToggler{}
	h := NewReembedToggleHandler(mock)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reembed", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
