package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/contextkeys"
	"github.com/HelpingPeopleNow/backend/internal/ports"
	pb "github.com/HelpingPeopleNow/backend/proto/helper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

// grpcRequestIDMetadataKey is the header used to propagate the per-request
// correlation ID across the gRPC boundary so the helper service can log its
// side under the same identifier (P3-4 audit cross-service tracing).
const grpcRequestIDMetadataKey = "x-request-id"

const expectedEmbeddingDim = 768

type GRPCLLMService struct {
	addr         string
	healthURL    string
	timeoutSecs  int
	llmTimeout   time.Duration // F3: Pass-1/Pass-2 budget
	embedTimeout time.Duration // F3: Embed budget
	mu           sync.Mutex
	conn         *grpc.ClientConn
	client       pb.HelperServiceClient
	// F3: circuit breaker
	breakerMu       sync.Mutex
	breakerFails    int
	breakerState    int // 0=closed, 1=open, 2=half-open
	breakerOpenedAt time.Time
}

func NewGRPCLLMService(addr, healthURL string) ports.LLMService {
	timeout := 60
	if ts := os.Getenv("HELPER_TIMEOUT_SECONDS"); ts != "" {
		if v, err := strconv.Atoi(ts); err == nil && v > 0 {
			timeout = v
		}
	}
	// F3: independent timeouts for LLM and Embed
	llmTimeout := 20 * time.Second
	if t := os.Getenv("HELPER_LLM_TIMEOUT"); t != "" {
		if v, err := strconv.Atoi(t); err == nil && v > 0 {
			llmTimeout = time.Duration(v) * time.Second
		}
	}
	embedTimeout := 8 * time.Second
	if t := os.Getenv("HELPER_EMBED_TIMEOUT"); t != "" {
		if v, err := strconv.Atoi(t); err == nil && v > 0 {
			embedTimeout = time.Duration(v) * time.Second
		}
	}
	return &GRPCLLMService{
		addr:         addr,
		healthURL:    healthURL,
		timeoutSecs:  timeout,
		llmTimeout:   llmTimeout,
		embedTimeout: embedTimeout,
	}
}

// F3: Circuit breaker helpers. States: 0=closed (normal), 1=open (failing fast), 2=half-open (testing).
const (
	breakerClosed   = 0
	breakerOpen     = 1
	breakerHalfOpen = 2

	breakerThreshold = 5 // consecutive failures to trip open
	breakerCooldown  = 30 * time.Second
)

// breakerRecord records success or failure; returns true if call is allowed.
func (s *GRPCLLMService) breakerAllow() bool {
	s.breakerMu.Lock()
	defer s.breakerMu.Unlock()
	switch s.breakerState {
	case breakerOpen:
		if time.Since(s.breakerOpenedAt) > breakerCooldown {
			s.breakerState = breakerHalfOpen
			return true
		}
		return false
	case breakerHalfOpen:
		return true // allow one probe
	default: // closed
		return true
	}
}

func (s *GRPCLLMService) breakerSuccess() {
	s.breakerMu.Lock()
	defer s.breakerMu.Unlock()
	s.breakerFails = 0
	s.breakerState = breakerClosed
}

func (s *GRPCLLMService) breakerFail() {
	s.breakerMu.Lock()
	defer s.breakerMu.Unlock()
	s.breakerFails++
	if s.breakerFails >= breakerThreshold {
		s.breakerState = breakerOpen
		s.breakerOpenedAt = time.Now()
		slog.Warn("helper circuit breaker OPEN", "fails", s.breakerFails)
	}
}

// BreakerState returns the current circuit breaker state as a string for metrics.
func (s *GRPCLLMService) BreakerState() string {
	s.breakerMu.Lock()
	defer s.breakerMu.Unlock()
	switch s.breakerState {
	case breakerOpen:
		return "open"
	case breakerHalfOpen:
		return "half_open"
	default:
		return "closed"
	}
}

// ensureClient creates a lazy, NON-blocking gRPC client (P1-2 audit, F5).
// The previous DialContext+WithBlock dial held s.mu for up to 5s while the
// network dial completed, so a degraded helper serialised every request
// behind this lock. grpc.NewClient returns immediately and connects on
// first RPC; the per-call context timeouts in Ask/Embed bound
// connection-establishment latency instead.
func (s *GRPCLLMService) ensureClient() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return nil
	}

	slog.Info("llm: creating helper gRPC client", "addr", s.addr)
	conn, err := grpc.NewClient(s.addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return fmt.Errorf("gRPC new client %s: %w", s.addr, err)
	}
	s.conn = conn
	s.client = pb.NewHelperServiceClient(conn)
	return nil
}

