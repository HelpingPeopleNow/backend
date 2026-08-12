package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/ports"
	"github.com/redis/go-redis/v9"
)

// sseChannelPrefix is the Redis channel name prefix for cross-replica SSE
// fan-out (GAP A fix — see infra/docs/FOLLOW_UP_SPOF_Backup_Replicas.md).
//
// Publish writes the event as JSON to `sse:user:<userID>`; every replica
// subscribed to that channel decodes the event and delivers it to its
// in-process subscribers. Channel-per-user (rather than a single channel
// for all events) avoids fan-out O(N) on every publish — a replica with
// no subscribers for a given user pays zero decoding cost on events for
// that user.
const sseChannelPrefix = "sse:user:"

// sseChannel returns the Redis channel name for a given userID.
func sseChannel(userID string) string { return sseChannelPrefix + userID }

// envelope is the wire format for cross-replica SSE events. It is the
// JSON-serialized shape written to Redis pub/sub; receivers decode back
// to ports.Event. Type is denormalized into the envelope (instead of
// nested) so the wire format is self-describing and survives encoding
// refactors.
type envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// redisBroker multiplexes an in-process broker with a Redis pub/sub
// layer so that SSE events published on one replica reach subscribers
// attached to any other replica. The single-replica behaviour is
// unchanged when REDIS_URL is empty (use the in-process NewSSEBroker
// constructor); this struct is only constructed when REDIS_URL is set.
//
// Per-replica state:
//   - local: the in-process broker (existing behaviour).
//   - pubsub: one channel per Subscribe call, tied to ctx lifetime.
//   - subs:   goroutine-started in Subscribe that pumps the channel
//     into the local broker while the subscription is alive.
//
// Concurrency: Subscribe / Publish / ActiveConnections take the in-process
// mutex briefly to look up / mutate local state; the Redis I/O happens
// outside the lock. Publish always publishes to Redis (even when no local
// subscribers exist) so other replicas still receive the event.
type redisBroker struct {
	local ports.Broker
	rdb   *redis.Client

	mu      sync.Mutex
	cancels map[string]map[int]context.CancelFunc // userID → subscriberID → cancel
	nextID  int

	// subscribed: tracks which userIDs have an active Redis
	// subscription on this replica. We PSUBSCRIBE / unsubscribe on
	// demand so a replica with no clients never pays Redis traffic.
	subscribed map[string]struct{}
}

// NewRedisSSEBroker wraps the in-process broker with a Redis pub/sub
// layer. If redisURL is empty, returns the in-process broker
// (graceful degradation — same behaviour as before this change).
//
// On a Redis dial failure during construction the returned broker falls
// back to in-process only and logs a warning so the operator can fix
// REDIS_URL without the backend failing to start.
//
// The second return value is a closer to release the Redis connection
// on shutdown; for the in-process fallback it is a no-op.
func NewRedisSSEBroker(redisURL string, local ports.Broker) (ports.Broker, func() error, error) {
	if redisURL == "" {
		return local, func() error { return nil }, nil
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, nil, fmt.Errorf("realtime: invalid REDIS_URL: %w", err)
	}
	rdb := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		// Close the client so we don't leak the connection.
		_ = rdb.Close()
		slog.Warn("realtime: redis ping failed; falling back to in-process broker only",
			"redis_url", redisURL, "error", err)
		return local, func() error { return nil }, nil
	}
	slog.Info("realtime: redis-backed SSE broker initialised", "redis_url", redisURL)
	rb := &redisBroker{
		local:      local,
		rdb:        rdb,
		cancels:    make(map[string]map[int]context.CancelFunc),
		subscribed: make(map[string]struct{}),
	}
	return rb, rb.Close, nil
}

// Close shuts down the Redis client. The caller (main) is responsible
// for invoking it on shutdown so subscribers see channel close and the
// underlying connection is released cleanly. Idempotent — a second
// call returns nil (the underlying redis-go Close returns
// "redis: client is closed" once closed).
func (b *redisBroker) Close() error {
	b.mu.Lock()
	for _, m := range b.cancels {
		for _, cancel := range m {
			cancel()
		}
	}
	b.cancels = nil
	b.subscribed = nil
	b.mu.Unlock()
	if err := b.rdb.Close(); err != nil && err.Error() != "redis: client is closed" {
		return err
	}
	return nil
}

