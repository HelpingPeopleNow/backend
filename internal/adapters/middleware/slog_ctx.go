package middleware

import (
	"context"
	"log/slog"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
)

// ContextHandler is an slog.Handler decorator that injects context-derived
// attrs into every record before delegating to the wrapped handler.
// Today it adds `request_id`; future versions can add tenant/region/etc.
// without touching call sites (P3-4 audit cross-service tracing).
type ContextHandler struct {
	inner slog.Handler
}

// NewContextHandler wraps an existing slog.Handler so every record from
// any logger that uses slog.Default() (or this handler directly) carries
// the request_id from the call's context.
func NewContextHandler(inner slog.Handler) *ContextHandler {
	return &ContextHandler{inner: inner}
}

// Enabled delegates to the wrapped handler.
func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle extracts the per-request ID from ctx (if set) and merges it as
// an slog attr before delegating to the wrapped handler.
func (h *ContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := contextkeys.GetRequestID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.inner.Handle(ctx, r)
}

// WithAttrs returns a new handler with the extra attrs applied, so
// `logger.With(...)` keeps the context-injection behaviour.
func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ContextHandler{inner: h.inner.WithAttrs(attrs)}
}

// WithGroup returns a new handler with the named group applied; the
// context-injection behaviour is preserved.
func (h *ContextHandler) WithGroup(name string) slog.Handler {
	return &ContextHandler{inner: h.inner.WithGroup(name)}
}
