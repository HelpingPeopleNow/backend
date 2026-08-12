package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter is the per-key rate-limit contract used by handlers. Both the
// in-process RateLimiter and the SharedRateLimiter satisfy it so callers
// don't need to care which backend is active.
type Limiter interface {
	Allow(key string) bool
}

// SharedRateLimiter is a per-key rate limiter backed by a Redis sliding
// window so multiple backend replicas share one quota (SPOF GAP D fix —
// see infra/docs/FOLLOW_UP_SPOF_Backup_Replicas.md). Without it, every
// replica's in-process bucket is consulted independently and effective
// limits become N× the intended cap.
//
// Algorithm: a fixed-window counter keyed `rl:<scope>:<key>`. On each
// Allow call:
//  1. INCR the counter atomically.
//  2. If the returned value == 1 (first hit in this window), set EXPIRE
//     to the window length so the counter self-cleans.
//  3. If the value exceeds the configured max, deny.
//
// INCR + EXPIRE is racy in the worst case (a TTL-less counter that
// survives forever if the process crashes between step 1 and step 2),
// so the Lua script below is used to make the pair atomic server-side.
// The window size is `period`; the cap is `max`.
//
// Failure handling: any Redis error causes the limiter to fall back to
// the embedded in-process limiter and log a warning. Operators see the
// "rate_limit_fallback_total{scope=...}" counter increment so they
// notice the degradation; the in-process limiter still enforces SOME
// cap on this replica alone (the pre-fix behaviour). Once Redis is
// healthy again the next Allow call returns to the Redis path.
type SharedRateLimiter struct {
	rdb   *redis.Client
	local Limiter // fallback when Redis is unhealthy
	scope string  // redis key prefix so chat/DM/feedback counters don't collide
	max   int
	win   time.Duration

	// fallbackTotal counts the number of Allow calls that fell back to
	// the in-process limiter because of a Redis error. Surfaced as a
	// Prometheus counter via RegisterCounter (rendered by the
	// metrics package) — see IncrRateLimitFallback in metrics.go.
	fallbackTotal func(reason string)
}

// allowScript is the atomic INCR + conditional EXPIRE. EVAL runs on
// every Redis node that hosts this limiter and is the only network call
// in the hot path. Returns the post-increment counter value.
var allowScript = redis.NewScript(`
local v = redis.call('INCR', KEYS[1])
if v == 1 then
    redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return v
`)

// NewSharedRateLimiter wires a Redis-backed sliding-window limiter with
// the supplied in-process fallback. redisURL is the standard
// redis://[user:pass@]host:port/db form (matches REDIS_URL).
//
// scope distinguishes counter buckets so chat, DM, and feedback don't
// share a quota (e.g. "chat", "dm-send", "feedback").
//
// A Redis dial failure at startup logs a warning and returns a limiter
// that ONLY uses the in-process fallback for the lifetime of the
// process — same behaviour as the pre-fix path. Operators should set
// REDIS_URL on every replica.
func NewSharedRateLimiter(redisURL, scope string, max int, period time.Duration, fallback Limiter) (*SharedRateLimiter, error) {
	sl := &SharedRateLimiter{
		local:         fallback,
		scope:         scope,
		max:           max,
		win:           period,
		fallbackTotal: func(string) {},
	}
	if redisURL == "" {
		return sl, nil
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("ratelimit: invalid REDIS_URL: %w", err)
	}
	sl.rdb = redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := sl.rdb.Ping(ctx).Err(); err != nil {
		_ = sl.rdb.Close()
		sl.rdb = nil
		slog.Warn("ratelimit: redis ping failed; limiter will use in-process fallback only",
			"scope", scope, "error", err)
		return sl, nil
	}
	slog.Info("ratelimit: shared redis-backed limiter active", "scope", scope, "max", max, "window", period)
	return sl, nil
}

// SetFallbackCounter wires a callback invoked when Allow falls back to
// the in-process limiter. Called by main once the metrics package is up.
func (s *SharedRateLimiter) SetFallbackCounter(inc func(reason string)) {
	if inc == nil {
		s.fallbackTotal = func(string) {}
		return
	}
	s.fallbackTotal = inc
}

// Allow implements the Limiter contract. On Redis success the returned
// bool reflects the cross-replica quota; on any Redis error it falls
// back to the embedded in-process limiter for THIS replica only.
func (s *SharedRateLimiter) Allow(key string) bool {
	if s.rdb == nil || s.local == nil {
		if s.local != nil {
			return s.local.Allow(key)
		}
		return true // no limiter configured — fail open
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	v, err := allowScript.Run(ctx, s.rdb, []string{"rl:" + s.scope + ":" + key}, s.win.Milliseconds()).Int64()
	if err != nil {
		s.fallbackTotal("redis_error")
		slog.Warn("ratelimit: redis script failed; using in-process fallback",
			"scope", s.scope, "key", key, "error", err)
		return s.local.Allow(key)
	}
	if v > int64(s.max) {
		slog.Debug("ratelimit: shared limit hit", "scope", s.scope, "key", key, "count", v, "max", s.max)
		return false
	}
	return true
}

// Close releases the Redis client. Idempotent — a second call is a
// no-op (matches the Redis-backed SSE broker convention).
func (s *SharedRateLimiter) Close() error {
	if s.rdb == nil {
		return nil
	}
	if err := s.rdb.Close(); err != nil && err.Error() != "redis: client is closed" {
		return err
	}
	return nil
}
