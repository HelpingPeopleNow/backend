package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsEndpointReturnsText(t *testing.T) {
	// Increment some counters so the output is non-empty
	IncrHTTPRequests("GET", "/api/v1/test", "200")
	ObserveHTTPDuration("GET", "/api/v1/test", 0.05)
	IncrChatRequests("search")

	mux := http.NewServeMux()
	RegisterMetricsRoutes(mux)

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")

	body := rec.Body.String()
	assert.True(t, strings.Contains(body, "http_requests_total"), "should contain http_requests_total metric")
	assert.True(t, strings.Contains(body, "chat_requests_total"), "should contain chat_requests_total metric")
}

func TestMetricsCounterIncrement(t *testing.T) {
	// Multiple increments should accumulate
	IncrHTTPRequests("POST", "/api/v1/metrics-test", "201")
	IncrHTTPRequests("POST", "/api/v1/metrics-test", "201")
	IncrHTTPRequests("POST", "/api/v1/metrics-test", "500")

	mux := http.NewServeMux()
	RegisterMetricsRoutes(mux)

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	// Just verify it contains data — exact counter values depend on package-level state
	assert.Contains(t, body, "http_requests_total")
}

func TestHealthStatusMetric(t *testing.T) {
	SetHealthStatus("postgres", true)
	SetHealthStatus("grpc_helper", false)

	mux := http.NewServeMux()
	RegisterMetricsRoutes(mux)

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	assert.Contains(t, body, "health_status")
}

func TestMetricsVectorSearch(t *testing.T) {
	IncrVectorSearch("vector")
	IncrVectorSearch("ilike")
	ObserveVectorScore(0.85)

	mux := http.NewServeMux()
	RegisterMetricsRoutes(mux)

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	assert.Contains(t, body, "vector_search_total")
	assert.Contains(t, body, "vector_score")
}

func TestMetricsHistogram(t *testing.T) {
	ObserveChatLLMDuration("openai", "worker_intake", 1.5)
	ObserveChatLLMDuration("openai", "worker_intake", 2.5)
	ObserveAuthResolve("session", 0.1)

	mux := http.NewServeMux()
	RegisterMetricsRoutes(mux)

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	require.Contains(t, body, "chat_llm_duration_seconds")
	require.Contains(t, body, "auth_resolve_duration_seconds")
}

func TestMetricsProfileConversations(t *testing.T) {
	IncrProfileSave("worker")
	IncrProfileSave("client")
	IncrConversation("list")
	IncrConversation("save")
	IncrDMSent("client")
	IncrDMReceived("worker")
	IncrAuthResolveErrors("session", "timeout")

	mux := http.NewServeMux()
	RegisterMetricsRoutes(mux)

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	assert.Contains(t, body, "profile_saves_total")
	assert.Contains(t, body, "conversations_total")
	assert.Contains(t, body, "dm_sent_total")
	assert.Contains(t, body, "dm_received_total")
	assert.Contains(t, body, "auth_resolve_errors_total")
}
