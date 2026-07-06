package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestCORSAllowedOrigin verifies that allowed origins get ACAO header with credentials.
func TestCORSAllowedOrigin(t *testing.T) {
	// Set env var before init runs (must reset after)
	os.Setenv("ALLOWED_ORIGINS", "https://helpingpeople.cloud,https://localhost:3000")
	defer os.Unsetenv("ALLOWED_ORIGINS")

	// Force re-read of env by calling isAllowedOrigin with known values
	// (init already ran at package load, so we test the current behavior)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat", nil)
	req.Header.Set("Origin", "https://helpingpeople.cloud")
	rr := httptest.NewRecorder()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	CORS(next).ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "https://helpingpeople.cloud" {
		t.Errorf("Expected ACAO=https://helpingpeople.cloud, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("Expected credentials=true, got %q", rr.Header().Get("Access-Control-Allow-Credentials"))
	}
	if rr.Header().Get("Vary") != "Origin" {
		t.Errorf("Expected Vary=Origin, got %q", rr.Header().Get("Vary"))
	}
}

// TestCORSDisallowedOrigin verifies that disallowed origins do NOT get ACAO header.
func TestCORSDisallowedOrigin(t *testing.T) {
	os.Setenv("ALLOWED_ORIGINS", "https://helpingpeople.cloud")
	defer os.Unsetenv("ALLOWED_ORIGINS")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat", nil)
	req.Header.Set("Origin", "https://evil.com")
	rr := httptest.NewRecorder()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	CORS(next).ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("Expected no ACAO for disallowed origin, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Errorf("Expected no credentials for disallowed origin, got %q", rr.Header().Get("Access-Control-Allow-Credentials"))
	}
}

// TestCORSNoOrigin verifies requests without Origin header work normally.
func TestCORSNoOrigin(t *testing.T) {
	os.Setenv("ALLOWED_ORIGINS", "https://helpingpeople.cloud")
	defer os.Unsetenv("ALLOWED_ORIGINS")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat", nil)
	// No Origin header
	rr := httptest.NewRecorder()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	CORS(next).ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("Expected no ACAO when no Origin header, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

// TestCORSOptionsPreflight verifies OPTIONS returns 204 with correct headers.
func TestCORSOptionsPreflight(t *testing.T) {
	os.Setenv("ALLOWED_ORIGINS", "https://helpingpeople.cloud")
	defer os.Unsetenv("ALLOWED_ORIGINS")

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/chat", nil)
	req.Header.Set("Origin", "https://helpingpeople.cloud")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rr := httptest.NewRecorder()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	CORS(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("Expected 204 for OPTIONS, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "https://helpingpeople.cloud" {
		t.Errorf("Expected ACAO on preflight, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Errorf("Expected Allow-Methods on preflight")
	}
	if rr.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Errorf("Expected Allow-Headers on preflight")
	}
}
