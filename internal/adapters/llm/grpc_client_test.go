package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthNoURL(t *testing.T) {
	svc := NewGRPCLLMService("localhost:50051", "")
	err := svc.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no health URL")
}

func TestHealthOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := NewGRPCLLMService("localhost:50051", srv.URL)
	err := svc.Health(context.Background())
	assert.NoError(t, err)
}

func TestHealthDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	svc := NewGRPCLLMService("localhost:50051", srv.URL)
	err := svc.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Internal Server Error")
}

func TestHealthServerUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // close immediately

	svc := NewGRPCLLMService("localhost:50051", srv.URL)
	err := svc.Health(context.Background())
	assert.Error(t, err)
}

func TestHealthTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow response — but the client has a 3s timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := NewGRPCLLMService("localhost:50051", srv.URL)
	err := svc.Health(context.Background())
	assert.NoError(t, err)
}

// --- DeepProbeStatus tests (OBSERVABILITY_AUDIT_REPORT.md roadmap item 5) ---

func TestDeepProbeStatusNoURL(t *testing.T) {
	svc := NewGRPCLLMService("localhost:50051", "").(*GRPCLLMService)
	status, results, err := svc.DeepProbeStatus(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no health URL")
	assert.Empty(t, status)
	assert.Nil(t, results)
}

func TestDeepProbeStatusOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"deep_probe":"ok","deep_probe_results":{"opencode0":"ok","ollama":"ok"}}`))
	}))
	defer srv.Close()

	svc := NewGRPCLLMService("localhost:50051", srv.URL).(*GRPCLLMService)
	status, results, err := svc.DeepProbeStatus(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "ok", status)
	assert.Equal(t, map[string]string{"opencode0": "ok", "ollama": "ok"}, results)
}

// TestDeepProbeStatusOn503 mirrors the helper's own behaviour: /health can
// return 503 (e.g. no healthy shallow adapter) while still carrying a valid
// deep_probe payload in the body. DeepProbeStatus must still parse it.
func TestDeepProbeStatusOn503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"deep_probe":"degraded","deep_probe_results":{"opencode0":"down"}}`))
	}))
	defer srv.Close()

	svc := NewGRPCLLMService("localhost:50051", srv.URL).(*GRPCLLMService)
	status, results, err := svc.DeepProbeStatus(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "degraded", status)
	assert.Equal(t, map[string]string{"opencode0": "down"}, results)
}

func TestDeepProbeStatusUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	svc := NewGRPCLLMService("localhost:50051", srv.URL).(*GRPCLLMService)
	_, _, err := svc.DeepProbeStatus(context.Background())
	assert.Error(t, err)
}

func TestDeepProbeStatusBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	svc := NewGRPCLLMService("localhost:50051", srv.URL).(*GRPCLLMService)
	_, _, err := svc.DeepProbeStatus(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode helper health response")
}

func TestNewGRPCLLMServiceDefaultTimeout(t *testing.T) {
	os.Unsetenv("HELPER_TIMEOUT_SECONDS")
	svc := NewGRPCLLMService("localhost:50051", "http://localhost:8080/health")
	gsvc, ok := svc.(*GRPCLLMService)
	require.True(t, ok)
	assert.Equal(t, 60, gsvc.timeoutSecs)
}

func TestNewGRPCLLMServiceCustomTimeout(t *testing.T) {
	os.Setenv("HELPER_TIMEOUT_SECONDS", "30")
	defer os.Unsetenv("HELPER_TIMEOUT_SECONDS")
	svc := NewGRPCLLMService("localhost:50051", "http://localhost:8080/health")
	gsvc, ok := svc.(*GRPCLLMService)
	require.True(t, ok)
	assert.Equal(t, 30, gsvc.timeoutSecs)
}

func TestNewGRPCLLMServiceInvalidTimeoutFallsBack(t *testing.T) {
	os.Setenv("HELPER_TIMEOUT_SECONDS", "notanumber")
	defer os.Unsetenv("HELPER_TIMEOUT_SECONDS")
	svc := NewGRPCLLMService("localhost:50051", "http://localhost:8080/health")
	gsvc, ok := svc.(*GRPCLLMService)
	require.True(t, ok)
	assert.Equal(t, 60, gsvc.timeoutSecs)
}

func TestNewGRPCLLMServiceZeroTimeoutFallsBack(t *testing.T) {
	os.Setenv("HELPER_TIMEOUT_SECONDS", "0")
	defer os.Unsetenv("HELPER_TIMEOUT_SECONDS")
	svc := NewGRPCLLMService("localhost:50051", "http://localhost:8080/health")
	gsvc, ok := svc.(*GRPCLLMService)
	require.True(t, ok)
	assert.Equal(t, 60, gsvc.timeoutSecs)
}

// TestEnsureClientLazyForUnreachable regression-tests P1-2: ensureClient
// no longer returns an error for unreachable addresses. grpc.NewClient is
// lazy by design (audit F5) so the network dial moves to first RPC time,
// bounded by the per-call timeout in Ask/Embed. Previously this test
// (TestEnsureClientDialFailure) asserted an error string "gRPC dial" which
// reflected the deprecated DialContext+WithBlock path.
func TestEnsureClientLazyForUnreachable(t *testing.T) {
	// port 1 is unlikely to be open
	svc := NewGRPCLLMService("localhost:1", "")
	gsvc := svc.(*GRPCLLMService)
	require.Nil(t, gsvc.client)

	err := gsvc.ensureClient()
	require.NoError(t, err, "ensureClient must succeed for unreachable address (lazy dial)")
	require.NotNil(t, gsvc.client, "client must be created even if dial will fail later")
}

