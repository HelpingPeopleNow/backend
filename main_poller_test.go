package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/adapters/handler"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// --- fakes for startHealthPoller ---

type pollerFakeLLM struct {
	err error
}

func (f *pollerFakeLLM) Health(_ context.Context) error { return f.err }

// pollerFakeDriver: a sql driver whose PingContext fails with a closed-pool
// error, so the postgres branch of the poller reports "down".
type pollerFakeDriver struct{}

func (d pollerFakeDriver) Open(name string) (driver.Conn, error) { return pollerFakeConn{}, nil }

type pollerFakeConn struct{}

func (c pollerFakeConn) Prepare(query string) (driver.Stmt, error) { return nil, nil }
func (c pollerFakeConn) Close() error                              { return nil }
func (c pollerFakeConn) Begin() (driver.Tx, error)                 { return nil, nil }

func init() {
	sql.Register("poller_fake", pollerFakeDriver{})
}

func pollerDB(t *testing.T, closed bool) *gorm.DB {
	t.Helper()
	sqlDB, err := sql.Open("poller_fake", "poller_fake:test")
	require.NoError(t, err)
	if closed {
		require.NoError(t, sqlDB.Close())
	}
	return &gorm.DB{Config: &gorm.Config{ConnPool: sqlDB}}
}

func TestStartHealthPollerHappyPath(t *testing.T) {
	handler.SetHealthStatus("postgres", false)
	handler.SetHealthStatus("grpc_helper", false)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	// Use a short-lived context: the poller returns on ctx.Done() and the
	// initial probe() runs synchronously before the ticker, so by the time
	// we cancel the gauges are already set.
	wg.Add(1)
	go func() {
		defer wg.Done()
		startHealthPoller(ctx, &wg, pollerDB(t, false), &pollerFakeLLM{err: nil})
	}()

	// Give the synchronous initial probe a moment to run.
	require.Eventually(t, func() bool {
		return handler.GetHealthStatus("postgres") == 1 && handler.GetHealthStatus("grpc_helper") == 1
	}, 2*time.Second, 10*time.Millisecond)

	cancel()
	wg.Wait()
}

func TestStartHealthPollerDegraded(t *testing.T) {
	handler.SetHealthStatus("postgres", true)
	handler.SetHealthStatus("grpc_helper", true)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// closed DB → postgres down; llm err → grpc_helper down.
		startHealthPoller(ctx, &wg, pollerDB(t, true), &pollerFakeLLM{err: errors.New("boom")})
	}()

	require.Eventually(t, func() bool {
		return handler.GetHealthStatus("postgres") == 0 && handler.GetHealthStatus("grpc_helper") == 0
	}, 2*time.Second, 10*time.Millisecond)

	cancel()
	wg.Wait()
}