func (s *GRPCLLMService) Ask(
	ctx context.Context,
	systemPrompt string,
	userMessage string,
	history []ports.MessagePair,
	provider string,
) (*ports.LLMResponse, error) {
	slog.Info("llm: Ask", "msg_len", len(userMessage), "history_len", len(history), "provider", provider)
	// F3: circuit breaker — fail fast when helper is unhealthy
	if !s.breakerAllow() {
		return nil, fmt.Errorf("helper circuit breaker open — search degraded")
	}
	if err := s.ensureClient(); err != nil {
		s.breakerFail()
		return nil, err
	}

	protoHistory := make([]*pb.Message, len(history))
	for i, h := range history {
		protoHistory[i] = &pb.Message{Role: h.Role, Content: h.Content}
	}

	// F3: use dedicated LLM timeout (default 20s)
	callCtx, cancel := context.WithTimeout(ctx, s.llmTimeout)
	defer cancel()
	// P3-4 (audit): attach the per-request ID to the outgoing call so the
	// helper service can correlate its logs with this backend's logs.
	callCtx = attachOutgoingRequestID(callCtx)

	req := &pb.AskRequest{
		Question:          userMessage,
		History:           protoHistory,
		SystemPrompt:      systemPrompt,
		LlmProvider:       provider,
		SkipRoleDetection: true,
	}

	resp, err := s.client.Ask(callCtx, req)
	if err != nil {
		slog.Error("gRPC ask failed", "error", err)
		s.breakerFail()
		if strings.Contains(err.Error(), "429") || strings.Contains(strings.ToLower(err.Error()), "rate limit") {
			return nil, fmt.Errorf("RATE_LIMIT: %w", err)
		}
		return nil, fmt.Errorf("gRPC ask: %w", err)
	}

	s.breakerSuccess()
	return &ports.LLMResponse{
		Answer: resp.GetAnswer(),
		Role:   resp.GetDetectedRole(),
	}, nil
}

// Embed forwards to the helper's Embed gRPC (VECTOR_SEARCH_PLAN §8.4).
// Length-mismatch validation happens here so mismatched-dim vectors never
// reach the database. F3: uses dedicated embed timeout (default 8s).
func (s *GRPCLLMService) Embed(ctx context.Context, text string) ([]float32, error) {
	slog.Info("llm: Embed", "text_len", len(text))
	if !s.breakerAllow() {
		return nil, fmt.Errorf("helper circuit breaker open — embed degraded")
	}
	if err := s.ensureClient(); err != nil {
		s.breakerFail()
		return nil, err
	}

	// F3: use dedicated embed timeout
	callCtx, cancel := context.WithTimeout(ctx, s.embedTimeout)
	defer cancel()
	// P3-4 (audit): propagate per-request ID to the helper.
	callCtx = attachOutgoingRequestID(callCtx)

	resp, err := s.client.Embed(callCtx, &pb.EmbedRequest{Text: text})
	if err != nil {
		slog.Error("gRPC embed failed", "error", err)
		s.breakerFail()
		return nil, fmt.Errorf("gRPC embed: %w", err)
	}
	if resp == nil {
		s.breakerFail()
		return nil, fmt.Errorf("gRPC embed: nil response")
	}
	if got, want := len(resp.Embedding), expectedEmbeddingDim; got != want {
		slog.Error("gRPC embed dim mismatch", "got", got, "want", want, "model", resp.GetModel())
		return nil, fmt.Errorf("embed dim mismatch: got %d, want %d (model=%s)", got, want, resp.GetModel())
	}
	s.breakerSuccess()
	return resp.Embedding, nil
}

func (s *GRPCLLMService) Health(ctx context.Context) error {
	slog.Info("llm: Health")
	if s.healthURL == "" {
		return fmt.Errorf("no health URL configured")
	}

	healthCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(healthCtx, http.MethodGet, s.healthURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("helper health returned %s", http.StatusText(resp.StatusCode))
	}
	return nil
}

// AdapterNames returns the list of adapters loaded in the helper service,
// as reported by the helper's /health endpoint under "loaded_adapters".
func (s *GRPCLLMService) AdapterNames(ctx context.Context) ([]string, error) {
	slog.Info("llm: AdapterNames")
	if s.healthURL == "" {
		return nil, fmt.Errorf("no health URL configured")
	}

	healthCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(healthCtx, http.MethodGet, s.healthURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("helper health returned %s", http.StatusText(resp.StatusCode))
	}

	var payload struct {
		LoadedAdapters []string `json:"loaded_adapters"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode helper health response: %w", err)
	}
	return payload.LoadedAdapters, nil
}

// attachOutgoingRequestID returns a derived context that carries the
// per-request correlation ID as gRPC metadata on the outbound call. Logs
// on the helper side can then grep for the same ID across both services.
// (P3-4 audit cross-service tracing.)
func attachOutgoingRequestID(ctx context.Context) context.Context {
	id := contextkeys.GetRequestID(ctx)
	if id == "" {
		return ctx
	}
	md := metadata.Pairs(grpcRequestIDMetadataKey, id)
	return metadata.NewOutgoingContext(ctx, md)
}
