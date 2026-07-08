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

// --- P1-5 per-user subscription cap regression tests ---

// TestSSESubscriberCapAllowsUnderLimit guards that the existing multi-tab
// use case (2-10 chrome tabs/devices per user) still works.
func TestSSESubscriberCapAllowsUnderLimit(t *testing.T) {
	broker := NewSSEBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 0; i < maxSSESubsPerUser; i++ {
		_, err := broker.Subscribe(ctx, "userCapOK")
		require.NoError(t, err, "subscription %d must succeed under cap", i+1)
	}
}

// TestSSESubscriberCapRejectsOverLimit regression-tests P1-5 (audit
// F6): the (maxSSESubsPerUser + 1)-th Subscribe call for the same user
// returns ErrSSETooManySubscribers and does NOT increase the count. This
// is the misbehaving-client protection.
func TestSSESubscriberCapRejectsOverLimit(t *testing.T) {
	broker := NewSSEBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 0; i < maxSSESubsPerUser; i++ {
		_, err := broker.Subscribe(ctx, "userCap")
		require.NoError(t, err)
	}

	ch, err := broker.Subscribe(ctx, "userCap")
	require.Error(t, err, "the %d-th subscriber for one user must be rejected", maxSSESubsPerUser+1)
	assert.Nil(t, ch, "rejected Subscribe must not return a channel")
	assert.ErrorIs(t, err, ErrSSETooManySubscribers)

	// Cap is enforced under b.mu — verify the count is still exactly
	// maxSSESubsPerUser and not higher.
	broker.(*sseBroker).mu.RLock()
	count := len(broker.(*sseBroker).subs["userCap"])
	broker.(*sseBroker).mu.RUnlock()
	assert.Equal(t, maxSSESubsPerUser, count,
		"rejected Subscribe must not bump the in-process count")
}

// TestSSESubscriberCapIsolates confirms the cap is per-userID, not global.
// Hitting the cap for userA must not affect userB.
func TestSSESubscriberCapIsolates(t *testing.T) {
	broker := NewSSEBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 0; i < maxSSESubsPerUser; i++ {
		_, err := broker.Subscribe(ctx, "userA")
		require.NoError(t, err)
	}
	_, err := broker.Subscribe(ctx, "userA")
	require.Error(t, err, "userA is at cap")

	_, err = broker.Subscribe(ctx, "userB")
	require.NoError(t, err, "userB must not be affected by userA's cap")
}

// TestSSESubscriberCapReleasesOnCancel confirms cancelled subscriptions
// free a slot, so a tab can be re-opened after the cap is reached.
// Without this, hitting the cap permanently would lock the user out.
//
// Loop fills (maxSSESubsPerUser - 1) subs under ctx so the cap-fulfilling
// last subscribe succeeds; then we trip the cap on the next attempt,
// cancel ctx, sleep to let cleanup goroutines drain, and confirm a fresh
// subscribe succeeds.
func TestSSESubscriberCapReleasesOnCancel(t *testing.T) {
	broker := NewSSEBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Fill (cap - 1) = 9 slots under ctx.
	for i := 0; i < maxSSESubsPerUser-1; i++ {
		_ = mustSubscribe(t, broker, ctx, "userReuse")
	}
	// Last subscribe succeeds and brings the count to exactly maxSSESubsPerUser.
	lastCh := mustSubscribe(t, broker, ctx, "userReuse")
	_ = lastCh

	// New subscribe at this point must fail (cap reached).
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	_, err := broker.Subscribe(ctx2, "userReuse")
	require.Error(t, err, "subscribe at cap must fail")

	// Cancel ALL the original context subscriptions so the cleanup
	// goroutines decrement the count.
	cancel()
	time.Sleep(100 * time.Millisecond)
	ctx2, cancel2 = context.WithCancel(context.Background())
	defer cancel2()
	_, err = broker.Subscribe(ctx2, "userReuse")
	require.NoError(t, err, "cancelled subscriptions must free slots")
}

func mustSubscribe(t *testing.T, b ports.Broker, ctx context.Context, userID string) <-chan ports.Event {
	t.Helper()
	ch, err := b.Subscribe(ctx, userID)
	require.NoError(t, err)
	return ch
}
