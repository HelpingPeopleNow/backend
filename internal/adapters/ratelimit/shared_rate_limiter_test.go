package ratelimit

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSharedLimiter(t *testing.T, max int, period time.Duration) (*SharedRateLimiter, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	fallback := NewRateLimiter(max, period)
	sl, err := NewSharedRateLimiter("redis://"+mr.Addr(), "test", max, period, fallback)
	require.NoError(t, err)
	// Inject the test miniredis-backed client directly so the limiter
	// uses our instance instead of building its own from redisURL.
	sl.rdb = rdb
	t.Cleanup(func() { _ = sl.Close() })
	return sl, mr
}

func TestSharedRateLimiterEmptyURLFallsBack(t *testing.T) {
	fallback := NewRateLimiter(2, time.Minute)
	sl, err := NewSharedRateLimiter("", "test", 2, time.Minute, fallback)
	require.NoError(t, err)
	// Without a Redis client, Allow should defer entirely to the fallback.
	assert.True(t, sl.Allow("user1"))
	assert.True(t, sl.Allow("user1"))
	assert.False(t, sl.Allow("user1"))
}

func TestSharedRateLimiterInvalidURL(t *testing.T) {
	_, err := NewSharedRateLimiter("://nope", "test", 1, time.Minute, NewRateLimiter(1, time.Minute))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "REDIS_URL")
}

func TestSharedRateLimiterEnforcesLimit(t *testing.T) {
	sl, _ := newTestSharedLimiter(t, 3, time.Minute)

	for i := 0; i < 3; i++ {
		assert.True(t, sl.Allow("user1"), "request %d should be allowed", i+1)
	}
	// 4th must be denied.
	assert.False(t, sl.Allow("user1"))
}

func TestSharedRateLimiterPerKeyIsolation(t *testing.T) {
	sl, _ := newTestSharedLimiter(t, 2, time.Minute)
	// user1 exhausts its quota; user2 must still be allowed.
	assert.True(t, sl.Allow("user1"))
	assert.True(t, sl.Allow("user1"))
	assert.False(t, sl.Allow("user1"))
	assert.True(t, sl.Allow("user2"))
	assert.True(t, sl.Allow("user2"))
	assert.False(t, sl.Allow("user2"))
}

func TestSharedRateLimiterCrossReplicaEquivalent(t *testing.T) {
	// Two limiter instances pointed at the same miniredis simulate two
	// replicas. A user's quota must be shared across them — this is the
	// GAP D regression guard (pre-fix, each replica tracked its own
	// in-process bucket and N replicas gave N× the configured cap).
	sl1, mr := newTestSharedLimiter(t, 5, time.Minute)
	rdb2 := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb2.Close() })
	fallback2 := NewRateLimiter(5, time.Minute)
	sl2, err := NewSharedRateLimiter("redis://"+mr.Addr(), "test", 5, time.Minute, fallback2)
	require.NoError(t, err)
	sl2.rdb = rdb2
	t.Cleanup(func() { _ = sl2.Close() })

	// 3 hits on replica A.
	for i := 0; i < 3; i++ {
		require.True(t, sl1.Allow("user1"))
	}
	// 2 hits on replica B should fill the quota.
	assert.True(t, sl2.Allow("user1"))
	assert.True(t, sl2.Allow("user1"))
	// 6th hit on either replica is rejected.
	assert.False(t, sl1.Allow("user1"))
	assert.False(t, sl2.Allow("user1"))
}

func TestSharedRateLimiterFallsBackOnRedisError(t *testing.T) {
	sl, mr := newTestSharedLimiter(t, 2, time.Minute)
	// Stop miniredis — every Redis op from this point errors out.
	mr.Close()
	// Allow should fall back to the in-process limiter and still enforce
	// SOME cap on this replica alone.
	assert.True(t, sl.Allow("user1"))
	assert.True(t, sl.Allow("user1"))
	assert.False(t, sl.Allow("user1"))
}

func TestSharedRateLimiterCloseIdempotent(t *testing.T) {
	sl, _ := newTestSharedLimiter(t, 1, time.Minute)
	require.NoError(t, sl.Close())
	// Second close must not panic and must not return "client is closed".
	require.NoError(t, sl.Close())
}

func TestSharedRateLimiterContextRespected(t *testing.T) {
	sl, mr := newTestSharedLimiter(t, 1, time.Minute)
	// Hammer Allow while the limiter's script run path has a tight
	// deadline; we don't assert on the return value, only that the call
	// returns within a reasonable time (no hang).
	done := make(chan bool, 1)
	go func() {
		_ = sl.Allow("user1")
		done <- true
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Allow did not return within 2s")
	}
	_ = mr
}
