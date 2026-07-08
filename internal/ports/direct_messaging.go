package ports

import "context"

// Broker is an in-process pub/sub bus for SSE events.
// A subscriber receives all events published to their userID.
type Broker interface {
	// Subscribe returns a channel of events for the given user.
	// The channel is closed when ctx is done.
	Subscribe(ctx context.Context, userID string) (<-chan Event, error)

	// Publish broadcasts an event to all subscribers of the given userID.
	Publish(userID string, event Event) error

	// ActiveConnections returns the total number of in-process
	// subscribers across all users. Surfaced as the
	// `sse_active_connections` gauge (P2-1 audit / F6).
	ActiveConnections() int
}

// Event is a real-time event delivered via SSE.
type Event struct {
	Type    string // "message" | "read" | "archive" | "block" | "report"
	Payload any
}
