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
	clientProfilePrompt string
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

// SetClientProfilePrompt updates the cached client profile intake prompt (thread-safe).
func (h *ChatHandler) SetClientProfilePrompt(prompt string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clientProfilePrompt = prompt
	slog.Info("client_profile_prompt cache updated", "len", len(prompt))
	if len(prompt) > 0 {
		slog.Debug("client_profile_prompt first 150 chars", "text", prompt[:min(len(prompt), 150)])
	}
}

func (h *ChatHandler) getClientProfilePrompt() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.clientProfilePrompt
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

	// If the conversation already has a detected_role, skip re-detection
	// unless the user has since cleared their role.
	var metaDetectedRole string
	if req.ConversationID != "" {
		var existing core.Conversation
		if err := h.db.First(&existing, "id = ?", req.ConversationID).Error; err == nil {
			if existing.Metadata != nil {
				var meta map[string]interface{}
				if err := json.Unmarshal(existing.Metadata, &meta); err == nil {
					if role, ok := meta["detected_role"]; ok {
						metaDetectedRole, _ = role.(string)
					}
				}
			}
		}
	}

	skipRoleDetection := metaDetectedRole != ""
	if skipRoleDetection {
		if userID := h.resolveUserID(r); userID != "" {
			type dbUserRole struct {
				Role string `gorm:"column:role"`
			}
			var u dbUserRole
			if err := h.db.Table("\"user\"").Where("id = ?", userID).First(&u).Error; err == nil {
				if u.Role == "" {
					skipRoleDetection = false
					slog.Debug("chat: user cleared role, allowing re-detection", "conv_id", req.ConversationID, "meta_role", metaDetectedRole)
				}
			}
		}
	}
	if skipRoleDetection {
		slog.Debug("chat: skipping role detection", "conv_id", req.ConversationID, "meta_role", metaDetectedRole)
	}

	slog.Info("chat gRPC call", "msg_len", len(req.Message), "history_len", len(req.History), "sp_len", len(sp), "provider", prov, "skip_role_detection", skipRoleDetection)
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
		Question:          req.Message,
		History:           history,
		SystemPrompt:      sp,
		LlmProvider:       prov,
		SkipRoleDetection: skipRoleDetection,
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
		metadata := map[string]interface{}{}
		if resp.DetectedRole == "worker" || resp.DetectedRole == "client" {
			metadata["detected_role"] = resp.DetectedRole
		}
		newID, err := h.saveConversation(userID, req.ConversationID, "main", req.Message, resp.Answer, nil, metadata)
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
	cookie, ok := sessionCookie(r)
	if !ok {
		slog.Warn("chat: no session cookie, skipping role update")
		return false
	}

	// The cookie is "<session.token>.<encrypted_payload>" — split to get the raw token
	token := rawSessionToken(cookie)
	if token == "" {
		slog.Warn("chat: empty session token, skipping role update")
		return false
	}

	type dbSession struct {
		UserID string `gorm:"column:userId"`
	}
	var s dbSession
	err := h.db.Table("\"session\"").Where("token = ? AND \"expiresAt\" > NOW()", token).First(&s).Error
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
	addSessionCookie(authReq, r)

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
	cookie, ok := sessionCookie(r)
	if !ok {
		slog.Debug("resolveUserID: no supported session cookie found")
		return ""
	}
	token := rawSessionToken(cookie)
	if token == "" {
		return ""
	}
	type dbSession struct {
		UserID string `gorm:"column:userId"`
	}
	var s dbSession
	err := h.db.Table("\"session\"").Where("token = ? AND \"expiresAt\" > NOW()", token).First(&s).Error
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
	addSessionCookie(authReq, r)
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

