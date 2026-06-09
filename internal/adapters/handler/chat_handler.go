package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/core"
	pb "github.com/HelpingPeopleNow/backend/proto/helper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gorm.io/gorm"
)

// ChatHandler proxies questions to the helper service via gRPC.
type ChatHandler struct {
	conn          *grpc.ClientConn
	client        pb.HelperServiceClient
	authURL       string
	mu            sync.RWMutex
	systemPrompt  string
	llmProvider   string
	db            *gorm.DB
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
	Answer        string `json:"answer"`
	DetectedRole  string `json:"detected_role,omitempty"`
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

func NewChatHandler(db *gorm.DB) *ChatHandler {
	helperAddr := os.Getenv("HELPER_GRPC_ADDR")
	if helperAddr == "" {
		helperAddr = "helpingpeoplenow-helper:50051"
	}
	authURL := os.Getenv("AUTH_SERVICE_URL")
	if authURL == "" {
		authURL = "http://auth:8083"
	}
	conn, client := dialHelper(helperAddr)
	return &ChatHandler{conn: conn, client: client, authURL: authURL, db: db}
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

// SetSystemPrompt updates the cached system prompt (thread-safe).
// Called at startup and whenever the admin modifies the prompt via PUT.
func (h *ChatHandler) SetSystemPrompt(prompt string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.systemPrompt = prompt
	slog.Info("system_prompt cache updated", "len", len(prompt))
	if len(prompt) > 0 {
		slog.Debug("system_prompt first 150 chars", "text", prompt[:min(len(prompt), 150)])
	}
}

func (h *ChatHandler) getSystemPrompt() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.systemPrompt
}

// SetLLMProvider updates the cached LLM provider (thread-safe).
// Called at startup and whenever the admin modifies it via PUT.
func (h *ChatHandler) SetLLMProvider(provider string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.llmProvider = provider
	slog.Info("llm_provider cache updated", "provider", provider)
}

func (h *ChatHandler) getLLMProvider() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.llmProvider
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
	sp := h.getSystemPrompt()
	prov := h.getLLMProvider()
	slog.Info("chat gRPC call", "msg_len", len(req.Message), "history_len", len(req.History), "sp_len", len(sp), "provider", prov)
	if len(sp) > 0 {
		slog.Debug("chat system_prompt[:150]", "text", sp[:min(len(sp), 150)])
	}

	if err := h.ensureClient(); err != nil {
		slog.Error("chat: helper unreachable")
		http.Error(w, `{"error":"helper service unreachable"}`, http.StatusServiceUnavailable)
		return
	}

	history := make([]*pb.Message, len(req.History))
	for i, m := range req.History {
		history[i] = &pb.Message{Role: m.Role, Content: m.Content}
	}

	// gRPC timeout: configurable via HELPER_TIMEOUT_SECONDS, default 60s for CPU inference
	timeoutSec := 60 * time.Second
	if ts := os.Getenv("HELPER_TIMEOUT_SECONDS"); ts != "" {
		if v, err := strconv.Atoi(ts); err == nil && v > 0 {
			timeoutSec = time.Duration(v) * time.Second
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeoutSec)
	defer cancel()

	start := time.Now()
	resp, err := h.client.Ask(ctx, &pb.AskRequest{
		Question:     req.Message,
		History:      history,
		SystemPrompt: sp,
		LlmProvider:  prov,
	})
	elapsed := time.Since(start)

	if err != nil {
			slog.Error("chat: gRPC call failed", "error", err, "duration_ms", elapsed.Milliseconds())
			// If rate-limited, return a readable message instead of HTTP error
			if strings.Contains(err.Error(), "429") || strings.Contains(strings.ToLower(err.Error()), "rate limit") {
				json.NewEncoder(w).Encode(chatResponse{
					Answer: "I'm temporarily rate-limited. Please try again in a minute.",
				})
				return
			}
			http.Error(w, `{"error":"helper service error: `+err.Error()+`"}`, http.StatusServiceUnavailable)
			return
		}

	slog.Info("chat response", "answer_len", len(resp.Answer), "detected_role", resp.DetectedRole, "duration_ms", elapsed.Milliseconds())

	// If the helper detected a role, update the user via auth service (async, don't block the response)
	if resp.DetectedRole != "" {
		go h.updateUserRole(context.Background(), r, resp.DetectedRole)
	}

	json.NewEncoder(w).Encode(chatResponse{
		Answer:       resp.Answer,
		DetectedRole: resp.DetectedRole,
	})
}

// updateUserRole extracts the user ID from the session cookie via direct DB lookup,
// then updates the user's role directly in the database.
func (h *ChatHandler) updateUserRole(ctx context.Context, r *http.Request, role string) bool {
	// Get the session cookie
	cookie, err := r.Cookie("better-auth.session_token")
	if err != nil {
		slog.Warn("chat: no session cookie, skipping role update")
		return false
	}

	// Look up the session directly in the database
	type dbSession struct {
		UserID string `gorm:"column:userId"`
	}
	var s dbSession
	err = h.db.Table("\"session\"").Where("token = ? AND \"expiresAt\" > NOW()", cookie.Value).First(&s).Error
	if err != nil {
		slog.Warn("chat: session not found in DB, skipping role update", "error", err)
		return false
	}

	// Update the user's role directly in the database
	err = h.db.Table("\"user\"").Where("id = ?", s.UserID).Update("role", role).Error
	if err != nil {
		slog.Error("chat: failed to update user role", "user_id", s.UserID, "error", err)
		return false
	}

	slog.Info("chat: user role updated", "user_id", s.UserID, "role", role)

	// Auto-create an empty worker profile if one doesn't exist yet
	go h.ensureWorkerProfile(s.UserID)

	return true
}

func (h *ChatHandler) ensureWorkerProfile(userID string) {
	var count int64
	h.db.Model(&core.WorkerProfile{}).Where("user_id = ?", userID).Count(&count)
	if count > 0 {
		return
	}
	wp := &core.WorkerProfile{UserID: userID}
	if err := h.db.Create(wp).Error; err != nil {
		slog.Warn("chat: failed to auto-create worker profile", "user_id", userID, "error", err)
		return
	}
	slog.Info("chat: auto-created worker profile", "user_id", userID)
}

// strReader is a small helper to create a strings.Reader-like from a string.
type strReader string

func (s strReader) Read(p []byte) (int, error) {
	return copy(p, s), nil
}
