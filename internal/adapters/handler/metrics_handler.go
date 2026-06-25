package handler

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Minimal Prometheus text-format metrics registry (no client_golang dependency)
// ---------------------------------------------------------------------------

// counter stores a monotonically increasing value identified by label values.
type counter struct {
	value float64
}

// gauge stores an arbitrary value identified by label values.
type gauge struct {
	value float64
}

// histogram tracks observations in configurable buckets plus a sum and count.
type histogram struct {
	buckets map[float64]float64 // upper_bound → cumulative count
	count   uint64
	sum     float64
}

// metrics holds all application metrics. All fields are guarded by mu.
var metrics = struct {
	sync.RWMutex

	httpRequestsTotal      map[string]*counter // key: method|path|status
	httpRequestDuration    map[string]*histogram
	chatRequestsTotal      map[string]*counter // key: mode
	chatLLMDuration        map[string]*histogram
	chatLLMErrorsTotal     map[string]*counter // key: provider|error_type
	authResolveDuration    map[string]*histogram
	authResolveErrorsTotal map[string]*counter // key: method|error_type
	profileSavesTotal      map[string]*counter // key: role
	conversationsTotal     map[string]*counter // key: operation
	dmSentTotal            map[string]*counter // key: role (client|worker) or "contact"
	dmReceivedTotal        map[string]*counter // key: role
	healthStatus           map[string]*gauge   // key: component

	// VECTOR_SEARCH_PLAN §12.3 — vector search metrics. NO Prometheus
	// client_golang import (Improvement #7): use the existing custom
	// registry pattern.
	vectorSearchTotal map[string]*counter   // key: status ("vector"|"ilike"|"ilike_disabled_via_env"|"ilike_low_top_score")
	vectorScore       map[string]*histogram // histogram of top_score from the vector branch
}{
	httpRequestsTotal:      make(map[string]*counter),
	httpRequestDuration:    make(map[string]*histogram),
	chatRequestsTotal:      make(map[string]*counter),
	chatLLMDuration:        make(map[string]*histogram),
	chatLLMErrorsTotal:     make(map[string]*counter),
	authResolveDuration:    make(map[string]*histogram),
	authResolveErrorsTotal: make(map[string]*counter),
	profileSavesTotal:      make(map[string]*counter),
	conversationsTotal:     make(map[string]*counter),
	dmSentTotal:            make(map[string]*counter),
	dmReceivedTotal:        make(map[string]*counter),
	healthStatus:           make(map[string]*gauge),

	vectorSearchTotal: make(map[string]*counter),
	vectorScore:       make(map[string]*histogram),
}

// defaultBuckets for latency histograms (seconds).
var defaultBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// helper: get-or-create counter
func getCounter(m map[string]*counter, key string) *counter {
	c, ok := m[key]
	if !ok {
		c = &counter{}
		m[key] = c
	}
	return c
}

// helper: get-or-create histogram
func getHistogram(m map[string]*histogram, key string) *histogram {
	h, ok := m[key]
	if !ok {
		buckets := make(map[float64]float64, len(defaultBuckets))
		for _, b := range defaultBuckets {
			buckets[b] = 0
		}
		h = &histogram{buckets: buckets}
		m[key] = h
	}
	return h
}

// helper: get-or-create gauge
func getGauge(m map[string]*gauge, key string) *gauge {
	g, ok := m[key]
	if !ok {
		g = &gauge{}
		m[key] = g
	}
	return g
}

// observeValue records a value in a histogram bucket set.
func observeValue(h *histogram, v float64) {
	h.count++
	h.sum += v
	for bound := range h.buckets {
		if v <= bound {
			h.buckets[bound]++
		}
	}
}

// ---------------------------------------------------------------------------
// Public API – called from handlers to record metrics
// ---------------------------------------------------------------------------

// IncrHTTPRequests increments the http_requests_total counter.
func IncrHTTPRequests(method, path, status string) {
	metrics.Lock()
	defer metrics.Unlock()
	key := method + "|" + path + "|" + status
	getCounter(metrics.httpRequestsTotal, key).value++
}