// saveConversation persists a pair of messages (user + assistant) to the messages table.
// If convID is non-empty and belongs to the user, it appends. Otherwise it creates a new conversation.
// Returns the conversation ID.
func (h *ChatHandler) saveConversation(userID, convID, convType string, reqMsg, respMsg string, fields json.RawMessage, metadata map[string]interface{}) (string, error) {
	if convID != "" {
		// Append to existing conversation (must belong to this user)
		var existing core.Conversation
		if err := h.db.First(&existing, "id = ? AND user_id = ?", convID, userID).Error; err != nil {
			// Conversation doesn't exist or doesn't belong to user — create new instead
			slog.Warn("saveConversation: conversation not found or not owned, creating new", "convID", convID, "userID", userID, "error", err)
			convID = ""
		} else {
			// Insert both messages
			for _, msg := range []core.Message{
				{ConversationID: convID, Role: "user", Content: reqMsg},
				{ConversationID: convID, Role: "assistant", Content: respMsg},
			} {
				if err := h.db.Create(&msg).Error; err != nil {
					return "", err
				}
			}

			// Update metadata + updated_at
			updates := map[string]interface{}{
				"updated_at": time.Now(),
			}
			if fields != nil || len(metadata) > 0 {
				meta := map[string]interface{}{}
				if existing.Metadata != nil {
					json.Unmarshal(existing.Metadata, &meta)
				}
				if fields != nil {
					meta["extracted_fields"] = fields
				}
				for k, v := range metadata {
					meta[k] = v
				}
				metaJSON, _ := json.Marshal(meta)
				updates["metadata"] = metaJSON
			}

			if err := h.db.Model(&core.Conversation{}).Where("id = ?", convID).Updates(updates).Error; err != nil {
				return "", err
			}

			slog.Info("saveConversation: appended to existing", "convID", convID, "type", convType)
			return convID, nil
		}
	}

	// Create new conversation
	meta := map[string]interface{}{}
	if fields != nil {
		meta["extracted_fields"] = fields
	}
	for k, v := range metadata {
		meta[k] = v
	}
	if convType == "worker" || convType == "client" {
		meta["type"] = "profile_intake"
		meta["completed"] = false
	}
	metaJSON, _ := json.Marshal(meta)

	conv := core.Conversation{
		UserID:   userID,
		Type:     convType,
		Metadata: metaJSON,
	}
	if err := h.db.Create(&conv).Error; err != nil {
		return "", err
	}

	// Insert both messages
	for _, msg := range []core.Message{
		{ConversationID: conv.ID, Role: "user", Content: reqMsg},
		{ConversationID: conv.ID, Role: "assistant", Content: respMsg},
	} {
		if err := h.db.Create(&msg).Error; err != nil {
			return "", err
		}
	}

	slog.Info("saveConversation: created new", "convID", conv.ID, "type", convType)
	return conv.ID, nil
}

// ----------- Client Profile Intake Chat -----------