// Subscribe attaches a local subscription AND, if this is the first
// local subscriber for the user on this replica, opens a Redis
// subscription to that user's channel. Inbound Redis events are fanned
// into the in-process broker so the local subscription path is
// unchanged.
func (b *redisBroker) Subscribe(ctx context.Context, userID string) (<-chan ports.Event, error) {
	ch, err := b.local.Subscribe(ctx, userID)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	if b.cancels[userID] == nil {
		b.cancels[userID] = make(map[int]context.CancelFunc)
	}
	subCtx, cancel := context.WithCancel(context.Background())
	b.cancels[userID][id] = cancel
	firstForUser := len(b.cancels[userID]) == 1
	b.mu.Unlock()

	if firstForUser {
		if err := b.ensureRedisSubscription(subCtx, userID); err != nil {
			cancel()
			// Best-effort: cancel the local subscription by closing its ctx.
			// If we can't open the Redis subscription we still keep the
			// local one; the user just won't see cross-replica events.
			slog.Warn("realtime: failed to open redis subscription; events from other replicas will be missed",
				"user_id", userID, "error", err)
		}
	}

	// Tear-down hook: when the caller's ctx is done, cancel our
	// Redis subscription slot. If no other subscribers remain for this
	// user on this replica, also unsubscribe from Redis.
	go func() {
		<-ctx.Done()
		b.mu.Lock()
		if m, ok := b.cancels[userID]; ok {
			if c, ok := m[id]; ok {
				c()
				delete(m, id)
			}
			if len(m) == 0 {
				delete(b.cancels, userID)
				delete(b.subscribed, userID)
			}
		}
		b.mu.Unlock()
	}()

	return ch, nil
}

// ensureRedisSubscription opens a Pub/Sub on the user's channel and
// pumps inbound envelopes into the in-process broker while subCtx is
// alive. The function blocks until subCtx is done or the underlying
// Redis connection errors out (in which case it retries every
// reconnectBackoff).
func (b *redisBroker) ensureRedisSubscription(ctx context.Context, userID string) error {
	ps := b.rdb.Subscribe(ctx, sseChannel(userID))
	// Wait for confirmation that subscription is created before
	// publishing anything (ReceiveOnce returns when the server confirms).
	if _, err := ps.Receive(ctx); err != nil {
		_ = ps.Close()
		return err
	}
	b.mu.Lock()
	b.subscribed[userID] = struct{}{}
	b.mu.Unlock()

	go b.pumpRedis(ctx, ps, userID)
	return nil
}

const reconnectBackoff = 2 * time.Second

// pumpRedis reads messages off the Redis pub/sub channel and forwards
// them into the in-process broker. On error (connection closed,
// timeout) it retries the subscription after a short backoff. The loop
// exits when ctx is done.
func (b *redisBroker) pumpRedis(ctx context.Context, ps *redis.PubSub, userID string) {
	ch := ps.Channel()
	for {
		select {
		case <-ctx.Done():
			_ = ps.Close()
			return
		case msg, ok := <-ch:
			if !ok {
				// Channel closed by Redis client (network blip or shutdown).
				// Retry after backoff unless ctx is done.
				if ctx.Err() != nil {
					return
				}
				time.Sleep(reconnectBackoff)
				newPS := b.rdb.Subscribe(ctx, sseChannel(userID))
				if _, err := newPS.Receive(ctx); err != nil {
					_ = newPS.Close()
					continue
				}
				ps = newPS
				ch = ps.Channel()
				continue
			}
			var env envelope
			if err := json.Unmarshal([]byte(msg.Payload), &env); err != nil {
				slog.Warn("realtime: redis envelope decode failed",
					"user_id", userID, "error", err)
				continue
			}
			// Payload is json.RawMessage; re-marshal into an any so
			// downstream JSON encoding works without a typed shape.
			var payload any
			if len(env.Payload) > 0 {
				if err := json.Unmarshal(env.Payload, &payload); err != nil {
					slog.Warn("realtime: redis payload decode failed",
						"user_id", userID, "error", err)
					continue
				}
			}
			if err := b.local.Publish(userID, ports.Event{
				Type:    env.Type,
				Payload: payload,
			}); err != nil {
				slog.Warn("realtime: local publish from redis failed",
					"user_id", userID, "error", err)
			}
		}
	}
}

// Publish fans out the event to local subscribers (existing in-process
// path) AND publishes the JSON envelope to Redis so other replicas
// receive the same event. A Redis publish failure is logged but does
// not fail the publish — local subscribers still get the event.
func (b *redisBroker) Publish(userID string, event ports.Event) error {
	if err := b.local.Publish(userID, event); err != nil {
		return err
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		slog.Warn("realtime: marshal payload for redis publish", "error", err)
		return nil
	}
	env := envelope{Type: event.Type, Payload: payload}
	data, err := json.Marshal(env)
	if err != nil {
		slog.Warn("realtime: marshal envelope for redis publish", "error", err)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := b.rdb.Publish(ctx, sseChannel(userID), data).Err(); err != nil {
		slog.Warn("realtime: redis publish failed (local subscribers still received event)",
			"user_id", userID, "event_type", event.Type, "error", err)
	}
	return nil
}

// ActiveConnections delegates to the in-process broker so the
// `sse_active_connections` gauge continues to reflect only the
// connections attached to THIS replica (not the cluster total —
// Prometheus scrapes per replica and sums on the server side).
func (b *redisBroker) ActiveConnections() int { return b.local.ActiveConnections() }

// Ensure interface compliance at compile time.
var _ ports.Broker = (*redisBroker)(nil)

// ErrNoRedisBroker is returned when a caller expects a *redisBroker
// but gets an in-process one (e.g. from Close-driven shutdown paths).
// It is unused today but reserved so a Close-driven shutdown can fail
// fast instead of silently no-op.
var ErrNoRedisBroker = errors.New("realtime: redis broker not configured")
