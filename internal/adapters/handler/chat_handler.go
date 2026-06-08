package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	pb "github.com/HelpingPeopleNow/backend/proto/helper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ChatHandler proxies questions to the helper service via gRPC.
type ChatHandler struct {
	conn   *grpc.ClientConn
	client pb.HelperServiceClient
}

type chatRequest struct {
	Message string         `json:"message"`
	History []historyItem `json:"history,omitempty"`
}

type historyItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Answer string `json:"answer"`
}

func dialHelper(addr string) (*grpc.ClientConn, pb.HelperServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	slog.Info("connecting to helper gRPC", "addr", addr)
	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		slog.Warn("helper gRPC connection failed (will retry on request)", "addr", addr, "error", err)
		return nil, nil
	}
	client := pb.NewHelperServiceClient(conn)
	slog.Info("helper gRPC connected", "addr", addr)
	return conn, client
}

func NewChatHandler() *ChatHandler {
	helperAddr := os.Getenv("HELPER_GRPC_ADDR")
	if helperAddr == "" {
		helperAddr = "helpingpeoplenow-helper:50051"
	}
	conn, client := dialHelper(helperAddr)
	return &ChatHandler{conn: conn, client: client}
}

func (h *ChatHandler) ensureClient() error {
	if h.client != nil {
		return nil
	}
	helperAddr := os.Getenv("HELPER_GRPC_ADDR")
	if helperAddr == "" {
		helperAddr = "helpingpeoplenow-helper:50051"
	}
	conn, client := dialHelper(helperAddr)
	h.conn = conn
	h.client = client
	if client == nil {
		return grpc.ErrClientConnClosing
	}
	return nil
}

func (h *ChatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		slog.Warn("chat: invalid method", "method", r.Method)
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Warn("chat: invalid JSON", "error", err)
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		slog.Warn("chat: empty message")
		http.Error(w, `{"error":"message cannot be empty"}`, http.StatusBadRequest)
		return
	}

	slog.Info("chat request", "msg_len", len(req.Message), "history_len", len(req.History))

	if err := h.ensureClient(); err != nil {
		slog.Error("chat: helper unreachable")
		http.Error(w, `{"error":"helper service unreachable"}`, http.StatusServiceUnavailable)
		return
	}

	history := make([]*pb.Message, len(req.History))
	for i, m := range req.History {
		history[i] = &pb.Message{Role: m.Role, Content: m.Content}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := h.client.Ask(ctx, &pb.AskRequest{
		Question: req.Message,
		History:  history,
	})
	elapsed := time.Since(start)

	if err != nil {
		slog.Error("chat: gRPC call failed", "error", err, "duration_ms", elapsed.Milliseconds())
		http.Error(w, `{"error":"helper service error: `+err.Error()+`"}`, http.StatusServiceUnavailable)
		return
	}

	slog.Info("chat response", "answer_len", len(resp.Answer), "duration_ms", elapsed.Milliseconds())
	json.NewEncoder(w).Encode(chatResponse{Answer: resp.Answer})
}
