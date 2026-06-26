package ratelimit

import (
	"log/slog"
	"sync"
	"time"
)

// RateLimiter is a per-user token-bucket rate limiter.
// Thread-safe for concurrent use across HTTP requests.
type RateLimiter struct {
	mu     sync.Mutex
	bucket map[string]*userBucket
	rate   int           // tokens per period
	period time.Duration // refill interval
	now    func() time.Time
}

type userBucket struct {
	tokens   float64
	lastSeen time.Time
}

// NewRateLimiter creates a rate limiter that allows `rate` requests per `period`.
func NewRateLimiter(rate int, period time.Duration) *RateLimiter {
	return NewRateLimiterWithClock(rate, period, time.Now)
}

// NewRateLimiterWithClock creates a rate limiter with an injectable clock for testing.
func NewRateLimiterWithClock(rate int, period time.Duration, now func() time.Time) *RateLimiter {
	rl := &RateLimiter{
		bucket: make(map[string]*userBucket),
		rate:   rate,
		period: period,
		now:    now,
	}
	go rl.cleanup(period * 3)
	return rl
}

// Allow checks whether a key (e.g. user ID) is allowed to make a request.
// Returns true if allowed, false if rate-limited.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.bucket[key]
	if !ok {
		b = &userBucket{tokens: float64(rl.rate - 1), lastSeen: rl.now()}
		rl.bucket[key] = b
		return true
	}

	// Refill tokens based on elapsed time
	elapsed := rl.now().Sub(b.lastSeen)
	refill := float64(rl.rate) * elapsed.Seconds() / rl.period.Seconds()
	b.tokens = min(float64(rl.rate), b.tokens+refill)
	b.lastSeen = rl.now()

	if b.tokens < 1 {
		slog.Debug("rate limit hit", "key", key)
		return false
	}
	b.tokens--
	return true
}

// cleanup periodically removes inactive buckets to prevent memory leaks.
func (rl *RateLimiter) cleanup(maxAge time.Duration) {
	ticker := time.NewTicker(maxAge / 3)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := rl.now()
		for k, b := range rl.bucket {
			if now.Sub(b.lastSeen) > maxAge {
				delete(rl.bucket, k)
			}
		}
		rl.mu.Unlock()
	}
}
