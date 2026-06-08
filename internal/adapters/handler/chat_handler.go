package handler

import (
	"context"
	"encoding/json"
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
	Message string       `json:"message"`
	History []historyItem `json:"history,omitempty"`
}

type historyItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Answer string `json:"answer"`
}

func NewChatHandler() *ChatHandler {
	helperAddr := os.Getenv("HELPER_GRPC_ADDR")
	if helperAddr == "" {
		helperAddr = "helpingpeoplenow-helper:50051"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, helperAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		// Fallback: will reconnect on each request
		return &ChatHandler{conn: nil, client: nil}
	}

	return &ChatHandler{
		conn:   conn,
		client: pb.NewHelperServiceClient(conn),
	}
}

func (h *ChatHandler) ensureClient() error {
	if h.client != nil {
		return nil
	}

	helperAddr := os.Getenv("HELPER_GRPC_ADDR")
	if helperAddr == "" {
		helperAddr = "helpingpeoplenow-helper:50051"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, helperAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return err
	}

	h.conn = conn
	h.client = pb.NewHelperServiceClient(conn)
	return nil
}

func (h *ChatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, `{"error":"message cannot be empty"}`, http.StatusBadRequest)
		return
	}

	if err := h.ensureClient(); err != nil {
		http.Error(w, `{"error":"helper service unreachable"}`, http.StatusServiceUnavailable)
		return
	}

	// Build gRPC request
	history := make([]*pb.Message, len(req.History))
	for i, h := range req.History {
		history[i] = &pb.Message{Role: h.Role, Content: h.Content}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := h.client.Ask(ctx, &pb.AskRequest{
		Question: req.Message,
		History:  history,
	})
	if err != nil {
		http.Error(w, `{"error":"helper service error: `+err.Error()+`"}`, http.StatusServiceUnavailable)
		return
	}

	json.NewEncoder(w).Encode(chatResponse{Answer: resp.Answer})
}
