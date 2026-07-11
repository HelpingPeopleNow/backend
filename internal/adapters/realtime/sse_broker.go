package realtime

import (
	"context"
	"log/slog"
	"sync"

	"github.com/HelpingPeopleNow/backend/internal/ports"
)

// maxSSESubsPerUser caps the number of in-process SSE subscriptions per
// user (P1-5 audit, F6). A handful of browser tabs/devices per user is
// reasonable; an unbounded count lets a single misbehaving client OOM
// the process by opening tabs in a loop. Enforced under b.mu so a
// concurrent subscribe/unsubscribe cannot race past the cap.
const maxSSESubsPerUser = 10

// sseBroker is an in-process pub/sub broker for SSE events.
// Each user can have multiple subscribers (multiple browser tabs/devices).
type sseBroker struct {
	mu   sync.RWMutex
	subs map[string][]chan ports.Event
}

// NewSSEBroker creates a new in-process broker.
func NewSSEBroker() ports.Broker {
	return &sseBroker{
		subs: make(map[string][]chan ports.Event),
	}
}

// ErrSSETooManySubscribers is returned when a user exceeds the per-user
// subscription cap.
var ErrSSETooManySubscribers = &sseCapError{}

type sseCapError struct{}

func (e *sseCapError) Error() string {
	return "sse subscription cap exceeded for user"
}

func (b *sseBroker) Subscribe(ctx context.Context, userID string) (<-chan ports.Event, error) {
	b.mu.Lock()
	if len(b.subs[userID]) >= maxSSESubsPerUser {
		slog.Warn("sse: subscription cap exceeded", "user_id", userID, "current", len(b.subs[userID]))
		b.mu.Unlock()
		return nil, ErrSSETooManySubscribers
	}
	ch := make(chan ports.Event, 32) // buffered to avoid blocking on slow consumers
	b.subs[userID] = append(b.subs[userID], ch)
	slog.Info("sse: subscribe", "user_id", userID, "total_subs", len(b.subs[userID]))
	b.mu.Unlock()

	// Cleanup when context is done
	go func() {
		<-ctx.Done()
		b.mu.Lock()
		subs := b.subs[userID]
		for i, c := range subs {
			if c == ch {
				b.subs[userID] = append(subs[:i], subs[i+1:]...)
				slog.Info("sse: unsubscribe", "user_id", userID)
				break
			}
		}
		if len(b.subs[userID]) == 0 {
			delete(b.subs, userID)
		}
		b.mu.Unlock()
		close(ch)
	}()

	return ch, nil
}

func (b *sseBroker) Publish(userID string, event ports.Event) error {
	b.mu.RLock()
	subs := b.subs[userID]
	// Copy slice to avoid holding lock during send
	chans := make([]chan ports.Event, len(subs))
	copy(chans, subs)
	b.mu.RUnlock()

	for _, ch := range chans {
		select {
		case ch <- event:
		default:
			// Subscriber buffer full — drop event to avoid blocking
			slog.Debug("sse: dropped event for slow subscriber",
				"user_id", userID, "event_type", event.Type)
		}
	}
	return nil
}

// ActiveConnections returns the total number of in-process SSE
// subscribers across all users (P2-1 audit / F6 observability).
// Used as the source callback for the `sse_active_connections` gauge.
func (b *sseBroker) ActiveConnections() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	n := 0
	for _, chans := range b.subs {
		n += len(chans)
	}
	return n
}
