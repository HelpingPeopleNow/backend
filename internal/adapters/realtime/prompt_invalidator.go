package realtime

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/ports"
	"github.com/redis/go-redis/v9"
)

// PromptInvalidator is the cross-replica cache invalidation bus for
// system prompts (SPOF GAP B fix — see
// infra/docs/FOLLOW_UP_SPOF_Backup_Replicas.md). Wire-up:
//
//   - main.go constructs ONE PromptInvalidator at startup.
//   - It is wired into the GormSystemPromptRepository via SetInvalidator:
//     so every Update publishes an invalidation event.
//   - It is also started as a long-lived goroutine that subscribes to
//     the same channel and calls repo.InvalidateCache on every event.
//
// Wire format: a single string column name on channel
// `system_prompts:invalidate`. Producers write the column name (or `*`
// to mean "reload all"). Consumers treat the body as the column name.
//
// Failure mode: a Redis error on publish is logged and ignored — the
// local cache is already current, so the worst case is sibling
// replicas serving stale text until their next rolling deploy or a
// manual restart. Acceptable for a LOW gap. SetInvalidator is nil-safe
// (single-replica / dev never calls it).
type PromptInvalidator struct {
	rdb    *redis.Client
	repo   promptCacheReloader
	scope  string // redis channel prefix
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu     sync.Mutex
	closed bool
}

// promptCacheReloader is the minimal contract this invalidator depends on.
// Both *GormSystemPromptRepository (production) and test stubs satisfy
// it; the wider SystemPromptRepository interface is not required so
// unit tests don't have to mock Get/Update just to drive the subscribe
// path.
type promptCacheReloader interface {
	InvalidateCache(ctx context.Context) error
}

// NewPromptInvalidator constructs and starts the invalidator goroutine
// that subscribes to the Redis channel and reloads the repo cache on
// each event. redisURL follows the same format as REDIS_URL.
//
// An empty redisURL returns a no-op invalidator: PublishInvalidation
// returns nil (caller's Update path proceeds silently) and no
// goroutine is started. Single-replica / dev behaviour, unchanged.
func NewPromptInvalidator(redisURL string, repo promptCacheReloader) (ports.SystemPromptInvalidator, func() error, error) {
	pi := &PromptInvalidator{
		repo:  repo,
		scope: "system_prompts:invalidate",
	}
	if redisURL == "" {
		return pi, func() error { return nil }, nil
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, nil, err
	}
	pi.rdb = redis.NewClient(opts)
	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := pi.rdb.Ping(pingCtx).Err(); err != nil {
		_ = pi.rdb.Close()
		pi.rdb = nil
		slog.Warn("realtime: prompt invalidator ping failed; running in no-op mode",
			"redis_url", redisURL, "error", err)
		return pi, func() error { return nil }, nil
	}

	subCtx, subCancel := context.WithCancel(context.Background())
	pi.cancel = subCancel
	pi.wg.Add(1)
	go pi.run(subCtx)
	slog.Info("realtime: prompt invalidator started", "channel", pi.scope)
	return pi, pi.Close, nil
}

// PublishInvalidation writes the column name to the Redis channel so
// sibling replicas reload their caches. Fire-and-forget: a 500ms
// timeout bounds the call so a Redis hang never blocks the PUT
// handler. Empty column is treated as "*" (force reload).
func (pi *PromptInvalidator) PublishInvalidation(ctx context.Context, column string) error {
	if pi == nil || pi.rdb == nil {
		return nil
	}
	if column == "" {
		column = "*"
	}
	pubCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if err := pi.rdb.Publish(pubCtx, pi.scope, column).Err(); err != nil {
		return err
	}
	return nil
}

// run subscribes to the invalidation channel and reloads the repo
// cache on every event. Reconnects after backoff if the underlying
// pub/sub errors out. Exits when ctx is cancelled (shutdown drain).
func (pi *PromptInvalidator) run(ctx context.Context) {
	defer pi.wg.Done()
	for {
		if ctx.Err() != nil {
			return
		}
		ps := pi.rdb.Subscribe(ctx, pi.scope)
		if _, err := ps.Receive(ctx); err != nil {
			_ = ps.Close()
			if ctx.Err() != nil {
				return
			}
			time.Sleep(2 * time.Second)
			continue
		}
		ch := ps.Channel()
		// Drain phase: keep consuming until the channel closes (network
		// blip) or ctx is cancelled. On channel close we break out and
		// re-enter the outer loop to resubscribe.
		drained := false
		for !drained {
			select {
			case <-ctx.Done():
				_ = ps.Close()
				return
			case msg, ok := <-ch:
				if !ok {
					drained = true
					break
				}
				if err := pi.repo.InvalidateCache(ctx); err != nil {
					slog.Warn("realtime: cross-replica invalidation reload failed",
						"column", msg.Payload, "error", err)
					continue
				}
				slog.Info("realtime: cache invalidated by cross-replica event",
					"column", msg.Payload)
			}
		}
		_ = ps.Close()
		if ctx.Err() != nil {
			return
		}
		time.Sleep(2 * time.Second)
	}
}

// Close stops the subscription goroutine and the Redis client.
// Idempotent.
func (pi *PromptInvalidator) Close() error {
	pi.mu.Lock()
	if pi.closed {
		pi.mu.Unlock()
		return nil
	}
	pi.closed = true
	pi.mu.Unlock()
	if pi.cancel != nil {
		pi.cancel()
	}
	if pi != nil {
		pi.wg.Wait()
	}
	if pi.rdb != nil {
		if err := pi.rdb.Close(); err != nil && err.Error() != "redis: client is closed" {
			return err
		}
	}
	return nil
}