// ObserveHTTPDuration records a request duration observation.
func ObserveHTTPDuration(method, path string, seconds float64) {
	metrics.Lock()
	defer metrics.Unlock()
	key := method + "|" + path
	h := getHistogram(metrics.httpRequestDuration, key)
	observeValue(h, seconds)
}

// IncrChatRequests increments the chat_requests_total counter.
func IncrChatRequests(mode string) {
	metrics.Lock()
	defer metrics.Unlock()
	getCounter(metrics.chatRequestsTotal, mode).value++
}

// ObserveChatLLMDuration records an LLM call duration observation.
func ObserveChatLLMDuration(provider, mode string, seconds float64) {
	metrics.Lock()
	defer metrics.Unlock()
	key := provider + "|" + mode
	h := getHistogram(metrics.chatLLMDuration, key)
	observeValue(h, seconds)
}

// IncrChatLLMErrors increments the chat_llm_errors_total counter.
func IncrChatLLMErrors(provider, errorType string) {
	metrics.Lock()
	defer metrics.Unlock()
	key := provider + "|" + errorType
	getCounter(metrics.chatLLMErrorsTotal, key).value++
}

// ObserveAuthResolve records an auth-resolve duration observation.
func ObserveAuthResolve(method string, seconds float64) {
	metrics.Lock()
	defer metrics.Unlock()
	h := getHistogram(metrics.authResolveDuration, method)
	observeValue(h, seconds)
}

// IncrAuthResolveErrors increments the auth_resolve_errors_total counter.
func IncrAuthResolveErrors(method, errorType string) {
	metrics.Lock()
	defer metrics.Unlock()
	key := method + "|" + errorType
	getCounter(metrics.authResolveErrorsTotal, key).value++
}

// IncrProfileSave increments the profile_saves_total counter.
func IncrProfileSave(role string) {
	metrics.Lock()
	defer metrics.Unlock()
	getCounter(metrics.profileSavesTotal, role).value++
}

// IncrConversation increments the conversations_total counter.
func IncrConversation(op string) {
	metrics.Lock()
	defer metrics.Unlock()
	getCounter(metrics.conversationsTotal, op).value++
}

// IncrDMSent increments the dm_sent_total counter.
func IncrDMSent(role string) {
	metrics.Lock()
	defer metrics.Unlock()
	getCounter(metrics.dmSentTotal, role).value++
}

// IncrDMReceived increments the dm_received_total counter.
func IncrDMReceived(role string) {
	metrics.Lock()
	defer metrics.Unlock()
	getCounter(metrics.dmReceivedTotal, role).value++
}

// SetHealthStatus sets the health_status gauge (1 = healthy, 0 = unhealthy).
func SetHealthStatus(component string, healthy bool) {
	metrics.Lock()
	defer metrics.Unlock()
	g := getGauge(metrics.healthStatus, component)
	if healthy {
		g.value = 1
	} else {
		g.value = 0
	}
}

// IncrVectorSearch increments the vector_search_total counter per branch
// (VECTOR_SEARCH_PLAN §12.3 / Idea C). status values:
//   - "vector"                        — vector branch produced the result
//   - "ilike"                         — ILIKE branch (no vector available)
//   - "ilike_disabled_via_env"        — VECTOR_SEARCH_ENABLED=false
//   - "ilike_low_top_score"           — vector ran but top_score < threshold
func IncrVectorSearch(status string) {
	metrics.Lock()
	defer metrics.Unlock()
	getCounter(metrics.vectorSearchTotal, status).value++
}

// ObserveVectorScore records the top_score from a vector search run.
// Used to inspect whether the configured VECTOR_SEARCH_MIN_TOP_SCORE
// threshold is sane (Phase 5.5 of §15 — raise the threshold if median
// drops below 0.55 across 100+ searches).
func ObserveVectorScore(score float64) {
	metrics.Lock()
	defer metrics.Unlock()
	h := getHistogram(metrics.vectorScore, "all")
	observeValue(h, score)
}

