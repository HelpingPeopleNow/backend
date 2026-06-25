package realtime

import (
	"context"
	"testing"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishDelivers(t *testing.T) {
	broker := NewSSEBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := broker.Subscribe(ctx, "user1")
	require.NoError(t, err)

	event := ports.Event{Type: "message", Payload: "hello"}
	require.NoError(t, broker.Publish("user1", event))

	select {
	case got := <-ch:
		assert.Equal(t, "message", got.Type)
		assert.Equal(t, "hello", got.Payload)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	broker := NewSSEBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch1, _ := broker.Subscribe(ctx, "user1")
	ch2, _ := broker.Subscribe(ctx, "user1")

	event := ports.Event{Type: "read", Payload: nil}
	broker.Publish("user1", event)

	for _, ch := range []<-chan ports.Event{ch1, ch2} {
		select {
		case got := <-ch:
			assert.Equal(t, "read", got.Type)
		case <-time.After(time.Second):
			t.Fatal("subscriber didn't receive event")
		}
	}
}

func TestContextCancelCleanup(t *testing.T) {
	broker := NewSSEBroker()
	ctx, cancel := context.WithCancel(context.Background())

	_, err := broker.Subscribe(ctx, "user1")
	require.NoError(t, err)

	// Verify subscriber exists
	broker.(*sseBroker).mu.RLock()
	assert.Len(t, broker.(*sseBroker).subs["user1"], 1)
	broker.(*sseBroker).mu.RUnlock()

	cancel()
	// Give cleanup goroutine time to run
	time.Sleep(50 * time.Millisecond)

	broker.(*sseBroker).mu.RLock()
	_, exists := broker.(*sseBroker).subs["user1"]
	broker.(*sseBroker).mu.RUnlock()
	assert.False(t, exists, "subscriber should be cleaned up after ctx cancel")
}

func TestDropOnFull(t *testing.T) {
	broker := NewSSEBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, _ := broker.Subscribe(ctx, "user1")

	// Fill the buffer (32 capacity)
	for i := 0; i < 32; i++ {
		broker.Publish("user1", ports.Event{Type: "message"})
	}

	// This one should be dropped (non-blocking)
	broker.Publish("user1", ports.Event{Type: "message"})

	// Drain first 32
	for i := 0; i < 32; i++ {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatal("timed out draining")
		}
	}

	// Channel should be empty (33rd was dropped)
	select {
	case <-ch:
		t.Fatal("should not have received 33rd event")
	default:
		// expected
	}
}

func TestPublishNoSubscribers(t *testing.T) {
	broker := NewSSEBroker()
	// Should not panic
	err := broker.Publish("nobody", ports.Event{Type: "message"})
	assert.NoError(t, err)
}

func TestSubscribeSameUserTwoContexts(t *testing.T) {
	broker := NewSSEBroker()
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	ch1, _ := broker.Subscribe(ctx1, "user1")
	ch2, _ := broker.Subscribe(ctx2, "user1")

	event := ports.Event{Type: "typing"}
	broker.Publish("user1", event)

	for _, ch := range []<-chan ports.Event{ch1, ch2} {
		select {
		case got := <-ch:
			assert.Equal(t, "typing", got.Type)
		case <-time.After(time.Second):
			t.Fatal("subscriber didn't receive event")
		}
	}

	// Cancel one, other should still work
	cancel1()
	time.Sleep(50 * time.Millisecond)

	event2 := ports.Event{Type: "read"}
	broker.Publish("user1", event2)

	select {
	case got := <-ch2:
		assert.Equal(t, "read", got.Type)
	case <-time.After(time.Second):
		t.Fatal("second subscriber should still receive after first cancelled")
	}
}