// HandleClientChat handles the client profile intake chat.
// It uses the client_profile_prompt system prompt and parses [FIELDS] blocks
// from the LLM response to auto-fill the client profile form.
func (h *ChatHandler) HandleClientChat(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		slog.Warn("client-chat: invalid method", "method", r.Method)
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Warn("client-chat: invalid JSON", "error", err)
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		slog.Warn("client-chat: empty message")
		http.Error(w, `{"error":"message cannot be empty"}`, http.StatusBadRequest)
		return
	}

	slog.Info("client-chat request", "msg_len", len(req.Message), "history_len", len(req.History), "conv_id", req.ConversationID)
	sp := h.getClientProfilePrompt()
	prov := h.getLLMProvider()
	if sp == "" {
		slog.Warn("client-chat: no client_profile_prompt configured, falling back to general prompt")
		sp = h.getSystemPrompt()
	}
	slog.Info("client-chat gRPC call", "msg_len", len(req.Message), "history_len", len(req.History), "sp_len", len(sp), "provider", prov)

	if err := h.ensureClient(); err != nil {
		slog.Error("client-chat: helper unreachable")
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
		slog.Error("client-chat: gRPC call failed", "error", err, "duration_ms", elapsed.Milliseconds())
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
		slog.Info("client-chat: extracted fields", "fields_json", string(fields), "answer_without_fields_len", len(answer))
	} else {
		slog.Debug("client-chat: no [FIELDS] block found in response")
	}

	// Save conversation to DB
	respConvID := req.ConversationID
	userID := h.resolveUserID(r)
	if userID != "" {
		newID, err := h.saveConversation(userID, req.ConversationID, "client", req.Message, answer, fields, nil)
		if err != nil {
			slog.Warn("client-chat: failed to save conversation", "error", err)
		} else {
			respConvID = newID
		}
	} else {
		slog.Debug("client-chat: skipping conversation save — no user session")
	}

	// If fields were extracted via [FIELDS], upsert the client profile into client_profiles
	if fields != nil && userID != "" {
		var rawMap map[string]interface{}
		if err := json.Unmarshal(fields, &rawMap); err == nil {
			var existing core.ClientProfile
			found := h.db.Where("user_id = ?", userID).First(&existing).Error == nil
			cp := existing
			if !found {
				cp = core.ClientProfile{UserID: userID}
			}

			if v, ok := rawString(rawMap, "full_name"); ok { cp.FullName = v }
			if v, ok := rawString(rawMap, "phone"); ok { cp.Phone = v }
			if v, ok := rawString(rawMap, "city"); ok { cp.City = v }
			if v, ok := rawString(rawMap, "address"); ok { cp.Address = v }
			if v, ok := rawString(rawMap, "bio"); ok { cp.Bio = v }
			if v, ok := rawString(rawMap, "preferred_contact"); ok { cp.PreferredContact = v }
			if v, ok := rawString(rawMap, "property_type"); ok { cp.PropertyType = v }
			if v, ok := rawString(rawMap, "notes"); ok { cp.Notes = v }

			if found {
				if err := h.db.Save(&cp).Error; err != nil {
					slog.Warn("client-chat: failed to save client profile", "error", err)
				} else {
					slog.Info("client-chat: client profile saved", "user_id", userID, "full_name", cp.FullName)
				}
			} else {
				if err := h.db.Create(&cp).Error; err != nil {
					slog.Warn("client-chat: failed to create client profile", "error", err)
				} else {
					slog.Info("client-chat: client profile created", "user_id", userID, "full_name", cp.FullName)
				}
			}
		} else {
			slog.Warn("client-chat: failed to parse fields JSON into rawMap", "error", err)
		}
	}

	slog.Info("client-chat response", "answer_len", len(answer), "duration_ms", elapsed.Milliseconds(), "conv_id", req.ConversationID, "resp_conv_id", respConvID)

	json.NewEncoder(w).Encode(workerChatResponse{
		Answer:         answer,
		DetectedFields: fields,
		ConversationID: respConvID,
	})
}

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
		newID, err := h.saveConversation(userID, req.ConversationID, "worker", req.Message, answer, fields, nil)
		if err != nil {
			slog.Warn("worker-chat: failed to save conversation", "error", err)
		} else {
			respConvID = newID
		}
	} else {
		slog.Debug("worker-chat: skipping conversation save — no user session")
	}

	// If fields were extracted via [FIELDS], upsert the worker profile
	if fields != nil && userID != "" {
		var rawMap map[string]interface{}
		if err := json.Unmarshal(fields, &rawMap); err == nil {
			var existing core.WorkerProfile
			found := h.db.Where("user_id = ?", userID).First(&existing).Error == nil
			wp := existing
			if !found {
				wp = core.WorkerProfile{UserID: userID}
			}

			if v, ok := rawString(rawMap, "profession"); ok { wp.Profession = v }
			if v, ok := rawString(rawMap, "business_name"); ok { wp.BusinessName = v }
			if v, ok := rawString(rawMap, "bio"); ok { wp.Bio = v }
			if v, ok := rawString(rawMap, "phone"); ok { wp.Phone = v }
			if v, ok := rawString(rawMap, "city"); ok { wp.City = v }
			if v, ok := rawString(rawMap, "address"); ok { wp.Address = v }
			if v, ok := rawString(rawMap, "website"); ok { wp.Website = v }

			if v, ok := rawFloat(rawMap, "hourly_rate"); ok { wp.HourlyRate = v }
			if v, ok := rawFloat(rawMap, "minimum_charge"); ok { wp.MinimumCharge = v }
			if v, ok := rawInt(rawMap, "service_radius_km"); ok { wp.ServiceRadiusKm = v }
			if v, ok := rawInt(rawMap, "years_experience"); ok { wp.YearsExperience = v }

			if v, ok := rawBool(rawMap, "free_estimate"); ok { wp.FreeEstimate = v }
			if v, ok := rawBool(rawMap, "has_insurance"); ok { wp.HasInsurance = v }
			if v, ok := rawBool(rawMap, "emergency_service"); ok { wp.EmergencyService = v }

			if v, ok := rawMap["certifications"]; ok {
				if arr, ok := v.([]interface{}); ok {
					b, _ := json.Marshal(arr)
					wp.Certifications = string(b)
				} else if s, ok := v.(string); ok {
					b, _ := json.Marshal([]string{s})
					wp.Certifications = string(b)
				} else if v == nil {
					wp.Certifications = ""
				}
			}
			if v, ok := rawMap["languages"]; ok {
				if arr, ok := v.([]interface{}); ok {
					b, _ := json.Marshal(arr)
					wp.Languages = string(b)
				} else if s, ok := v.(string); ok {
					b, _ := json.Marshal([]string{s})
					wp.Languages = string(b)
				} else if v == nil {
					wp.Languages = ""
				}
			}

			hasSocialKey := false
			socialFieldNames := map[string]string{
				"instagram": "Instagram", "facebook": "Facebook",
				"twitter": "Twitter", "linkedin": "LinkedIn",
				"tiktok": "TikTok", "youtube": "YouTube",
			}
			for field := range socialFieldNames {
				if _, ok := rawMap[field]; ok {
					hasSocialKey = true
					break
				}
			}
			if _, ok := rawMap["social_links"]; ok {
				hasSocialKey = true
			}
			if hasSocialKey {
				var links []map[string]string
				var existingLinks []core.SocialLink
				json.Unmarshal([]byte(wp.SocialLinks), &existingLinks)
				knownPlatforms := map[string]bool{}
				for _, l := range existingLinks {
					key := strings.ToLower(l.Platform)
					if !knownPlatforms[key] {
						links = append(links, map[string]string{"platform": l.Platform, "url": l.URL})
						knownPlatforms[key] = true
					}
				}
				for field, platform := range socialFieldNames {
					if v, ok := rawString(rawMap, field); ok && v != "" {
						key := strings.ToLower(platform)
						if !knownPlatforms[key] {
							links = append(links, map[string]string{"platform": platform, "url": v})
							knownPlatforms[key] = true
						} else {
							for i, l := range links {
								if strings.ToLower(l["platform"]) == key {
									links[i]["url"] = v
									break
								}
							}
						}
					}
				}
				if v, ok := rawMap["social_links"]; ok {
					if arr, ok := v.([]interface{}); ok {
						for _, item := range arr {
							if m, ok := item.(map[string]interface{}); ok {
								l := map[string]string{}
								if p, ok := m["platform"].(string); ok {
									l["platform"] = p
								}
								if u, ok := m["url"].(string); ok {
									l["url"] = u
								}
								if l["platform"] != "" || l["url"] != "" {
									key := strings.ToLower(l["platform"])
									if !knownPlatforms[key] {
										links = append(links, l)
										knownPlatforms[key] = true
									} else {
										for i, existing := range links {
											if strings.ToLower(existing["platform"]) == key {
												if l["url"] != "" {
													links[i]["url"] = l["url"]
												}
												break
											}
										}
									}
								}
							}
						}
					}
				}
				if len(links) > 0 {
					b, _ := json.Marshal(links)
					wp.SocialLinks = string(b)
				} else if v, ok := rawMap["social_links"]; ok && v == nil {
					wp.SocialLinks = ""
				}
			}

			if found {
				if err := h.db.Save(&wp).Error; err != nil {
					slog.Warn("worker-chat: failed to save worker profile", "error", err)
				} else {
					slog.Info("worker-chat: worker profile saved", "user_id", userID, "profession", wp.Profession)
				}
			} else {
				if err := h.db.Create(&wp).Error; err != nil {
					slog.Warn("worker-chat: failed to create worker profile", "error", err)
				} else {
					slog.Info("worker-chat: worker profile created", "user_id", userID, "profession", wp.Profession)
				}
			}
		} else {
			slog.Warn("worker-chat: failed to parse fields JSON into rawMap", "error", err)
		}
	}

	slog.Info("worker-chat response", "answer_len", len(answer), "duration_ms", elapsed.Milliseconds(), "conv_id", req.ConversationID, "resp_conv_id", respConvID)

	json.NewEncoder(w).Encode(workerChatResponse{
		Answer:         answer,
		DetectedFields: fields,
		ConversationID: respConvID,
	})
}