// RegisterMetricsRoutes registers GET /metrics on the provided mux.
func RegisterMetricsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /metrics", metricsHandler)
}

// ---------------------------------------------------------------------------
// Prometheus text format rendering
// ---------------------------------------------------------------------------

type metricFamily struct {
	name       string
	help       string
	metricType string // counter, histogram, gauge
	keys       []string
	counters   map[string]*counter
	histograms map[string]*histogram
	gauges     map[string]*gauge
}

func renderFamily(sb *strings.Builder, fam metricFamily) {
	// HELP line
	fmt.Fprintf(sb, "# HELP %s %s\n", fam.name, fam.help)
	// TYPE line
	fmt.Fprintf(sb, "# TYPE %s %s\n", fam.name, fam.metricType)

	// Collect keys and sort for deterministic output
	type entry struct {
		key    string
		values []string
	}
	var entries []entry
	for k := range fam.counters {
		vals := strings.Split(k, "|")
		entries = append(entries, entry{key: k, values: vals})
	}
	for k := range fam.histograms {
		vals := strings.Split(k, "|")
		entries = append(entries, entry{key: k, values: vals})
	}
	for k := range fam.gauges {
		vals := strings.Split(k, "|")
		entries = append(entries, entry{key: k, values: vals})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].key < entries[j].key
	})

	for _, e := range entries {
		labels := buildLabels(fam.keys, e.values)
		switch fam.metricType {
		case "counter":
			c := fam.counters[e.key]
			if c != nil {
				fmt.Fprintf(sb, "%s%s %s\n", fam.name, labels, formatFloat(c.value))
			}
		case "gauge":
			g := fam.gauges[e.key]
			if g != nil {
				fmt.Fprintf(sb, "%s%s %s\n", fam.name, labels, formatFloat(g.value))
			}
		case "histogram":
			h := fam.histograms[e.key]
			if h == nil {
				continue
			}
			// Render bucket lines
			bounds := make([]float64, 0, len(h.buckets))
			for b := range h.buckets {
				bounds = append(bounds, b)
			}
			sort.Float64s(bounds)
			for _, bound := range bounds {
				fmt.Fprintf(sb, "%s_bucket%s %d\n", fam.name, labelsWithLe(labels, bound), uint64(h.buckets[bound]))
			}
			fmt.Fprintf(sb, "%s_count%s %d\n", fam.name, labels, h.count)
			fmt.Fprintf(sb, "%s_sum%s %s\n", fam.name, labels, formatFloat(h.sum))
		}
	}
}

func buildLabels(keys, values []string) string {
	if len(keys) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		v := ""
		if i < len(values) {
			v = values[i]
		}
		fmt.Fprintf(&sb, "%s=%q", k, v)
	}
	sb.WriteByte('}')
	return sb.String()
}

func labelsWithLe(labels string, le float64) string {
	if labels == "" {
		return fmt.Sprintf("{le=%q}", formatFloat(le))
	}
	// Insert le before closing brace
	return labels[:len(labels)-1] + fmt.Sprintf(",le=%q}", formatFloat(le))
}

