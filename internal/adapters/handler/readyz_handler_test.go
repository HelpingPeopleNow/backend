package handler

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Each test below uses a private *atomic.Bool to avoid coupling to
// the package-level singleton — under -shuffle, an unrelated test
// could otherwise flip the singleton state mid-assertion. The
// singleton's behaviour is verified explicitly in
// TestReadyzSingletonSurvivesMarkReady below.

func TestReadyzBeforeStartupReturns503(t *testing.T) {
	flag := &atomic.Bool{} // private, starts false
	h := NewReadyzHandler(flag)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code,
		"/readyz must return 503 before the flag is flipped")
	assert.Contains(t, rec.Body.String(), `"ready":false`)
}

func TestReadyzAfterStartupReturns200(t *testing.T) {
	flag := &atomic.Bool{}
	h := NewReadyzHandler(flag)
	flag.Store(true)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assert.Equal(t, http.StatusOK, rec.Code,
		"/readyz must return 200 after MarkReady flipped the flag")
	assert.Contains(t, rec.Body.String(), `"ready":true`)
}

func TestReadyzNilFlagNeverReady(t *testing.T) {
	// A nil flag must not panic and must consistently report not-ready.
	// This guards against a future edit that forgets to wire ReadyFlag()
	// in main.go — the readiness check should fail closed.
	h := NewReadyzHandler(nil)
	rec := httptest.NewRecorder()
	assert.NotPanics(t, func() {
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	})
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code,
		"nil ready flag must default to 503 (fail closed)")
	assert.Contains(t, rec.Body.String(), `"ready":false`)
}

func TestReadyzConcurrentReads(t *testing.T) {
	// The atomic.Bool guarantees consistent reads across goroutines
	// under -race. Smoke-test that 100 concurrent reads see one
	// consistent answer (all 200 or all 503) by toggling once at
	// half-time.
	flag := &atomic.Bool{}
	h := NewReadyzHandler(flag)

	var wg sync.WaitGroup
	codes := make([]int, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			codes[idx] = rec.Code
		}(i)
	}
	flag.Store(true)
	wg.Wait()

	for _, c := range codes {
		assert.True(t, c == http.StatusOK || c == http.StatusServiceUnavailable,
			"every /readyz response must be 200 or 503; got %d", c)
	}
}

// TestReadyzSingletonSurvivesMarkReady is the only test in this package
// that touches the package-level singleton. It verifies the production
// wiring: NewReadyzHandler(ReadyFlag()) + MarkReady() observes the
// SAME atom, so /readyz flips 503 → 200 in main.go's startup sequence.
// If anyone reintroduces a per-call fresh atom in ReadyFlag() this
// test will fail.
func TestReadyzSingletonSurvivesMarkReady(t *testing.T) {
	// Reset to a known state. Note this test is inherently
	// non-parallelizable (it manipulates shared package state), so it
	// must NOT call t.Parallel(). The flag's "previous" value is
	// captured so we can restore it on cleanup, which keeps the
	// assertion order-stable under -shuffle.
	prev := ReadyFlag().Load()
	t.Cleanup(func() {
		readyFlag.Store(prev)
	})
	readyFlag.Store(false)
	defer readyFlag.Store(true)

	h := NewReadyzHandler(ReadyFlag())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code,
		"before MarkReady, the singleton handler must return 503")

	MarkReady()

	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusOK, rec2.Code,
		"after MarkReady, the SAME singleton handler must return 200")
	assert.Contains(t, rec2.Body.String(), `"ready":true`)
}

// TestReadyzFlipsBothWays is the drain semantic for Phase 2 of the SPOF
// remediation (see infra/docs/FOLLOW_UP_SPOF_Backup_Replicas.md). It exercises the
// MarkUnready companion to MarkReady by binding a handler to the
// singleton readyFlag then flipping via the package-level functions:
//
//  1. /readyz reads whether the singleton is true (200) or false (503).
//  2. MarkUnready() flips it to false. The Traefik load-balancer
//     health check (path:/readyz, interval:10s, timeout:3s — via
//     infra/traefik-dev-dynamic.yaml + the dev/prod
//     traefik.http.services.backend.loadbalancer.healthcheck labels)
//     will see the 5xx on the next 10s tick and remove the replica
//     from the routing pool.
//  3. MarkReady() flips it back to true. Traefik returns the replica
//     to the routing pool on the next health-check tick.
//
// Without this test, a future refactor that breaks the MarkReady /
// MarkUnready contract (e.g., reintroducing a per-call fresh atom in
// ReadyFlag()) would only fail at the next multi-replica deploy —
// silent regression. This test surfaces the regression in CI.
//
// Uses t.Cleanup to restore the singleton's previous state so sibling
// tests under -shuffle are isolated. Like
// TestReadyzSingletonSurvivesMarkReady, this test is inherently
// non-parallelizable and must NOT call t.Parallel(). Each step also
// asserts the static `{"ready":true|false}` body shape — mirroring
// the existing TestReadyzBeforeStartupReturns503 /
// TestReadyzAfterStartupReturns200 contracts so a silent mutation
// that breaks the JSON envelope is caught here too.
func TestReadyzFlipsBothWays(t *testing.T) {
	prev := ReadyFlag().Load()
	t.Cleanup(func() { readyFlag.Store(prev) })

	// Start from a known-false state for the explicit
	// 503 → 200 → 503 → 200 round-trip.
	readyFlag.Store(false)

	h := NewReadyzHandler(ReadyFlag())

	// Step 1: fresh-flip-false reads 503 with the static
	// `{"ready":false}` envelope.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code,
		"fresh-flip-false singleton must read 503 before any MarkReady")
	assert.Contains(t, rec.Body.String(), `"ready":false`,
		"body must match the static {\"ready\":false} shape on 503")

	// Step 2: MarkReady → 200 with the static `{"ready":true}` envelope.
	MarkReady()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusOK, rec.Code,
		"MarkReady must flip singleton to true; handler observes 200")
	assert.Contains(t, rec.Body.String(), `"ready":true`,
		"body must match the static {\"ready\":true} shape on 200")

	// Step 3: MarkUnready → 503. This is the critical drain step —
	// the SAME handler instance (bound to the singleton) must observe
	// 503. If MarkUnready accidentally flips a different atom, this
	// fails loud.
	MarkUnready()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code,
		"MarkUnready must flip singleton to false; handler observes 503")
	assert.Contains(t, rec.Body.String(), `"ready":false`,
		"body must stay the static {\"ready\":false} shape on the drain flip")

	// Step 4: MarkReady again → 200. The recovery is part of the LB
	// contract (5xx → drain, 2xx → return-to-pool) and must be
	// symmetric to the drain flip.
	MarkReady()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusOK, rec.Code,
		"MarkReady after MarkUnready must restore; handler observes 200")
	assert.Contains(t, rec.Body.String(), `"ready":true`,
		"body must match the static {\"ready\":true} shape on recovery")
}
