package handler

import (
	"net/http"
	"sync/atomic"
)

// readyFlag is the package-level readiness flag shared by NewReadyzHandler
// (the consumer; bound to /readyz) and MarkReady (the producer; called
// from main.go after buildMux returns). Package-scoped so handler and
// producer observe the same atomic — a per-call fresh allocation (the
// original implementation) defeated Phase 1 of the SPOF remediation:
// the handler's /readyz endpoint would perpetually return 503 even
// after MarkReady fired on a different atom.
var readyFlag = &atomic.Bool{}

// ReadyFlag returns the package-level readiness flag. main.go
// passes this to NewReadyzHandler (in buildMux) and MarkReady (after
// buildMux returns). Tests should not bind to this flag
// directly — use a private *atomic.Bool so a sibling test flipping
// the singleton can't taint your assertions under -shuffle.
func ReadyFlag() *atomic.Bool {
	return readyFlag
}

// ReadyzHandler implements the k8s readiness-probe idiom: 503 while the
// process is still wiring up (before /metrics sources are registered,
// before the SSE broker is up, etc.) and 200 once the orchestration
// is complete.
//
// This is the SPOF follow-up ticket's gating endpoint (see
// `infra/docs/FOLLOW_UP_SPOF.md`): a multi-replica Traefik
// deployment can drain a replica by holding the response at 503
// across a deploy, instead of killing existing in-flight requests.
type ReadyzHandler struct {
	ready *atomic.Bool
}

// NewReadyzHandler constructs a /readyz endpoint bound to the
// supplied readiness flag (typically `ReadyFlag()` so main.go's
// MarkReady caller sees the change).
func NewReadyzHandler(ready *atomic.Bool) *ReadyzHandler {
	return &ReadyzHandler{ready: ready}
}

// ServeHTTP responds 200 once ready=true has been observed, else 503.
// Body is a tiny static JSON shape: `{ "ready": true|false }` — a
// load-balancer health check is the canonical consumer, and the
// response is also helpful for human `curl` diagnostics during a
// rolling deploy.
func (h *ReadyzHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if h.ready != nil && h.ready.Load() {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ready":true}`))
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"ready":false}`))
}

// MarkReady flips the package-level readyFlag to true. main.go calls
// it once after buildMux returns. There is no param — the dual
// semantics (passed atom vs nil) introduced before this fix confused
// the contract and let a `MarkReady(localAtom)` caller forget to flip
// the singleton, which is what /readyz observes.
func MarkReady() {
	readyFlag.Store(true)
}