func formatFloat(f float64) string {
	if f == math.Trunc(f) && !math.IsInf(f, 0) && !math.IsNaN(f) {
		return fmt.Sprintf("%.0f", f)
	}
	return fmt.Sprintf("%g", f)
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	metrics.RLock()
	defer metrics.RUnlock()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	var sb strings.Builder
	write := func(s string) { sb.WriteString(s) }

	// 1. http_requests_total
	write(renderFamilyToString(metricFamily{
		name:       "http_requests_total",
		help:       "Total HTTP requests processed.",
		metricType: "counter",
		keys:       []string{"method", "path", "status"},
		counters:   metrics.httpRequestsTotal,
	}))

	// 2. http_request_duration_seconds
	write(renderFamilyToString(metricFamily{
		name:       "http_request_duration_seconds",
		help:       "HTTP request latency in seconds.",
		metricType: "histogram",
		keys:       []string{"method", "path"},
		histograms: metrics.httpRequestDuration,
	}))

	// 3. chat_requests_total
	write(renderFamilyToString(metricFamily{
		name:       "chat_requests_total",
		help:       "Total chat requests by mode.",
		metricType: "counter",
		keys:       []string{"mode"},
		counters:   metrics.chatRequestsTotal,
	}))

	// 4. chat_llm_duration_seconds
	write(renderFamilyToString(metricFamily{
		name:       "chat_llm_duration_seconds",
		help:       "LLM call latency in seconds.",
		metricType: "histogram",
		keys:       []string{"provider", "mode"},
		histograms: metrics.chatLLMDuration,
	}))

	// 5. chat_llm_errors_total
	write(renderFamilyToString(metricFamily{
		name:       "chat_llm_errors_total",
		help:       "Total LLM errors by provider and error type.",
		metricType: "counter",
		keys:       []string{"provider", "error_type"},
		counters:   metrics.chatLLMErrorsTotal,
	}))

	// 6. auth_resolve_duration_seconds
	write(renderFamilyToString(metricFamily{
		name:       "auth_resolve_duration_seconds",
		help:       "Auth resolve latency in seconds.",
		metricType: "histogram",
		keys:       []string{"method"},
		histograms: metrics.authResolveDuration,
	}))

	// 7. auth_resolve_errors_total
	write(renderFamilyToString(metricFamily{
		name:       "auth_resolve_errors_total",
		help:       "Total auth resolve errors by method and error type.",
		metricType: "counter",
		keys:       []string{"method", "error_type"},
		counters:   metrics.authResolveErrorsTotal,
	}))

	// 8. profile_saves_total
	write(renderFamilyToString(metricFamily{
		name:       "profile_saves_total",
		help:       "Total profile saves by role.",
		metricType: "counter",
		keys:       []string{"role"},
		counters:   metrics.profileSavesTotal,
	}))

	// 9. conversations_total
	write(renderFamilyToString(metricFamily{
		name:       "conversations_total",
		help:       "Total conversation operations.",
		metricType: "counter",
		keys:       []string{"operation"},
		counters:   metrics.conversationsTotal,
	}))

	// 10. dm_sent_total
	write(renderFamilyToString(metricFamily{
		name:       "dm_sent_total",
		help:       "Total direct messages sent by role.",
		metricType: "counter",
		keys:       []string{"role"},
		counters:   metrics.dmSentTotal,
	}))

	// 11. dm_received_total
	write(renderFamilyToString(metricFamily{
		name:       "dm_received_total",
		help:       "Total direct messages received by role.",
		metricType: "counter",
		keys:       []string{"role"},
		counters:   metrics.dmReceivedTotal,
	}))

	// 12. health_status
	write(renderFamilyToString(metricFamily{
		name:       "health_status",
		help:       "Health status of components (1=healthy, 0=unhealthy).",
		metricType: "gauge",
		keys:       []string{"component"},
		gauges:     metrics.healthStatus,
	}))

	// 13. vector_search_total (VECTOR_SEARCH_PLAN §12.3 / Idea C / N1)
	write(renderFamilyToString(metricFamily{
		name:       "vector_search_total",
		help:       "Total search requests by branch (vector|ilike|ilike_disabled_via_env|ilike_low_top_score).",
		metricType: "counter",
		keys:       []string{"branch"},
		counters:   metrics.vectorSearchTotal,
	}))

	// 14. vector_score (VECTOR_SEARCH_PLAN §12.3 / Phase 5.5)
	write(renderFamilyToString(metricFamily{
		name:       "vector_score",
		help:       "Top cosine score of the vector branch per search (0–1; close to 1 is semantically close).",
		metricType: "histogram",
		histograms: metrics.vectorScore,
	}))

	w.Write([]byte(sb.String()))
}

// renderFamilyToString is a thin wrapper that renders to a string.
func renderFamilyToString(fam metricFamily) string {
	var sb strings.Builder
	renderFamily(&sb, fam)
	return sb.String()
}