func rawString(m map[string]interface{}, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	if v == nil {
		return "", true
	}
	if s, ok := v.(string); ok {
		return s, true
	}
	return "", false
}

func rawFloat(m map[string]interface{}, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	if v == nil {
		return 0, true
	}
	if f, ok := v.(float64); ok {
		return f, true
	}
	return 0, false
}

func rawInt(m map[string]interface{}, key string) (int, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	if v == nil {
		return 0, true
	}
	if f, ok := v.(float64); ok {
		return int(f), true
	}
	if s, ok := v.(string); ok {
		n, err := strconv.Atoi(s)
		if err == nil {
			return n, true
		}
	}
	return 0, false
}

func rawBool(m map[string]interface{}, key string) (bool, bool) {
	v, ok := m[key]
	if !ok {
		return false, false
	}
	if v == nil {
		return false, true
	}
	if b, ok := v.(bool); ok {
		return b, true
	}
	if f, ok := v.(float64); ok {
		return f != 0, true
	}
	if s, ok := v.(string); ok {
		return strings.EqualFold(s, "true") || s == "1", true
	}
	return false, false
}

// parseFieldsFromAnswer extracts the last [FIELDS]...[/FIELDS] block from the
// LLM answer, strips it from the response, and returns the JSON as raw bytes.
// Returns the cleaned answer and the raw JSON fields (or nil if no block found).
func parseFieldsFromAnswer(answer string) (string, json.RawMessage) {
	const openTag = "[FIELDS]"
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

// HandleResetRole clears the user's role to allow re-detection.
func (h *ChatHandler) HandleResetRole(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		slog.Warn("reset-role: invalid method", "method", r.Method)
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	userID := h.resolveUserID(r)
	if userID == "" {
		slog.Warn("reset-role: no user session")
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Clear role via direct DB write (same pattern as auth service)
	if err := h.db.Table("\"user\"").Where("id = ?", userID).Update("role", "").Error; err != nil {
		slog.Error("reset-role: failed to update", "error", err)
		http.Error(w, `{"error":"failed to reset role"}`, http.StatusInternalServerError)
		return
	}

	slog.Info("reset-role: role cleared", "user_id", userID)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
