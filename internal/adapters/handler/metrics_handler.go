package handler

import (
	"crypto/subtle"
	"fmt"
	metricspkg "github.com/HelpingPeopleNow/backend/internal/metrics"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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

// gaugeScrapeSource is a callback-style "pull" gauge source. The metrics
// handler invokes Source() at scrape time and writes the returned value
// into the underlying gauge. This lets the /metrics endpoint surface
// up-to-the-moment values from external state (search cache size, SSE
// subscriber count, DB pool in-use) without those producers having to
// know about Prometheus conventions. (P2-1 audit / F6 observability gap.)
type gaugeScrapeSource struct {
	name        string
	help        string
	labelValues []string
	labelNames  []string
	source      func() float64
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
	healthStatus           map[string]*gauge   // key: component (DEPRECATED: use atomicHealthStatus)

	// VECTOR_SEARCH_PLAN §12.3 — vector search metrics. NO Prometheus
	// client_golang import (Improvement #7): use the existing custom
	// registry pattern.
	vectorSearchTotal map[string]*counter   // key: status (vector|ilike|ilike_disabled_via_env|ilike_low_top_score|ilike_embed_failed)
	vectorScore       map[string]*histogram // histogram of top_score from the vector branch

	// F2/F4 — search rate limiting + embed failure metrics.
	searchRateLimitedTotal map[string]*counter // key: user_id prefix
	embedFailuresTotal     map[string]*counter // key: "embed"
	helperBreakerState     map[string]*gauge   // key: state (closed|open|half_open)

	// gaugeScrapeSources — registered callbacks that refresh gauge
	// values on each /metrics scrape. The text-format renderer walks
	// this slice and writes the returned value into the named gauge.
	// (P2-1 audit.)
	gaugeScrapeSources []*gaugeScrapeSource

	// Live-refresh gauges whose value is set by the scrape-source
	// callback. Stored separately from the static health gauges so
	// renderFamily() can distinguish them if needed.
	dynamicGauges map[string]*gauge // key: source.Name()
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

	vectorSearchTotal:      make(map[string]*counter),
	vectorScore:            make(map[string]*histogram),
	searchRateLimitedTotal: make(map[string]*counter),
	embedFailuresTotal:     make(map[string]*counter),
	helperBreakerState:     make(map[string]*gauge),

	gaugeScrapeSources: nil,
	dynamicGauges:      make(map[string]*gauge),
}

// atomicHealthStatus uses atomics instead of the metrics RWMutex to avoid
// the reader-writer convoy that caused the 2026-07-10 deadlock (goroutine
// dump: SetHealthStatus held metrics.Lock while Prometheus scrapes queued
// behind it, starving ALL HTTP handlers including /readyz and /livez).
var atomicHealthStatus = map[string]*atomic.Int64{
	"postgres":    {},
	"grpc_helper": {},
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
// P2-1 — Gauge scrape-source registration
// ---------------------------------------------------------------------------

// RegisterGaugeScrapeSource registers a callback that produces the live
// value for a named gauge at every /metrics scrape. Composition-root
// code (main.go) calls this with closures over SearchService, Broker,
// and *sql.DB so each /metrics request sees up-to-the-moment values.
//
// Returns silently if a source with the same name is already registered
// (allows safe restart with overlapping registration order).
func RegisterGaugeScrapeSource(name, help string, labelNames []string, labelValues []string, source func() float64) {
	metrics.Lock()
	defer metrics.Unlock()
	for _, existing := range metrics.gaugeScrapeSources {
		if existing.name == name && sameLabels(existing.labelValues, labelValues) {
			return
		}
	}
	metrics.gaugeScrapeSources = append(metrics.gaugeScrapeSources, &gaugeScrapeSource{
		name:        name,
		help:        help,
		labelNames:  labelNames,
		labelValues: labelValues,
		source:      source,
	})
	// Pre-create the gauge so the first scrape after registration has a
	// stable zero rather than being absent.
	metrics.dynamicGauges[name] = &gauge{}
}

func sameLabels(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
// Uses atomic store to avoid acquiring the metrics RWMutex, which caused
// a reader-writer convoy deadlock on 2026-07-10 (see atomicHealthStatus doc).
func SetHealthStatus(component string, healthy bool) {
	if a, ok := atomicHealthStatus[component]; ok {
		if healthy {
			a.Store(1)
		} else {
			a.Store(0)
		}
		return
	}
	// Fallback for unknown components (should not happen in production).
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

// IncrSearchRateLimited increments the search_rate_limited_total counter (F2).
func IncrSearchRateLimited(userID string) {
	metrics.Lock()
	defer metrics.Unlock()
	getCounter(metrics.searchRateLimitedTotal, "user").value++
}

// IncrEmbedFailures increments the embed_failures_total counter (F4).
func IncrEmbedFailures() {
	metrics.Lock()
	defer metrics.Unlock()
	getCounter(metrics.embedFailuresTotal, "embed").value++
}

// SetHelperBreakerState sets the helper_breaker_state gauge (F3).
func SetHelperBreakerState(state string) {
	metrics.Lock()
	defer metrics.Unlock()
	g := getGauge(metrics.helperBreakerState, state)
	g.value = 1
}

// ---------------------------------------------------------------------------
// /metrics route registration (P2-2 — bearer-token gate)
// ---------------------------------------------------------------------------

// RegisterMetricsRoutes registers GET /metrics on the provided mux.
//
// token — if non-empty, /metrics requires `Authorization: Bearer <token>`
// with a constant-time byte comparison. The expected token is read from
// the METRICS_TOKEN env at composition time (production must set it).
//
// token == "" preserves the legacy "unauthenticated metrics endpoint"
// behaviour but logs a loud warning so an operator notices. The audit
// notes that unauthenticated /metrics is recon-friendly (F9).
func RegisterMetricsRoutes(mux *http.ServeMux, token string) {
	if token == "" {
		slog.Warn("metrics: registered WITHOUT bearer-token auth — set METRICS_TOKEN to lock down (F9 / P2-2 audit)")
	} else {
		slog.Info("metrics: registered with bearer-token auth (F9 / P2-2)")
	}
	expected := []byte(token)
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		if len(expected) > 0 {
			auth := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(auth, prefix) {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			got := []byte(auth[len(prefix):])
			if subtle.ConstantTimeCompare(got, expected) != 1 {
				http.Error(w, "invalid bearer token", http.StatusUnauthorized)
				return
			}
		}
		metricsHandler(w, r)
	})
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
	// P2-1: refresh dynamic gauges BEFORE snapshotting the registry for
	// rendering. Each registered source callback runs with no mutex held
	// — that's safe because the callbacks should be cheap getters
	// (SearchService.SearchCacheSize, Broker.ActiveConnections,
	// *sql.DB.Stats().InUse).
	refreshDynamicGauges()

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

	// 12. health_status — read from atomics (no mutex needed)
	{
		var sb2 strings.Builder
		sb2.WriteString("# HELP health_status Health status of components (1=healthy, 0=unhealthy).\n")
		sb2.WriteString("# TYPE health_status gauge\n")
		// Sort keys for deterministic output.
		keys := make([]string, 0, len(atomicHealthStatus))
		for k := range atomicHealthStatus {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := atomicHealthStatus[k].Load()
			sb2.WriteString(fmt.Sprintf("health_status{component=%q} %d\n", k, v))
		}
		write(sb2.String())
	}

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

	// 15. search_rate_limited_total (F2)
	write(renderFamilyToString(metricFamily{
		name:       "search_rate_limited_total",
		help:       "Total search requests rejected by per-user rate limiter.",
		metricType: "counter",
		counters:   metrics.searchRateLimitedTotal,
	}))

	// 16. embed_failures_total (F4)
	write(renderFamilyToString(metricFamily{
		name:       "embed_failures_total",
		help:       "Total embed call failures (distinct from vector-query fallbacks).",
		metricType: "counter",
		counters:   metrics.embedFailuresTotal,
	}))

	// 17. helper_breaker_state (F3)
	write(renderFamilyToString(metricFamily{
		name:       "helper_breaker_state",
		help:       "Helper circuit breaker state (0=closed, 1=open, 2=half_open).",
		metricType: "gauge",
		keys:       []string{"state"},
		gauges:     metrics.helperBreakerState,
	}))

	// 18+ — dynamic gauges (P2-1): one HELP/TYPE block per registered scrape source.
	write(renderDynamicGauges())

	// 21+ — reembed metrics live in internal/metrics — appended below.
	write(metricspkg.Render())

	w.Write([]byte(sb.String()))
}

// refreshDynamicGauges invokes every registered gauge scrape source and
// writes the returned value into the corresponding dynamic gauge. Runs
// without holding any long-lived mutex; each source callback is expected
// to be a quick getter.
func refreshDynamicGauges() {
	metrics.RLock()
	sources := make([]*gaugeScrapeSource, len(metrics.gaugeScrapeSources))
	copy(sources, metrics.gaugeScrapeSources)
	metrics.RUnlock()

	for _, src := range sources {
		if src == nil || src.source == nil {
			continue
		}
		v := src.source()
		metrics.Lock()
		g, ok := metrics.dynamicGauges[src.name]
		if !ok {
			g = &gauge{}
			metrics.dynamicGauges[src.name] = g
		}
		g.value = v
		metrics.Unlock()
	}
}

// renderDynamicGauges writes one HELP/TYPE block per registered gauge
// source plus a single series line. Ordered by source name for stable
// scraper output.
func renderDynamicGauges() string {
	metrics.RLock()
	defer metrics.RUnlock()

	if len(metrics.dynamicGauges) == 0 {
		return ""
	}

	// Group gauges by source name (no labels in our current usage, but
	// the renderFamily helper expects a map[string]*gauge keyed on the
	// full label string).
	type fam struct {
		name, help string
		gauges     map[string]*gauge
	}
	bySource := map[string]*fam{}
	for _, src := range metrics.gaugeScrapeSources {
		g, ok := metrics.dynamicGauges[src.name]
		if !ok {
			continue
		}
		if _, exists := bySource[src.name]; !exists {
			bySource[src.name] = &fam{
				name:   src.name,
				help:   src.help,
				gauges: make(map[string]*gauge),
			}
		}
		// key the gauge map by a stable label-string for the
		// renderFamily helper. Empty string renders as no-label form.
		key := strings.Join(src.labelValues, "|")
		bySource[src.name].gauges[key] = g
	}

	names := make([]string, 0, len(bySource))
	for n := range bySource {
		names = append(names, n)
	}
	sort.Strings(names)

	var sb strings.Builder
	for _, name := range names {
		f := bySource[name]
		// Find the source again so we can pass label names through.
		var labelNames []string
		for _, src := range metrics.gaugeScrapeSources {
			if src.name == name {
				labelNames = src.labelNames
				break
			}
		}
		sb.WriteString(renderFamilyToString(metricFamily{
			name:       f.name,
			help:       f.help,
			metricType: "gauge",
			keys:       labelNames,
			gauges:     f.gauges,
		}))
	}
	return sb.String()
}

// renderFamilyToString is a thin wrapper that renders to a string.
func renderFamilyToString(fam metricFamily) string {
	var sb strings.Builder
	renderFamily(&sb, fam)
	return sb.String()
}
