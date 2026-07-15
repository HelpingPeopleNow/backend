package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/adapters/handler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShutdownSequenceDrainPhase is the SPOF Phase 3 regression test
// (see infra/docs/FOLLOW_UP_SPOF_Backup_Replicas.md). It asserts three invariants:
//
//  1. cancelRoot() is called BEFORE startShutdown (parallelises
//     sweeper drain with HTTP shutdown — total time shorter than
//     the serial alternative).
//  2. handler.MarkUnready() is called BEFORE startShutdown
//     (Traefik LB health-check sees the 503 drain before the
//     listener closes — without this, in-flight requests get
//     connection-reset).
//  3. drainWait elapses between MarkUnready and startShutdown
//     (the LB needs at least one full health-check tick — 10s
//     interval + 3s timeout — to register the 503).
//
// Without the SPOF Phase 3 wiring, the order would be `Shutdown →
// cancelRoot` (the legacy code), which tears down accept listeners
// in-flight before Traefik has a chance to drain — observable to
// external clients as 502/504 spikes during every rolling deploy.
func TestShutdownSequenceDrainPhase(t *testing.T) {
	// Save+restore the singleton; non-parallelizable like the other
	// readyz tests.
	prev := handler.ReadyFlag().Load()
	t.Cleanup(func() { handler.ReadyFlag().Store(prev) })
	handler.MarkReady() // start from 200

	const drainWait = 50 * time.Millisecond
	seqStart := time.Now()

	var (
		startShutdownAt atomic.Pointer[time.Time]
		cancelRootAt    atomic.Pointer[time.Time]
	)
	startShutdown := func(ctx context.Context) error {
		now := time.Now()
		startShutdownAt.Store(&now)

		// Invariant 2: at this point /readyz MUST be 503. If the
		// singleton is still true, MarkUnready didn't fire before
		// Shutdown — the Phase 3 contract is broken.
		assert.False(t, handler.ReadyFlag().Load(),
			"INVARIANT VIOLATED: MarkUnready must flip /readyz to 503 BEFORE startShutdown fires")

		// Invariant 3: drainWait must have elapsed (with a tiny
		// ceil to absorb scheduler jitter).
		assert.GreaterOrEqual(t, time.Since(seqStart), drainWait-time.Millisecond,
			"INVARIANT VIOLATED: drainWait must elapse between MarkUnready and startShutdown "+
				"(covers the Traefik LB health-check tick)")

		// Invariant 1 mirror: at this point cancelRoot MUST have fired.
		assert.NotNil(t, cancelRootAt.Load(),
			"INVARIANT VIOLATED: cancelRoot must run BEFORE startShutdown to parallelise drains")
		return nil
	}
	cancelRoot := func() {
		now := time.Now()
		cancelRootAt.Store(&now)
		// cancelRoot fires before startShutdown (we are inside
		// runShutdownSequence step 1).
		if startShutdownAt.Load() != nil {
			t.Fatalf("INVARIANT VIOLATED: cancelRoot must be called BEFORE startShutdown")
		}
	}

	runShutdownSequence(context.Background(), startShutdown, cancelRoot, drainWait)

	require.NotNil(t, startShutdownAt.Load(), "startShutdown must have been called")
	require.NotNil(t, cancelRootAt.Load(), "cancelRoot must have been called")

	// Sanity: cancelRoot ran first. Compare the timestamps.
	cancelT := *cancelRootAt.Load()
	shutdownT := *startShutdownAt.Load()
	assert.True(t, cancelT.Before(shutdownT) || cancelT.Equal(shutdownT),
		"cancelRoot timestamp %v must be <= startShutdown timestamp %v", cancelT, shutdownT)

	// Clean up: flip back to ready so non-`t.Parallel()` sibling
	// tests under -shuffle aren't disturbed.
	handler.MarkReady()
}

// TestShutdownSequenceNilCancelRootOk guards against a future edit
// that passes a nil cancelRoot. The function should still drain /readyz
// + sleep + startShutdown without panicking.
func TestShutdownSequenceNilCancelRootOk(t *testing.T) {
	prev := handler.ReadyFlag().Load()
	t.Cleanup(func() { handler.ReadyFlag().Store(prev) })
	handler.MarkReady()

	called := false
	runShutdownSequence(
		context.Background(),
		func(ctx context.Context) error { called = true; return nil },
		nil, // cancelRoot intentionally nil
		10*time.Millisecond,
	)
	assert.True(t, called, "startShutdown must run even when cancelRoot is nil")
	assert.False(t, handler.ReadyFlag().Load(),
		"MarkUnready must still flip /readyz to 503 even when cancelRoot is nil")

	handler.MarkReady() // cleanup
}

// TestShutdownDrainDurDefault verifies the 14s default matches the
// Phase 2 Traefik LB health-check worst-case ceiling (10s interval +
// 3s timeout + 1s slack).
func TestShutdownDrainDurDefault(t *testing.T) {
	t.Setenv("SHUTDOWN_DRAIN_WAIT", "") // unset -> default
	d := shutdownDrainDur()
	assert.Equal(t, 14*time.Second, d,
		"default drain wait must be 14s (10s interval + 3s timeout + 1s slack)")
}

// TestShutdownDrainDurOverride verifies the SHUTDOWN_DRAIN_WAIT env
// override works.
func TestShutdownDrainDurOverride(t *testing.T) {
	t.Setenv("SHUTDOWN_DRAIN_WAIT", "30s")
	assert.Equal(t, 30*time.Second, shutdownDrainDur())

	t.Setenv("SHUTDOWN_DRAIN_WAIT", "200ms")
	assert.Equal(t, 200*time.Millisecond, shutdownDrainDur())
}

// TestShutdownDrainDurInvalidFallsBack ensures unparseable / negative
// values fall back to 14s (fail-closed, never block forever).
func TestShutdownDrainDurInvalidFallsBack(t *testing.T) {
	t.Setenv("SHUTDOWN_DRAIN_WAIT", "not-a-duration")
	assert.Equal(t, 14*time.Second, shutdownDrainDur(),
		"unparseable SHUTDOWN_DRAIN_WAIT must fall back to default (fail-closed)")

	t.Setenv("SHUTDOWN_DRAIN_WAIT", "-5s")
	assert.Equal(t, 14*time.Second, shutdownDrainDur(),
		"negative SHUTDOWN_DRAIN_WAIT must fall back to default (fail-closed)")
}
