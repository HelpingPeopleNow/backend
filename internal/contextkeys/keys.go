package contextkeys

import "context"

type contextKey string

const (
	UserIDKey    contextKey = "user_id"
	IsAdminKey   contextKey = "is_admin"
	RequestIDKey contextKey = "request_id"
)

func GetUserID(ctx context.Context) string {
	v, _ := ctx.Value(UserIDKey).(string)
	return v
}

func SetUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, UserIDKey, id)
}

func GetIsAdmin(ctx context.Context) bool {
	v, _ := ctx.Value(IsAdminKey).(bool)
	return v
}

func SetIsAdmin(ctx context.Context, isAdmin bool) context.Context {
	return context.WithValue(ctx, IsAdminKey, isAdmin)
}

// GetRequestID returns the per-request correlation ID set by the
// middleware/RequestID middleware, or "" if not set. (P3-4 audit.)
func GetRequestID(ctx context.Context) string {
	v, _ := ctx.Value(RequestIDKey).(string)
	return v
}

// SetRequestID stores the correlation ID in ctx. Callers outside the
// middleware should generally not set this — it is the middleware's job.
func SetRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, RequestIDKey, id)
}
