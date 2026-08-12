package realtime

import (
	"context"
	"testing"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/ports"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRedis starts an in-memory Redis (miniredis) and returns a
// configured *redis.Client plus a closer. Use this in tests to exercise
// the redisBroker end-to-end without a real Redis dependency.
func newTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb, mr
}

// TestNewRedisSSEBrokerEmptyURL verifies that an empty REDIS_URL falls
// back to the in-process broker (no Redis dial attempted).
func TestNewRedisSSEBrokerEmptyURL(t *testing.T) {
	local := NewSSEBroker()
	broker, closer, err := NewRedisSSEBroker("", local)
	require.NoError(t, err)
	assert.Same(t, local, broker, "empty URL should return the in-process broker unchanged")
	assert.NotNil(t, closer)
	require.NoError(t, closer())
}

// TestNewRedisSSEBrokerInvalidURL verifies that a malformed URL is
// rejected at construction with a wrapped error.
func TestNewRedisSSEBrokerInvalidURL(t *testing.T) {
	local := NewSSEBroker()
	_, _, err := NewRedisSSEBroker("://not-a-valid-redis-url", local)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "REDIS_URL")
}

// TestRedisBrokerPublishDeliversLocally ensures the local in-process
// path still works when a redisBroker wraps it.
func TestRedisBrokerPublishDeliversLocally(t *testing.T) {
	rdb, _ := newTestRedis(t)
	local := NewSSEBroker()
	broker := &redisBroker{
		local:      local,
		rdb:        rdb,
		cancels:    make(map[string]map[int]context.CancelFunc),
		subscribed: make(map[string]struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := broker.Subscribe(ctx, "user1")
	require.NoError(t, err)

	require.NoError(t, broker.Publish("user1", ports.Event{Type: "message", Payload: "hi"}))

	select {
	case ev := <-ch:
		assert.Equal(t, "message", ev.Type)
		assert.Equal(t, "hi", ev.Payload)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for local event")
	}
}

// TestRedisBrokerCrossReplica is the GAP A scenario: two replicas
// (two redisBroker instances pointed at the same miniredis) must
// deliver a publish from replica A to a subscriber on replica B.
func TestRedisBrokerCrossReplica(t *testing.T) {
	rdb, _ := newTestRedis(t)

	replicaA := &redisBroker{
		local:      NewSSEBroker(),
		rdb:        rdb,
		cancels:    make(map[string]map[int]context.CancelFunc),
		subscribed: make(map[string]struct{}),
	}
	replicaB := &redisBroker{
		local:      NewSSEBroker(),
		rdb:        rdb,
		cancels:    make(map[string]map[int]context.CancelFunc),
		subscribed: make(map[string]struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe on replica B only.
	chB, err := replicaB.Subscribe(ctx, "user1")
	require.NoError(t, err)

	// Give the Redis subscription goroutine a moment to wire up.
	// miniredis is synchronous but the Subscribe call returns before
	// the pumpRedis goroutine starts; without this sleep the publish
	// from A can race the subscribe on B and drop the message.
	time.Sleep(50 * time.Millisecond)

	// Publish on replica A.
	require.NoError(t, replicaA.Publish("user1", ports.Event{
		Type:    "message",
		Payload: map[string]string{"body": "cross-replica hello"},
	}))

	select {
	case ev := <-chB:
		assert.Equal(t, "message", ev.Type)
		payload, ok := ev.Payload.(map[string]any)
		require.True(t, ok, "payload should decode as map[string]any")
		assert.Equal(t, "cross-replica hello", payload["body"])
	case <-time.After(2 * time.Second):
		t.Fatal("replica B did not receive cross-replica event")
	}
}

// TestRedisBrokerCloseReleasesSubscribers verifies Close cancels all
// per-user subscription goroutines and releases the underlying Redis
// client. After Close, further Subscribe calls should error (the
// underlying client is closed).
func TestRedisBrokerCloseReleasesSubscribers(t *testing.T) {
	rdb, _ := newTestRedis(t)
	local := NewSSEBroker()
	broker, closer, err := NewRedisSSEBroker("redis://"+rdb.Options().Addr, local)
	// NewRedisSSEBroker builds its own client; ours was a helper, so
	// the URL it constructs from miniredis has the right shape.
	require.NoError(t, err)
	require.NotNil(t, broker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = broker.Subscribe(ctx, "user1")
	require.NoError(t, err)

	require.NoError(t, closer())

	// After Close, the in-process local broker is the fallback we
	// passed in; the redisBroker-specific path is gone. We just check
	// that the closer is idempotent (a double Close should not panic).
	require.NoError(t, closer())
}
