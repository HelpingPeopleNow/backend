// Package metrics holds application-wide Prometheus metric helpers
// without depending on any handler or service package, breaking
// the handler ↔ services import cycle (P2-2 audit).
package metrics

import (
	"fmt"
	"math"
	"strings"
	"sync"
)

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
	buckets map[float64]float64
	count   uint64
	sum     float64
}

// registry holds all application metrics. All fields are guarded by mu.
var registry = struct {
	sync.RWMutex

	reembedEnabled   map[string]*gauge
	reembedSkipped   map[string]*counter
	reembedCompleted map[string]*counter
}{
	reembedEnabled:   make(map[string]*gauge),
	reembedSkipped:   make(map[string]*counter),
	reembedCompleted: make(map[string]*counter),
}

// defaultBuckets for latency histograms (seconds).
var defaultBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

func getCounter(m map[string]*counter, key string) *counter {
	c, ok := m[key]
	if !ok {
		c = &counter{}
		m[key] = c
	}
	return c
}

func getGauge(m map[string]*gauge, key string) *gauge {
	g, ok := m[key]
	if !ok {
		g = &gauge{}
		m[key] = g
	}
	return g
}

// SetReembedEnabled sets the reembed_enabled gauge (1 = on, 0 = off).
func SetReembedEnabled(enabled bool) {
	registry.Lock()
	defer registry.Unlock()
	g := getGauge(registry.reembedEnabled, "default")
	if enabled {
		g.value = 1
	} else {
		g.value = 0
	}
}

// IncrReembedSkipped increments the reembed_skipped_total counter.
// Reason values: "kill_switch", "no_profile", "no_fields", "no_change".
func IncrReembedSkipped(reason string) {
	registry.Lock()
	defer registry.Unlock()
	getCounter(registry.reembedSkipped, reason).value++
}

// IncrReembedCompleted increments the reembed_completed_total counter.
// Status values: "ok", "embed_err", "upsert_err".
func IncrReembedCompleted(status string) {
	registry.Lock()
	defer registry.Unlock()
	getCounter(registry.reembedCompleted, status).value++
}

// Render returns Prometheus text-format lines for all metrics in this package.
// Called by the main metricsHandler so Grafana can scrape them.
func Render() string {
	var sb strings.Builder
	write := func(s string) { sb.WriteString(s) }

	write(renderFamily(family{
		name:       "reembed_enabled",
		help:       "Whether re-embedding is enabled (1) or paused via kill switch (0).",
		metricType: "gauge",
		keys:       []string{"dummy"},
		gauges:     registry.reembedEnabled,
	}))

	write(renderFamily(family{
		name:       "reembed_skipped_total",
		help:       "Total re-embed attempts skipped by reason.",
		metricType: "counter",
		keys:       []string{"reason"},
		counters:   registry.reembedSkipped,
	}))

	write(renderFamily(family{
		name:       "reembed_completed_total",
		help:       "Total re-embed jobs completed by status.",
		metricType: "counter",
		keys:       []string{"status"},
		counters:   registry.reembedCompleted,
	}))

	return sb.String()
}

// ------------------------------------------------------------------
// Rendering helpers (duplicated from metrics_handler.go but kept
// self-contained to avoid pulling in the handler's global registry).
// ------------------------------------------------------------------

type family struct {
	name       string
	help       string
	metricType string
	keys       []string
	counters   map[string]*counter
	histograms map[string]*histogram
	gauges     map[string]*gauge
}

func renderFamily(fam family) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# HELP %s %s\n", fam.name, fam.help)
	fmt.Fprintf(&sb, "# TYPE %s %s\n", fam.name, fam.metricType)

	type entry struct {
		key    string
		values []string
	}
	var entries []entry
	for k := range fam.counters {
		entries = append(entries, entry{key: k, values: strings.Split(k, "|")})
	}
	for k := range fam.gauges {
		entries = append(entries, entry{key: k, values: strings.Split(k, "|")})
	}
	for k := range fam.histograms {
		entries = append(entries, entry{key: k, values: strings.Split(k, "|")})
	}

	for _, e := range entries {
		labels := buildLabels(fam.keys, e.values)
		switch fam.metricType {
		case "counter":
			c := fam.counters[e.key]
			if c != nil {
				fmt.Fprintf(&sb, "%s%s %s\n", fam.name, labels, formatFloat(c.value))
			}
		case "gauge":
			g := fam.gauges[e.key]
			if g != nil {
				fmt.Fprintf(&sb, "%s%s %s\n", fam.name, labels, formatFloat(g.value))
			}
		case "histogram":
			h := fam.histograms[e.key]
			if h == nil {
				continue
			}
			bounds := make([]float64, 0, len(h.buckets))
			for b := range h.buckets {
				bounds = append(bounds, b)
			}
			for _, bound := range bounds {
				fmt.Fprintf(&sb, "%s_bucket%s %d\n", fam.name, labelsWithLe(labels, bound), uint64(h.buckets[bound]))
			}
			fmt.Fprintf(&sb, "%s_count%s %d\n", fam.name, labels, h.count)
			fmt.Fprintf(&sb, "%s_sum%s %s\n", fam.name, labels, formatFloat(h.sum))
		}
	}
	return sb.String()
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
	return labels[:len(labels)-1] + fmt.Sprintf(",le=%q}", formatFloat(le))
}

func formatFloat(f float64) string {
	if f == math.Trunc(f) && !math.IsInf(f, 0) && !math.IsNaN(f) {
		return fmt.Sprintf("%.0f", f)
	}
	return fmt.Sprintf("%g", f)
}
