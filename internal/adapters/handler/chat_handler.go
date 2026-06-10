package handler

import (
	"context"
	"encoding/json"
	"io"
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
	conn                *grpc.ClientConn
	client              pb.HelperServiceClient
	authURL             string
	mu                  sync.RWMutex
	systemPrompt        string
	workerProfilePrompt string
	llmProvider         string
	db                  *gorm.DB
}

type chatRequest struct {
	Message        string        `json:"message"`
	History        []historyItem `json:"history,omitempty"`
	ConversationID string        `json:"conversation_id,omitempty"`
}

type historyItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Answer         string `json:"answer"`
	DetectedRole   string `json:"detected_role,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
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

// SetWorkerProfilePrompt updates the cached worker profile intake prompt (thread-safe).
func (h *ChatHandler) SetWorkerProfilePrompt(prompt string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.workerProfilePrompt = prompt
	slog.Info("worker_profile_prompt cache updated", "len", len(prompt))
	if len(prompt) > 0 {
		slog.Debug("worker_profile_prompt first 150 chars", "text", prompt[:min(len(prompt), 150)])
	}
}

func (h *ChatHandler) getWorkerProfilePrompt() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.workerProfilePrompt
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

	slog.Info("chat request", "msg_len", len(req.Message), "history_len", len(req.History), "conv_id", req.ConversationID)
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
		http.Error(w, `{"error":"helper service error: `+err.Error()+`"}`, http.StatusServiceUnavailable)
		return
	}

	// Save conversation to DB (synchronous so we can return the correct conversation_id)
	respConvID := req.ConversationID
	userID := h.resolveUserID(r)
	if userID != "" {
		newID, err := h.saveConversation(userID, req.ConversationID, "main", req.Message, resp.Answer, nil)
		if err != nil {
			slog.Warn("chat: failed to save conversation", "error", err)
		} else {
			respConvID = newID
		}
	} else {
		slog.Debug("chat: skipping conversation save — no user session")
	}

	slog.Info("chat response", "answer_len", len(resp.Answer), "detected_role", resp.DetectedRole, "duration_ms", elapsed.Milliseconds(), "conv_id", req.ConversationID, "resp_conv_id", respConvID)

	// If the helper detected a role, update the user via auth service (async, don't block the response)
	if resp.DetectedRole != "" {
		go h.updateUserRole(context.Background(), r, resp.DetectedRole)
	}

	json.NewEncoder(w).Encode(chatResponse{
		Answer:         resp.Answer,
		DetectedRole:   resp.DetectedRole,
		ConversationID: respConvID,
	})
}

// updateUserRole resolves the user ID from the session JWT via direct DB read,
// then calls the auth service (the role authority) to perform the role update.
func (h *ChatHandler) updateUserRole(ctx context.Context, r *http.Request, role string) bool {
	cookie, err := r.Cookie("better-auth-session")
	if err != nil {
		slog.Warn("chat: no session cookie, skipping role update")
		return false
	}

	// The cookie is "<session.token>.<encrypted_payload>" — split to get the raw token
	token := strings.SplitN(cookie.Value, ".", 2)[0]

	type dbSession struct {
		UserID string `gorm:"column:userId"`
	}
	var s dbSession
	err = h.db.Table("\"session\"").Where("token = ? AND \"expiresAt\" > NOW()", token).First(&s).Error
	if err != nil {
		slog.Warn("chat: session not found in DB, skipping role update", "error", err)
		return false
	}

	// Auth service is the authority for role mutations — call it
	authURL := os.Getenv("AUTH_SERVICE_URL")
	if authURL == "" {
		authURL = "http://auth:8083"
	}

	bodyPayload, _ := json.Marshal(map[string]string{"role": role})
	authReq, err := http.NewRequest(
		http.MethodPut,
		authURL+"/api/auth/user/"+s.UserID+"/role",
		strings.NewReader(string(bodyPayload)),
	)
	if err != nil {
		slog.Error("chat: failed to create auth request", "error", err)
		return false
	}
	authReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(authReq)
	if err != nil {
		slog.Error("chat: auth call failed", "user_id", s.UserID, "error", err)
		return false
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		slog.Error("chat: auth returned non-200", "user_id", s.UserID, "status", resp.StatusCode, "body", string(respBody))
		return false
	}

	slog.Info("chat: user role updated via auth", "user_id", s.UserID, "role", role)

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

// resolveUserID extracts the user ID from the better-auth session cookie.
// Tries the auth service's user-id endpoint first, then falls back to
// a direct DB query using the raw session token.
func (h *ChatHandler) resolveUserID(r *http.Request) string {
	// First, try via auth service (validates full JWT cookie)
	if userID := h.resolveUserIDViaAuth(r); userID != "" {
		return userID
	}
	// Fallback: read cookie and query DB directly
	// (works for cookies where we only have the session token part)
	cookie, err := r.Cookie("better-auth-session")
	if err != nil {
		slog.Debug("resolveUserID: no better-auth-session cookie found", "err", err)
		return ""
	}
	if cookie.Value == "" {
		return ""
	}
	token := strings.SplitN(cookie.Value, ".", 2)[0]
	if token == "" {
		return ""
	}
	type dbSession struct {
		UserID string `gorm:"column:userId"`
	}
	var s dbSession
	err = h.db.Table("\"session\"").Where("token = ? AND \"expiresAt\" > NOW()", token).First(&s).Error
	if err != nil {
		slog.Debug("resolveUserID: session not found in DB", "token_prefix", token[:min(len(token), 15)])
		return ""
	}
	return s.UserID
}

// resolveUserIDViaAuth calls the auth service to validate the full JWT cookie.
func (h *ChatHandler) resolveUserIDViaAuth(r *http.Request) string {
	authReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "http://auth:8083/api/auth/user-id", nil)
	if err != nil {
		return ""
	}
	// Copy the better-auth-session cookie from the original request
	for _, c := range r.Cookies() {
		if c.Name == "better-auth-session" {
			authReq.AddCookie(c)
			break
		}
	}
	client := &http.Client{Timeout: 3 * time.Second}
	authResp, err := client.Do(authReq)
	if err != nil {
		return ""
	}
	defer authResp.Body.Close()
	if authResp.StatusCode != http.StatusOK {
		return ""
	}
	var result struct {
		UserID string `json:"userId"`
	}
	if err := json.NewDecoder(authResp.Body).Decode(&result); err != nil {
		return ""
	}
	return result.UserID
}

// conversationMsg is a single message stored in the JSONB array.
type conversationMsg struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// saveConversation persists a pair of messages (user + assistant) to the conversations table.
// If convID is non-empty and belongs to the user, it appends. Otherwise it creates a new conversation.
// Returns the conversation ID.
func (h *ChatHandler) saveConversation(userID, convID, convType string, reqMsg, respMsg string, fields json.RawMessage) (string, error) {
	now := time.Now()
	newMsgs := []conversationMsg{
		{Role: "user", Content: reqMsg, Timestamp: now},
		{Role: "assistant", Content: respMsg, Timestamp: now.Add(time.Second)},
	}

	if convID != "" {
		// Append to existing conversation (must belong to this user)
		var existing core.Conversation
		if err := h.db.First(&existing, "id = ? AND user_id = ?", convID, userID).Error; err != nil {
			// Conversation doesn't exist or doesn't belong to user — create new instead
			slog.Warn("saveConversation: conversation not found or not owned, creating new", "convID", convID, "userID", userID, "error", err)
			convID = ""
		} else {
			var msgs []conversationMsg
			if err := json.Unmarshal(existing.Messages, &msgs); err != nil {
				msgs = []conversationMsg{}
			}
			msgs = append(msgs, newMsgs...)
			updatedMsgs, _ := json.Marshal(msgs)

			updates := map[string]interface{}{
				"messages":  updatedMsgs,
				"updated_at": now,
			}
			// Update metadata with latest fields for worker chat
			if fields != nil {
				meta := map[string]interface{}{}
				if existing.Metadata != nil {
					json.Unmarshal(existing.Metadata, &meta)
				}
				meta["extracted_fields"] = fields
				metaJSON, _ := json.Marshal(meta)
				updates["metadata"] = metaJSON
			}

			if err := h.db.Model(&core.Conversation{}).Where("id = ?", convID).Updates(updates).Error; err != nil {
				return "", err
			}
			slog.Info("saveConversation: appended to existing", "convID", convID, "type", convType, "total_msgs", len(newMsgs)+len(msgs)-2)
			return convID, nil
		}
	}

	// Create new conversation
	msgsJSON, _ := json.Marshal(newMsgs)
	title := reqMsg
	if len(title) > 80 {
		title = title[:80] + "..."
	}

	meta := map[string]interface{}{}
	if fields != nil {
		meta["extracted_fields"] = fields
	}
	if convType == "worker" {
		meta["type"] = "profile_intake"
		meta["completed"] = false
	}
	metaJSON, _ := json.Marshal(meta)

	conv := core.Conversation{
		UserID:   userID,
		Type:     convType,
		Title:    title,
		Messages: msgsJSON,
		Metadata: metaJSON,
	}
	if err := h.db.Create(&conv).Error; err != nil {
		return "", err
	}
	slog.Info("saveConversation: created new", "convID", conv.ID, "type", convType)
	return conv.ID, nil
}

// ----------- Worker Profile Intake Chat -----------

type workerChatResponse struct {
	Answer         string          `json:"answer"`
	DetectedFields json.RawMessage `json:"detected_fields,omitempty"`
	ConversationID string          `json:"conversation_id,omitempty"`
}

// HandleWorkerChat handles the worker profile intake chat.
// It uses the worker_profile_prompt system prompt and parses [FIELDS] blocks
// from the LLM response to auto-fill the worker profile form.
func (h *ChatHandler) HandleWorkerChat(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		slog.Warn("worker-chat: invalid method", "method", r.Method)
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Warn("worker-chat: invalid JSON", "error", err)
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		slog.Warn("worker-chat: empty message")
		http.Error(w, `{"error":"message cannot be empty"}`, http.StatusBadRequest)
		return
	}

	slog.Info("worker-chat request", "msg_len", len(req.Message), "history_len", len(req.History), "conv_id", req.ConversationID)
	sp := h.getWorkerProfilePrompt()
	prov := h.getLLMProvider()
	if sp == "" {
		slog.Warn("worker-chat: no worker_profile_prompt configured, falling back to general prompt")
		sp = h.getSystemPrompt()
	}
	slog.Info("worker-chat gRPC call", "msg_len", len(req.Message), "history_len", len(req.History), "sp_len", len(sp), "provider", prov)
	if len(sp) > 0 {
		slog.Debug("worker-chat prompt[:150]", "text", sp[:min(len(sp), 150)])
	}

	if err := h.ensureClient(); err != nil {
		slog.Error("worker-chat: helper unreachable")
		http.Error(w, `{"error":"helper service unreachable"}`, http.StatusServiceUnavailable)
		return
	}

	history := make([]*pb.Message, len(req.History))
	for i, m := range req.History {
		history[i] = &pb.Message{Role: m.Role, Content: m.Content}
	}

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
		slog.Error("worker-chat: gRPC call failed", "error", err, "duration_ms", elapsed.Milliseconds())
		if strings.Contains(err.Error(), "429") || strings.Contains(strings.ToLower(err.Error()), "rate limit") {
			json.NewEncoder(w).Encode(workerChatResponse{
				Answer: "I'm temporarily rate-limited. Please try again in a minute.",
			})
			return
		}
		http.Error(w, `{"error":"helper service error: `+err.Error()+`"}`, http.StatusServiceUnavailable)
		return
	}

	// Parse [FIELDS] blocks from the LLM response
	answer, fields := parseFieldsFromAnswer(resp.Answer)
	if fields != nil {
		slog.Info("worker-chat: extracted fields", "fields_json", string(fields), "answer_without_fields_len", len(answer))
	} else {
		slog.Debug("worker-chat: no [FIELDS] block found in response")
	}

	// Save conversation to DB (synchronous so we can return the correct conversation_id)
	respConvID := req.ConversationID
	userID := h.resolveUserID(r)
	if userID != "" {
		newID, err := h.saveConversation(userID, req.ConversationID, "worker", req.Message, answer, fields)
		if err != nil {
			slog.Warn("worker-chat: failed to save conversation", "error", err)
		} else {
			respConvID = newID
		}
	} else {
		slog.Debug("worker-chat: skipping conversation save — no user session")
	}

	slog.Info("worker-chat response", "answer_len", len(answer), "duration_ms", elapsed.Milliseconds(), "conv_id", req.ConversationID, "resp_conv_id", respConvID)

	json.NewEncoder(w).Encode(workerChatResponse{
		Answer:         answer,
		DetectedFields: fields,
		ConversationID: respConvID,
	})
}

// parseFieldsFromAnswer extracts the last [FIELDS]...[/FIELDS] block from the
// LLM answer, strips it from the response, and returns the JSON as raw bytes.
// Returns the cleaned answer and the raw JSON fields (or nil if no block found).
func parseFieldsFromAnswer(answer string) (string, json.RawMessage) {
	const openTag  = "[FIELDS]"
	const closeTag = "[/FIELDS]"

	lastOpen := strings.LastIndex(answer, openTag)
	if lastOpen < 0 {
		return answer, nil
	}
	// Find the matching close tag after the opening tag
	afterOpen := answer[lastOpen+len(openTag):]
	closeIdx := strings.Index(afterOpen, closeTag)
	if closeIdx < 0 {
		return answer, nil
	}
	raw := afterOpen[:closeIdx]
	// Validate that it's parseable JSON
	var dummy interface{}
	if err := json.Unmarshal([]byte(raw), &dummy); err != nil {
		slog.Warn("worker-chat: [FIELDS] content is not valid JSON", "raw", raw[:min(len(raw), 100)], "error", err)
		return answer, nil
	}
	// Strip the entire [FIELDS]json[/FIELDS] block plus any trailing whitespace
	cleaned := strings.TrimSpace(answer[:lastOpen] + afterOpen[closeIdx+len(closeTag):])
	return cleaned, json.RawMessage(raw)
}
