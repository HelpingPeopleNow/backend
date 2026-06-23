package llm

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/ports"
	pb "github.com/HelpingPeopleNow/backend/proto/helper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const expectedEmbeddingDim = 768

type GRPCLLMService struct {
	addr        string
	healthURL   string
	timeoutSecs int
	mu          sync.Mutex
	conn        *grpc.ClientConn
	client      pb.HelperServiceClient
}

func NewGRPCLLMService(addr, healthURL string) ports.LLMService {
	timeout := 60
	if ts := os.Getenv("HELPER_TIMEOUT_SECONDS"); ts != "" {
		if v, err := strconv.Atoi(ts); err == nil && v > 0 {
			timeout = v
		}
	}
	return &GRPCLLMService{
		addr:        addr,
		healthURL:   healthURL,
		timeoutSecs: timeout,
	}
}

func (s *GRPCLLMService) ensureClient() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return nil
	}

	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	slog.Info("llm: dialing helper gRPC", "addr", s.addr)
	conn, err := grpc.DialContext(dialCtx, s.addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return fmt.Errorf("gRPC dial %s: %w", s.addr, err)
	}
	s.conn = conn
	s.client = pb.NewHelperServiceClient(conn)
	slog.Info("llm: helper gRPC connected", "addr", s.addr)
	return nil
}

func (s *GRPCLLMService) Ask(
	ctx context.Context,
	systemPrompt string,
	userMessage string,
	history []ports.MessagePair,
	provider string,
) (*ports.LLMResponse, error) {
	if err := s.ensureClient(); err != nil {
		return nil, err
	}

	protoHistory := make([]*pb.Message, len(history))
	for i, h := range history {
		protoHistory[i] = &pb.Message{Role: h.Role, Content: h.Content}
	}

	callCtx, cancel := context.WithTimeout(ctx, time.Duration(s.timeoutSecs)*time.Second)
	defer cancel()

	req := &pb.AskRequest{
		Question:          userMessage,
		History:           protoHistory,
		SystemPrompt:      systemPrompt,
		LlmProvider:       provider,
		SkipRoleDetection: true,
	}

	resp, err := s.client.Ask(callCtx, req)
	if err != nil {
		if strings.Contains(err.Error(), "429") || strings.Contains(strings.ToLower(err.Error()), "rate limit") {
			return nil, fmt.Errorf("RATE_LIMIT: %w", err)
		}
		return nil, fmt.Errorf("gRPC ask: %w", err)
	}

	return &ports.LLMResponse{
		Answer: resp.GetAnswer(),
		Role:   resp.GetDetectedRole(),
	}, nil
}

// Embed forwards to the helper's Embed gRPC (VECTOR_SEARCH_PLAN §8.4).
// Length-mismatch validation happens here so mismatched-dim vectors never
// reach the database.
func (s *GRPCLLMService) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := s.ensureClient(); err != nil {
		return nil, err
	}

	callCtx, cancel := context.WithTimeout(ctx, time.Duration(s.timeoutSecs)*time.Second)
	defer cancel()

	resp, err := s.client.Embed(callCtx, &pb.EmbedRequest{Text: text})
	if err != nil {
		return nil, fmt.Errorf("gRPC embed: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("gRPC embed: nil response")
	}
	if got, want := len(resp.Embedding), expectedEmbeddingDim; got != want {
		return nil, fmt.Errorf("embed dim mismatch: got %d, want %d (model=%s)", got, want, resp.GetModel())
	}
	return resp.Embedding, nil
}

func (s *GRPCLLMService) Health(ctx context.Context) error {
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
