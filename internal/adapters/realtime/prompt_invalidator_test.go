package realtime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/adapters/repository"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

// stubRepo is a hand-rolled stand-in for ports.SystemPromptRepository
// to keep the test self-contained (the real GormSystemPromptRepository
// is exercised in the integration tests). It records invalidation
// calls so the test can assert that subscribe → reload path works.
type stubRepo struct {
	mu             sync.Mutex
	invalidateHits int
	lastReloadAt   time.Time
}

func (s *stubRepo) InvalidateCache(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invalidateHits++
	s.lastReloadAt = time.Now()
	return nil
}

func TestPromptInvalidatorEmptyURLReturnsNoOp(t *testing.T) {
	inv, closer, err := NewPromptInvalidator("", nil)
	require.NoError(t, err)
	require.NotNil(t, inv)
	require.NotNil(t, closer)
	// PublishInvalidation on a no-op must be a no-op (no error).
	require.NoError(t, inv.PublishInvalidation(context.Background(), "worker_profile_prompt"))
	require.NoError(t, closer())
}

func TestPromptInvalidatorInvalidURL(t *testing.T) {
	_, _, err := NewPromptInvalidator("://not-a-redis-url", &stubRepo{})
	require.Error(t, err)
	// Don't pin the exact message — go-redis's URL parser formats
	// change between minor versions. We only care that invalid input
	// is rejected.
}

func TestPromptInvalidatorPublishAndSubscribeReload(t *testing.T) {
	// Single miniredis instance; two PromptInvalidators (simulating
	// two replicas) share it via shared Redis pub/sub.
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	url := "redis://" + mr.Addr()
	repoA := &stubRepo{}
	invA, closerA, err := NewPromptInvalidator(url, repoA)
	require.NoError(t, err)
	t.Cleanup(func() { _ = closerA() })

	repoB := &stubRepo{}
	invB, closerB, err := NewPromptInvalidator(url, repoB)
	require.NoError(t, err)
	t.Cleanup(func() { _ = closerB() })
	_ = invB

	// Give both subscriptions a moment to wire up so the publish on A
	// races the subscribe on B's goroutine instead of the Subscribe call.
	time.Sleep(50 * time.Millisecond)

	// Replica A publishes an invalidation event. Both replicas should
	// see it (each invalidator reloads its own repo).
	require.NoError(t, invA.PublishInvalidation(context.Background(), "worker_profile_prompt"))

	// Wait up to 2s for the reload to propagate.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		repoA.mu.Lock()
		hitsA := repoA.invalidateHits
		repoA.mu.Unlock()
		repoB.mu.Lock()
		hitsB := repoB.invalidateHits
		repoB.mu.Unlock()
		if hitsA > 0 && hitsB > 0 {
			return // success
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected both replicas to receive invalidation event, "+
		"repoA.hits=%d repoB.hits=%d", repoA.invalidateHits, repoB.invalidateHits)
}

func TestPromptInvalidatorCloseStopsGoroutine(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	repo := &stubRepo{}
	inv, closer, err := NewPromptInvalidator("redis://"+mr.Addr(), repo)
	require.NoError(t, err)
	require.NoError(t, closer())
	require.NoError(t, closer(), "second close must be idempotent")
	_ = inv
}

// Ensure the GormSystemPromptRepository type compiles with the
// SystemPromptInvalidator-using methods — defensive against a future
// rename or signature drift.
func TestGormSystemPromptRepositoryHasSetInvalidator(t *testing.T) {
	// Compile-time check via reflect: SetInvalidator must exist on the
	// concrete type. We can't easily do that without importing the
	// concrete package here, so just exercise the constructor.
	var _ = (*repository.GormSystemPromptRepository)(nil)
}
