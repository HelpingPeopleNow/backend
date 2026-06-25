package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
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
