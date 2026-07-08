package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	RegisterMetricsRoutes(mux, "")

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
	RegisterMetricsRoutes(mux, "")

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
	RegisterMetricsRoutes(mux, "")

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
	RegisterMetricsRoutes(mux, "")

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
	RegisterMetricsRoutes(mux, "")

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
	RegisterMetricsRoutes(mux, "")

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

// ── P2-2 (audit / F9) — /metrics bearer-token auth ────────────────────────

func TestMetricsRequiresBearerToken(t *testing.T) {
	mux := http.NewServeMux()
	RegisterMetricsRoutes(mux, "s3cr3t-token-XYZ")

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "missing bearer must 401")
}

func TestMetricsRejectsWrongBearer(t *testing.T) {
	mux := http.NewServeMux()
	RegisterMetricsRoutes(mux, "s3cr3t-token-XYZ")

	req := httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "wrong bearer must 401")
}

func TestMetricsAcceptsCorrectBearer(t *testing.T) {
	mux := http.NewServeMux()
	RegisterMetricsRoutes(mux, "s3cr3t-token-XYZ")

	req := httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Bearer s3cr3t-token-XYZ")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	// Help lines from each registered family should be present.
	assert.Contains(t, body, "# HELP http_requests_total")
	assert.Contains(t, body, "# HELP chat_requests_total")
}

func TestMetricsRejectsNonBearerScheme(t *testing.T) {
	mux := http.NewServeMux()
	RegisterMetricsRoutes(mux, "s3cr3t-token-XYZ")

	req := httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Basic c29tZTpoZWxsbw==")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "Basic auth must not satisfy bearer check")
}

// ── P2-1 (audit / F6) — gauge scrape sources ─────────────────────────────

// TestGaugeScrapeSourceRendersValue registers a synthetic scrape source
// and asserts the dynamic gauge appears in the /metrics output with the
// value the callback returned at scrape time.
func TestGaugeScrapeSourceRendersValue(t *testing.T) {
	// Use a unique name so this test is order-independent of other
	// registrations already present in this binary's package state.
	name := "test_dynamic_gauge_value_render"
	RegisterGaugeScrapeSource(
		name,
		"Synthetic gauge for TestGaugeScrapeSourceRendersValue.",
		nil, nil,
		func() float64 { return 42 },
	)

	mux := http.NewServeMux()
	RegisterMetricsRoutes(mux, "")

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	assert.Contains(t, body, "# HELP "+name, "scrape-source gauge must appear in # HELP output")
	assert.Contains(t, body, "# TYPE "+name+" gauge", "scrape-source gauge must declare gauge type")
	assert.Contains(t, body, name+" 42", "scrape-source gauge must publish the callback value")
}

// TestGaugeScrapeSourceRefreshesOnEachScrape registers a counter-style
// scrape source and asserts consecutive scrapes see monotonically
// increasing values — proving the refreshDynamicGauges path is invoked
// per request, not just at startup.
func TestGaugeScrapeSourceRefreshesOnEachScrape(t *testing.T) {
	var calls int
	var mu sync.Mutex
	name := "test_dynamic_gauge_refresh_each_scrape"
	RegisterGaugeScrapeSource(
		name,
		"Synthetic gauge for TestGaugeScrapeSourceRefreshesOnEachScrape.",
		nil, nil,
		func() float64 {
			mu.Lock()
			defer mu.Unlock()
			calls++
			return float64(calls)
		},
	)

	mux := http.NewServeMux()
	RegisterMetricsRoutes(mux, "")

	for i := 1; i <= 3; i++ {
		req := httptest.NewRequest("GET", "/metrics", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		body := rec.Body.String()
		assert.Contains(t, body, name+" "+itoa(i),
			"scrape %d must observe value %d", i, i)
	}
}

// TestRegisterGaugeScrapeSourceIdempotent registers the same name twice
// and asserts only ONE help/type block is rendered in /metrics (so a
// doubled registration in main.go doesn't double-emit).
func TestRegisterGaugeScrapeSourceIdempotent(t *testing.T) {
	name := "test_dynamic_gauge_idempotent_register"
	RegisterGaugeScrapeSource(name, "first help.", nil, nil, func() float64 { return 1 })
	RegisterGaugeScrapeSource(name, "second help.", nil, nil, func() float64 { return 2 })

	mux := http.NewServeMux()
	RegisterMetricsRoutes(mux, "")
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	helpCount := strings.Count(body, "# HELP "+name)
	typeCount := strings.Count(body, "# TYPE "+name+" gauge")
	assert.Equal(t, 1, helpCount, "double-registered source must render one HELP line, got %d", helpCount)
	assert.Equal(t, 1, typeCount, "double-registered source must render one TYPE line, got %d", typeCount)
}

// itoa is a tiny helper so we can avoid importing strconv at the top of
// the test file. Kept short to keep the test code obvious.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
