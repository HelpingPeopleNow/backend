package realtime

import (
	"context"
	"log/slog"
	"sync"

	"github.com/HelpingPeopleNow/backend/internal/ports"
)

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

func (b *sseBroker) Subscribe(ctx context.Context, userID string) (<-chan ports.Event, error) {
	ch := make(chan ports.Event, 32) // buffered to avoid blocking on slow consumers

	b.mu.Lock()
	b.subs[userID] = append(b.subs[userID], ch)
	b.mu.Unlock()

	// Cleanup when context is done
	go func() {
		<-ctx.Done()
		b.mu.Lock()
		subs := b.subs[userID]
		for i, c := range subs {
			if c == ch {
				b.subs[userID] = append(subs[:i], subs[i+1:]...)
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
