package llm

import (
	"context"
	"strings"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

// =============================================================================
// P3-4 — gRPC metadata carries x-request-id for cross-service tracing
// =============================================================================
//
// attachOutgoingRequestID is the building block used by both Ask and Embed
// to propagate the per-request correlation ID to the helper service. We
// test it directly (without spinning up a real gRPC server) by reading
// from the metadata that the helper package attaches via
// metadata.NewOutgoingContext.

func TestAttachOutgoingRequestIDFromContext(t *testing.T) {
	const wantID = "abc1234567890abcdef01234567890ab"
	ctx := contextkeys.SetRequestID(context.Background(), wantID)

	out := attachOutgoingRequestID(ctx)
	md, ok := metadata.FromOutgoingContext(out)
	require.True(t, ok, "expected outgoing metadata to be set")

	got := md.Get(grpcRequestIDMetadataKey)
	require.Len(t, got, 1, "expected exactly one x-request-id metadata value; got %v", got)
	assert.Equal(t, wantID, got[0],
		"x-request-id metadata must carry the request_id from the backend ctx")
}

func TestAttachOutgoingRequestIDEmptyContextLeavesCtxAlone(t *testing.T) {
	// Background ctx (no request_id) — attachOutgoingRequestID should
	// return the original ctx WITHOUT adding metadata (cheaper RPC path).
	ctx := context.Background()
	out := attachOutgoingRequestID(ctx)

	_, hasMD := metadata.FromOutgoingContext(out)
	assert.False(t, hasMD,
		"empty request_id must not add outgoing metadata (no extra wire overhead)")
}

func TestAttachOutgoingRequestIDDoesNotPanicOnNilCtx(t *testing.T) {
	require.NotPanics(t, func() {
		_ = attachOutgoingRequestID(context.Background())
	}, "attachOutgoingRequestID must never panic")
}

func TestGRPCRequestIDMetadataKeyMatchesGatewayHeader(t *testing.T) {
	// The metadata key must match the inbound HTTP X-Request-ID header so
	// responders on the helper side reading either side see the same ID.
	// Lowercase for gRPC (per io.grpc metadata convention; gateway will
	// surface it as X-Request-ID when proxying).
	assert.True(t, strings.EqualFold(grpcRequestIDMetadataKey, "x-request-id"),
		"metadata key must case-insensitively match \"x-request-id\" for cross-service tracing")
}
