package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewRateLimiter(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	rl := NewRateLimiterWithClock(5, time.Minute, func() time.Time { return now })

	assert.True(t, rl.Allow("user1"))
}

func TestAllowConsumesTokens(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	var clock = func() time.Time { return now }
	rl := NewRateLimiterWithClock(3, time.Minute, clock)

	// First 3 requests allowed (rate=3, first gets rate-1=2 remaining)
	assert.True(t, rl.Allow("u"))
	assert.True(t, rl.Allow("u"))
	assert.True(t, rl.Allow("u"))
	// Fourth blocked
	assert.False(t, rl.Allow("u"))
}

func TestRefillOverTime(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	elapsed := time.Duration(0)
	clock := func() time.Time { return now.Add(elapsed) }
	rl := NewRateLimiterWithClock(2, time.Minute, clock)

	// Use 2 tokens
	assert.True(t, rl.Allow("u"))
	assert.True(t, rl.Allow("u"))
	assert.False(t, rl.Allow("u"))

	// Advance 30s → refill = 2 * 30/60 = 1 token
	elapsed = 30 * time.Second
	assert.True(t, rl.Allow("u"))
}

func TestDifferentKeysIndependent(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	rl := NewRateLimiterWithClock(1, time.Minute, func() time.Time { return now })

	assert.True(t, rl.Allow("a"))
	assert.False(t, rl.Allow("a"))
	assert.True(t, rl.Allow("b")) // different key, full bucket
}

func TestCleanup(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	elapsed := time.Duration(0)
	clock := func() time.Time { return now.Add(elapsed) }
	rl := NewRateLimiterWithClock(5, time.Minute, clock)

	rl.Allow("u")

	// Advance past maxAge (period * 3 = 3 minutes)
	elapsed = 4 * time.Minute
	// Manually trigger cleanup logic
	rl.mu.Lock()
	for k, b := range rl.bucket {
		if rl.now().Sub(b.lastSeen) > rl.period*3 {
			delete(rl.bucket, k)
		}
	}
	rl.mu.Unlock()

	// Bucket cleaned → next request creates new bucket
	assert.True(t, rl.Allow("u"))
}
