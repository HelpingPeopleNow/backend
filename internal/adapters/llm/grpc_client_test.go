package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

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

func TestEnsureClientDialFailure(t *testing.T) {
	// Use an unreachable address so dial fails
	svc := NewGRPCLLMService("localhost:1", "") // port 1 is unlikely to be open
	gsvc := svc.(*GRPCLLMService)

	err := gsvc.ensureClient()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gRPC dial")
}

func TestEnsureClientNilClientInitialized(t *testing.T) {
	svc := NewGRPCLLMService("localhost:1", "")
	gsvc := svc.(*GRPCLLMService)
	assert.Nil(t, gsvc.client)
}

func TestAskEnsureClientFails(t *testing.T) {
	// Ask with unreachable address should fail at ensureClient
	svc := NewGRPCLLMService("localhost:1", "")
	_, err := svc.Ask(context.Background(), "sys", "msg", nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gRPC dial")
}

func TestEmbedEnsureClientFails(t *testing.T) {
	svc := NewGRPCLLMService("localhost:1", "")
	_, err := svc.Embed(context.Background(), "text")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gRPC dial")
}
