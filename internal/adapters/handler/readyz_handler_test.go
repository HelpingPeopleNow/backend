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