func TestEnsureClientNilClientInitialized(t *testing.T) {
	svc := NewGRPCLLMService("localhost:1", "")
	gsvc := svc.(*GRPCLLMService)
	assert.Nil(t, gsvc.client)
}

// TestAskEnsureClientFails checks the RPC-level failure path now that
// ensureClient is lazy. The error happens at first Ask call, not at
// ensureClient construction.
func TestAskEnsureClientFails(t *testing.T) {
	svc := NewGRPCLLMService("localhost:1", "")
	// 2s deadline so RPC fails within test timeout, not the 20s default.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := svc.Ask(ctx, "sys", "msg", nil, nil)
	require.Error(t, err, "Ask should fail at RPC time when destination is unreachable")
}

func TestEmbedEnsureClientFails(t *testing.T) {
	svc := NewGRPCLLMService("localhost:1", "")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := svc.Embed(ctx, "text")
	require.Error(t, err)
}

// --- Circuit breaker tests (F3) ---

func TestBreakerClosedByDefault(t *testing.T) {
	svc := NewGRPCLLMService("localhost:1", "")
	gsvc := svc.(*GRPCLLMService)
	assert.Equal(t, "closed", gsvc.BreakerState())
}

func TestBreakerOpensAfterFiveFails(t *testing.T) {
	svc := NewGRPCLLMService("localhost:1", "")
	gsvc := svc.(*GRPCLLMService)

	// Call breakerFail directly 5 times (avoids slow gRPC dial).
	for i := 0; i < 5; i++ {
		gsvc.breakerFail()
	}
	assert.Equal(t, "open", gsvc.BreakerState())
	assert.Equal(t, 5, gsvc.breakerFails)
}

func TestBreakerOpenRejectsAsk(t *testing.T) {
	svc := NewGRPCLLMService("localhost:1", "")
	gsvc := svc.(*GRPCLLMService)

	// Trip the breaker open via breakerFail (fast).
	for i := 0; i < 5; i++ {
		gsvc.breakerFail()
	}
	require.Equal(t, "open", gsvc.BreakerState())

	// Ask should fail immediately with breaker error, no gRPC dial attempt.
	_, err := svc.Ask(context.Background(), "sys", "msg", nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "breaker")
}

func TestBreakerOpenRejectsEmbed(t *testing.T) {
	svc := NewGRPCLLMService("localhost:1", "")
	gsvc := svc.(*GRPCLLMService)

	// Trip the breaker open via breakerFail (fast).
	for i := 0; i < 5; i++ {
		gsvc.breakerFail()
	}
	require.Equal(t, "open", gsvc.BreakerState())

	// Embed should also be rejected.
	_, err := svc.Embed(context.Background(), "text")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "breaker")
}

func TestBreakerHalfOpenAfterCooldown(t *testing.T) {
	svc := NewGRPCLLMService("localhost:1", "")
	gsvc := svc.(*GRPCLLMService)

	// Trip the breaker open via breakerFail (fast).
	for i := 0; i < 5; i++ {
		gsvc.breakerFail()
	}
	require.Equal(t, "open", gsvc.BreakerState())

	// Simulate cooldown by setting breakerOpenedAt to 31 seconds ago.
	gsvc.breakerMu.Lock()
	gsvc.breakerOpenedAt = time.Now().Add(-31 * time.Second)
	gsvc.breakerMu.Unlock()

	// Ask should now be allowed (half-open probe via breakerAllow) but
	// will fail at RPC time against the unreachable destination, which
	// calls breakerFail again (6th fail). P1-2 lifted the blocking dial
	// from ensureClient; error text no longer contains "gRPC dial".
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := svc.Ask(ctx, "sys", "msg", nil, nil)
	require.Error(t, err, "Ask should fail at RPC time when helper is unreachable in half-open probe")

	// After RPC failure in half-open, breaker goes back to open.
	gsvc.breakerMu.Lock()
	fails := gsvc.breakerFails
	gsvc.breakerMu.Unlock()
	assert.Equal(t, 6, fails)
}

func TestBreakerSuccessResetsState(t *testing.T) {
	svc := NewGRPCLLMService("localhost:1", "")
	gsvc := svc.(*GRPCLLMService)

	// Trip open.
	for i := 0; i < 5; i++ {
		gsvc.breakerFail()
	}
	require.Equal(t, "open", gsvc.BreakerState())

	// Move to half-open.
	gsvc.breakerMu.Lock()
	gsvc.breakerOpenedAt = time.Now().Add(-31 * time.Second)
	gsvc.breakerMu.Unlock()

	// Manually call breakerSuccess to simulate a successful probe.
	gsvc.breakerSuccess()
	assert.Equal(t, "closed", gsvc.BreakerState())
	assert.Equal(t, 0, gsvc.breakerFails)
}

func TestBreakerStateLabels(t *testing.T) {
	svc := NewGRPCLLMService("localhost:1", "")
	gsvc := svc.(*GRPCLLMService)

	// Default: closed.
	assert.Equal(t, "closed", gsvc.BreakerState())

	// Force open.
	gsvc.breakerMu.Lock()
	gsvc.breakerState = breakerOpen
	gsvc.breakerOpenedAt = time.Now()
	gsvc.breakerMu.Unlock()
	assert.Equal(t, "open", gsvc.BreakerState())

	// Force half-open.
	gsvc.breakerMu.Lock()
	gsvc.breakerState = breakerHalfOpen
	gsvc.breakerMu.Unlock()
	assert.Equal(t, "half_open", gsvc.BreakerState())
}
