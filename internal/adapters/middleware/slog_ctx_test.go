package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureHandler is a minimal slog.Handler that stores the most recent
// record it receives. Used so tests can assert the request_id attr was
// injected (or not) without binding to text-format edge cases.
//
// WithAttrs / WithGroup return a NEW handler that merges the With'd attrs
// into the record's per-Handle attrs (slog's Handler contract). The
// returned handler shares `lastAttrs` with the parent so the test sees
// all Handle calls without re-binding.
type captureHandler struct {
	mu        sync.Mutex
	pinned    []slog.Attr
	lastAttrs map[string]any
}

func (c *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

// lazyInit creates the shared lastAttrs map (under the mutex) if nil.
// Used by Handle and WithAttrs to ensure that the parent cap variable
// resolves to the same map as the child handler returned from WithAttrs.
func (c *captureHandler) lazyInit() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastAttrs == nil {
		c.lastAttrs = map[string]any{}
	}
}

func (c *captureHandler) Handle(_ context.Context, r slog.Record) error {
	c.lazyInit()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, a := range c.pinned {
		c.lastAttrs[a.Key] = a.Value.Any()
	}
	r.Attrs(func(a slog.Attr) bool {
		c.lastAttrs[a.Key] = a.Value.Any()
		return true
	})
	return nil
}

func (c *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Force lastAttrs creation BEFORE the child shares the reference,
	// otherwise the child's Handle would lazily init a separate map the
	// parent test variable cannot observe.
	c.lazyInit()
	merged := make([]slog.Attr, 0, len(c.pinned)+len(attrs))
	merged = append(merged, c.pinned...)
	merged = append(merged, attrs...)
	return &captureHandler{pinned: merged, lastAttrs: c.lastAttrs}
}

func (c *captureHandler) WithGroup(_ string) slog.Handler {
	// Group semantics are intentionally NOT implemented here — all attrs
	// live in the same flat namespace. Tests assert key presence; rename
	// into a group would require reworking the assertion shape.
	c.lazyInit()
	return &captureHandler{pinned: c.pinned, lastAttrs: c.lastAttrs}
}

func TestContextHandlerInjectsRequestIDWhenPresent(t *testing.T) {
	cap := &captureHandler{}
	h := NewContextHandler(cap)
	ctx := contextkeys.SetRequestID(context.Background(), "test-req-id-123")

	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
	require.NoError(t, h.Handle(ctx, rec))

	assert.Equal(t, "test-req-id-123", cap.lastAttrs["request_id"],
		"ContextHandler must inject request_id from ctx when set")
}

func TestContextHandlerOmitsRequestIDWhenAbsent(t *testing.T) {
	cap := &captureHandler{}
	h := NewContextHandler(cap)
	ctx := context.Background() // no request_id

	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
	require.NoError(t, h.Handle(ctx, rec))

	_, present := cap.lastAttrs["request_id"]
	assert.False(t, present,
		"no request_id in ctx → handler must NOT add the attr (keeps logs clean)")
}

func TestContextHandlerEnabledPropagates(t *testing.T) {
	inner := &captureHandler{}
	h := NewContextHandler(inner)

	assert.True(t, h.Enabled(context.Background(), slog.LevelInfo))
	// We don't have a "false" path because captureHandler.Enabled is hard-coded
	// true; this is fine — the wrapping is straightforward delegation.
}

func TestContextHandlerWithAttrsComposes(t *testing.T) {
	// WithAttrs must return a Handler that still injects request_id — the
	// composition is what slog.Default().With(...) relies on.
	cap := &captureHandler{}
	base := NewContextHandler(cap)
	withTag := base.WithAttrs([]slog.Attr{slog.String("tag", "v")})

	ctx := contextkeys.SetRequestID(context.Background(), "id-with-tag")
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
	require.NoError(t, withTag.Handle(ctx, rec))

	assert.Equal(t, "id-with-tag", cap.lastAttrs["request_id"])
	assert.Equal(t, "v", cap.lastAttrs["tag"], "WithAttrs attr must propagate")
}

func TestContextHandlerEndToEndJSONFormat(t *testing.T) {
	// End-to-end: write a real slog with the ContextHandler wrapping
	// JSONHandler; parse the result and confirm request_id is in
	// upstream attrs, not in top-level extra fields.
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	h := NewContextHandler(base)
	logger := slog.New(h)

	ctx := contextkeys.SetRequestID(context.Background(), "e2e-id-zzz")
	logger.InfoContext(ctx, "hello", "k", "v")

	out := strings.TrimSpace(buf.String())
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &got))

	assert.Equal(t, "e2e-id-zzz", got["request_id"],
		"JSON output must contain a top-level request_id attr")
	assert.Equal(t, "v", got["k"], "user-supplied attrs preserved")
	assert.Equal(t, "hello", got["msg"])
}

func TestContextHandlerDoesNotBreakSlogDefaultChaining(t *testing.T) {
	// slog.New(handler).With(...) chains must still work. The chain
	// operation threads through WithAttrs (which we implemented); if it
	// didn't, logger.With(...).Info(...) would silently drop attrs.
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	h := NewContextHandler(base)
	logger := slog.New(h).With("component", "auth")

	ctx := contextkeys.SetRequestID(context.Background(), "chained-id")
	logger.InfoContext(ctx, "event")

	out := buf.String()
	assert.Contains(t, out, "request_id=chained-id")
	assert.Contains(t, out, "component=auth")
	assert.Contains(t, out, "msg=event")
}
