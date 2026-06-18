package handler

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

	"github.com/HelpingPeopleNow/backend/internal/core"
	pb "github.com/HelpingPeopleNow/backend/proto/helper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gorm.io/gorm"
)

type ChatHandler struct {
	conn                         *grpc.ClientConn
	client                       pb.HelperServiceClient
	authURL                      string
	mu                           sync.RWMutex
	workerProfilePrompt          string
	clientProfilePrompt          string
	findTraderSearchPrompt       string
	findTraderPresentationPrompt string
	llmProvider                  string
	db                           *gorm.DB
}

type chatRequest struct {
	Mode           string        `json:"mode"`
	Message        string        `json:"message"`
	History        []historyItem `json:"history,omitempty"`
	ConversationID string        `json:"conversation_id,omitempty"`
	Lang           string        `json:"lang,omitempty"`
}

type historyItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Answer         string                 `json:"answer"`
	ConversationID string                 `json:"conversation_id,omitempty"`
	Workers        []findTraderWorkerCard `json:"workers,omitempty"`
	DetectedFields json.RawMessage        `json:"detected_fields,omitempty"`
}

type findTraderWorkerCard struct {
	ID               string   `json:"id"`
	Profession       string   `json:"profession"`
	BusinessName     string   `json:"business_name"`
	Bio              string   `json:"bio"`
	City             string   `json:"city"`
	Phone            string   `json:"phone"`
	HourlyRate       float64  `json:"hourly_rate"`
	FreeEstimate     bool     `json:"free_estimate"`
	YearsExperience  int      `json:"years_experience"`
	Certifications   []string `json:"certifications"`
	HasInsurance     bool     `json:"has_insurance"`
	EmergencyService bool     `json:"emergency_service"`
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
	authURL := os.Getenv("AUTH_SERVICE_URL")
	conn, client := dialHelper(helperAddr)
	return &ChatHandler{conn: conn, client: client, authURL: authURL, db: db}
}

func (h *ChatHandler) ensureClient() error {
	if h.client != nil {
		return nil
	}
	helperAddr := os.Getenv("HELPER_GRPC_ADDR")
	conn, client := dialHelper(helperAddr)
	h.conn = conn
	h.client = client
	if client == nil {
		return grpc.ErrClientConnClosing
	}
	return nil
}

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
	if h.workerProfilePrompt != "" {
		return h.workerProfilePrompt
	}
	return defaultWorkerProfilePrompt
}

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
	if h.clientProfilePrompt != "" {
		return h.clientProfilePrompt
	}
	return defaultClientProfilePrompt
}

func (h *ChatHandler) SetFindTraderSearchPrompt(prompt string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.findTraderSearchPrompt = prompt
	slog.Info("find_trader_search_prompt cache updated", "len", len(prompt))
}

func (h *ChatHandler) getFindTraderSearchPrompt() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.findTraderSearchPrompt != "" {
		return h.findTraderSearchPrompt
	}
	return defaultFindTraderSearchPrompt
}

func (h *ChatHandler) SetFindTraderPresentationPrompt(prompt string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.findTraderPresentationPrompt = prompt
	slog.Info("find_trader_presentation_prompt cache updated", "len", len(prompt))
}

func (h *ChatHandler) getFindTraderPresentationPrompt() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.findTraderPresentationPrompt != "" {
		return h.findTraderPresentationPrompt
	}
	return defaultFindTraderPresentationPrompt
}

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

	mode := req.Mode
	if mode == "" {
		mode = "worker_intake"
	}
	if mode != "worker_intake" && mode != "client_intake" && mode != "search" {
		slog.Warn("chat: invalid mode", "mode", mode)
		http.Error(w, `{"error":"invalid mode, must be worker_intake, client_intake, or search"}`, http.StatusBadRequest)
		return
	}

	slog.Info("chat request", "mode", mode, "msg_len", len(req.Message), "history_len", len(req.History), "conv_id", req.ConversationID)

	if err := h.ensureClient(); err != nil {
		slog.Error("chat: helper unreachable")
		http.Error(w, `{"error":"helper service unreachable"}`, http.StatusServiceUnavailable)
		return
	}

	userID := h.resolveUserID(r)
	prov := h.getLLMProvider()

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

	var resp chatResponse
	var err error

	switch mode {
	case "worker_intake":
		resp, err = h.handleIntake(r.Context(), "worker_intake", req, history, userID, prov, timeoutSec)
	case "client_intake":
		resp, err = h.handleIntake(r.Context(), "client_intake", req, history, userID, prov, timeoutSec)
	case "search":
		resp, err = h.handleSearch(r.Context(), req, history, userID, prov, timeoutSec)
	}

	if err != nil {
		slog.Error("chat: handler failed", "mode", mode, "error", err)
		errStr := err.Error()
		if strings.Contains(errStr, "429") || strings.Contains(strings.ToLower(errStr), "rate limit") {
			json.NewEncoder(w).Encode(chatResponse{
				Answer: "I'm temporarily rate-limited. Please try again in a minute.",
			})
			return
		}
		http.Error(w, `{"error":"helper service error: `+errStr+`"}`, http.StatusServiceUnavailable)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

func (h *ChatHandler) handleIntake(ctx context.Context, mode string, req chatRequest, history []*pb.Message, userID, prov string, timeout time.Duration) (chatResponse, error) {
	var sp string
	if mode == "worker_intake" {
		sp = h.getWorkerProfilePrompt()
	} else {
		sp = h.getClientProfilePrompt()
	}
	if sp == "" {
		sp = "Profile prompt not configured. Please contact an administrator."
	}

	// Language enforcement: append instruction so LLM responds in the user's UI language
	if req.Lang == "es" {
		sp += "\n\nIMPORTANTE: Responde SIEMPRE en español al usuario. Todas tus respuestas deben ser en español."
	} else if req.Lang == "en" {
		sp += "\n\nIMPORTANT: Always respond in English to the user. All your responses must be in English."
	}
	slog.Info("chat: intake", "mode", mode, "lang", req.Lang, "prompt_len", len(sp))

	ctx2, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	resp, err := h.client.Ask(ctx2, &pb.AskRequest{
		Question:          req.Message,
		History:           history,
		SystemPrompt:      sp,
		LlmProvider:       prov,
		SkipRoleDetection: true,
	})
	elapsed := time.Since(start)
	if err != nil {
		slog.Error("chat: intake gRPC failed", "mode", mode, "error", err, "duration_ms", elapsed.Milliseconds())
		return chatResponse{}, err
	}

	answer, fields := parseFieldsFromAnswer(resp.Answer)
	if fields != nil {
		slog.Info("chat: extracted fields", "mode", mode, "fields_len", len(fields))
	} else {
		slog.Debug("chat: no [FIELDS] block found", "mode", mode)
	}

	var convType string
	if mode == "worker_intake" {
		convType = "worker"
	} else {
		convType = "client"
	}

	if userID == "" {
		slog.Warn("chat: cannot save conversation — user not authenticated", "mode", mode)
	}

	respConvID := req.ConversationID
	if userID != "" {
		newID, saveErr := h.saveConversation(userID, req.ConversationID, convType, req.Message, answer, fields, nil)
		if saveErr != nil {
			slog.Warn("chat: failed to save conversation", "error", saveErr)
		} else {
			respConvID = newID
		}
	}

	if fields != nil && userID != "" {
		if mode == "worker_intake" {
			h.upsertWorkerProfile(userID, fields)
		} else {
			h.upsertClientProfile(userID, fields)
		}
	}

	slog.Info("chat: intake response", "mode", mode, "answer_len", len(answer), "duration_ms", elapsed.Milliseconds(), "conv_id", respConvID)

	return chatResponse{
		Answer:         answer,
		ConversationID: respConvID,
		DetectedFields: fields,
	}, nil
}

func (h *ChatHandler) handleSearch(ctx context.Context, req chatRequest, history []*pb.Message, userID, prov string, timeout time.Duration) (chatResponse, error) {
	var clientCity string
	if userID != "" {
		var cp core.ClientProfile
		if err := h.db.Where("user_id = ?", userID).First(&cp).Error; err == nil && cp.City != "" {
			clientCity = cp.City
			slog.Debug("chat: loaded client city from profile", "city", clientCity)
		}
	}

	searchSP := h.getFindTraderSearchPrompt()
	if searchSP == "" {
		slog.Warn("chat: no find_trader_search_prompt configured")
		return chatResponse{
			Answer: "Search is not configured yet. Please contact an administrator.",
		}, nil
	}

	if clientCity != "" {
		searchSP = fmt.Sprintf("The client is based in %s. Use this as the default city unless they specify a different one.\n\n%s", clientCity, searchSP)
	}

	// Language enforcement for search pass 1
	if req.Lang == "es" {
		searchSP += "\n\nIMPORTANTE: Responde SIEMPRE en español al usuario."
	} else if req.Lang == "en" {
		searchSP += "\n\nIMPORTANT: Always respond in English to the user."
	}

	// Pass 1: extract search params
	ctx1, cancel1 := context.WithTimeout(ctx, timeout)
	defer cancel1()

	start1 := time.Now()
	resp1, err := h.client.Ask(ctx1, &pb.AskRequest{
		Question:          req.Message,
		History:           history,
		SystemPrompt:      searchSP,
		LlmProvider:       prov,
		SkipRoleDetection: true,
	})
	elapsed1 := time.Since(start1)
	if err != nil {
		slog.Error("chat: search pass 1 gRPC failed", "error", err, "duration_ms", elapsed1.Milliseconds())
		return chatResponse{}, err
	}

	pass1Clean, searchParams := parseSearchFromAnswer(resp1.Answer)

	// If the LLM didn't produce search params, it treated this as a greeting
	// or non-search message — return the conversational response directly
	// instead of running Pass 2 which would incorrectly say "no matches found".
	if searchParams == nil {
		slog.Info("chat: search pass 1 — no search params, returning conversational response", "answer_len", len(pass1Clean))
		respConvID := req.ConversationID
		if userID != "" {
			newID, saveErr := h.saveConversation(userID, req.ConversationID, "client-find", req.Message, pass1Clean, nil, nil)
			if saveErr != nil {
				slog.Warn("chat: failed to save search conversation", "error", saveErr)
			} else {
				respConvID = newID
			}
		}
		return chatResponse{
			Answer:         pass1Clean,
			ConversationID: respConvID,
		}, nil
	}

	var workerCards []findTraderWorkerCard
	if searchParams != nil {
		var rawMap map[string]interface{}
		if err := json.Unmarshal(searchParams, &rawMap); err == nil {
			profession, _ := rawString(rawMap, "profession")
			city, _ := rawString(rawMap, "city")
			emergency, _ := rawBool(rawMap, "emergency")
			freeEstimate, _ := rawBool(rawMap, "free_estimate")
			insured, _ := rawBool(rawMap, "insured")

			if city == "" && clientCity != "" {
				city = clientCity
			}

			slog.Info("chat: searching workers", "profession", profession, "city", city, "emergency", emergency, "free_estimate", freeEstimate, "insured", insured)

			workers, dbErr := h.searchWorkers(profession, city, emergency, freeEstimate, insured)
			if dbErr != nil {
				slog.Error("chat: DB query failed", "error", dbErr)
			}

			workerCards = make([]findTraderWorkerCard, 0, len(workers))
			for _, w := range workers {
				var certs []string
				json.Unmarshal([]byte(w.Certifications), &certs)
				if certs == nil {
					certs = []string{}
				}
				workerCards = append(workerCards, findTraderWorkerCard{
					ID:               w.ID,
					Profession:       w.Profession,
					BusinessName:     w.BusinessName,
					Bio:              w.Bio,
					City:             w.City,
					Phone:            w.Phone,
					HourlyRate:       w.HourlyRate,
					FreeEstimate:     w.FreeEstimate,
					YearsExperience:  w.YearsExperience,
					Certifications:   certs,
					HasInsurance:     w.HasInsurance,
					EmergencyService: w.EmergencyService,
				})
			}
		}
	}

	// Pass 2: present results
	presentationSP := h.getFindTraderPresentationPrompt()
	if presentationSP == "" {
		presentationSP = "Presentation prompt not configured. Please contact an administrator."
	}

	// Language enforcement for search pass 2
	if req.Lang == "es" {
		presentationSP += "\n\nIMPORTANTE: Responde SIEMPRE en español al usuario. Presenta los resultados en español."
	} else if req.Lang == "en" {
		presentationSP += "\n\nIMPORTANT: Always respond in English to the user. Present results in English."
	}

	var pass2Question string
	if len(workerCards) == 0 {
		pass2Question = "No workers matched the search criteria. Let the user know empathetically and suggest they broaden their search."
	} else {
		var sb strings.Builder
		sb.WriteString("Here are the matching workers:\n")
		for i, w := range workerCards {
			sb.WriteString(fmt.Sprintf("%d. %s - %s in %s, €%.0f/hr, %d years experience",
				i+1, w.BusinessName, w.Profession, w.City, w.HourlyRate, w.YearsExperience))
			if w.Phone != "" {
				sb.WriteString(fmt.Sprintf(", phone: %s", w.Phone))
			}
			if w.Bio != "" {
				sb.WriteString(fmt.Sprintf(", bio: %s", w.Bio))
			}
			if len(w.Certifications) > 0 {
				sb.WriteString(fmt.Sprintf(", certifications: %s", strings.Join(w.Certifications, ", ")))
			}
			if w.HasInsurance {
				sb.WriteString(", insured")
			}
			if w.EmergencyService {
				sb.WriteString(", emergency service")
			}
			if w.FreeEstimate {
				sb.WriteString(", free estimates")
			}
			sb.WriteString("\n")
		}
		pass2Question = sb.String()
	}

	slog.Info("chat: search pass 2 — presenting results", "worker_count", len(workerCards))

	ctx2, cancel2 := context.WithTimeout(ctx, timeout)
	defer cancel2()

	start2 := time.Now()
	resp2, err := h.client.Ask(ctx2, &pb.AskRequest{
		Question:          pass2Question,
		SystemPrompt:      presentationSP,
		LlmProvider:       prov,
		SkipRoleDetection: true,
	})
	elapsed2 := time.Since(start2)
	if err != nil {
		slog.Error("chat: search pass 2 gRPC failed", "error", err, "duration_ms", elapsed2.Milliseconds())
		return chatResponse{}, err
	}

	finalAnswer := resp2.Answer
	slog.Info("chat: search complete", "answer_len", len(finalAnswer), "worker_count", len(workerCards), "pass1_ms", elapsed1.Milliseconds(), "pass2_ms", elapsed2.Milliseconds())

	if userID == "" {
		slog.Warn("chat: cannot save search conversation — user not authenticated", "mode", "search")
	}

	respConvID := req.ConversationID
	if userID != "" {
		newID, saveErr := h.saveConversation(userID, req.ConversationID, "client-find", req.Message, finalAnswer, nil, nil)
		if saveErr != nil {
			slog.Warn("chat: failed to save search conversation", "error", saveErr)
		} else {
			respConvID = newID
		}
	}

	return chatResponse{
		Answer:         finalAnswer,
		ConversationID: respConvID,
		Workers:        workerCards,
	}, nil
}

func (h *ChatHandler) upsertWorkerProfile(userID string, fields json.RawMessage) {
	var rawMap map[string]interface{}
	if err := json.Unmarshal(fields, &rawMap); err != nil {
		slog.Warn("chat: failed to parse worker fields JSON", "error", err)
		return
	}

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
			slog.Warn("chat: failed to save worker profile", "error", err)
		} else {
			slog.Info("chat: worker profile saved", "user_id", userID, "profession", wp.Profession)
		}
	} else {
		if err := h.db.Create(&wp).Error; err != nil {
			slog.Warn("chat: failed to create worker profile", "error", err)
		} else {
			slog.Info("chat: worker profile created", "user_id", userID, "profession", wp.Profession)
		}
	}
}

func (h *ChatHandler) upsertClientProfile(userID string, fields json.RawMessage) {
	var rawMap map[string]interface{}
	if err := json.Unmarshal(fields, &rawMap); err != nil {
		slog.Warn("chat: failed to parse client fields JSON", "error", err)
		return
	}

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
			slog.Warn("chat: failed to save client profile", "error", err)
		} else {
			slog.Info("chat: client profile saved", "user_id", userID, "full_name", cp.FullName)
		}
	} else {
		if err := h.db.Create(&cp).Error; err != nil {
			slog.Warn("chat: failed to create client profile", "error", err)
		} else {
			slog.Info("chat: client profile created", "user_id", userID, "full_name", cp.FullName)
		}
	}
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

func (h *ChatHandler) searchWorkers(profession, city string, emergency, freeEstimate, insured bool) ([]core.WorkerProfile, error) {
	query := h.db.Model(&core.WorkerProfile{}).Where("profession ILIKE ?", "%"+profession+"%")

	if city != "" {
		query = query.Where("city ILIKE ?", "%"+city+"%")
	}
	if emergency {
		query = query.Where("emergency_service = true")
	}
	if freeEstimate {
		query = query.Where("free_estimate = true")
	}
	if insured {
		query = query.Where("has_insurance = true")
	}

	if city != "" {
		query = query.Order(gorm.Expr("CASE WHEN LOWER(city) = LOWER(?) THEN 0 ELSE 1 END, created_at DESC", city))
	} else {
		query = query.Order("created_at DESC")
	}

	query = query.Limit(50)

	var workers []core.WorkerProfile
	if err := query.Find(&workers).Error; err != nil {
		return nil, err
	}
	return workers, nil
}

func parseSearchFromAnswer(answer string) (string, json.RawMessage) {
	const openTag = "[SEARCH]"
	const closeTag = "[/SEARCH]"

	lastOpen := strings.LastIndex(answer, openTag)
	if lastOpen < 0 {
		return answer, nil
	}
	afterOpen := answer[lastOpen+len(openTag):]
	closeIdx := strings.Index(afterOpen, closeTag)
	if closeIdx < 0 {
		return answer, nil
	}
	raw := strings.TrimSpace(afterOpen[:closeIdx])
	// Handle empty [SEARCH][/SEARCH] — strip the tags, return no search params
	if raw == "" {
		cleaned := strings.TrimSpace(answer[:lastOpen] + afterOpen[closeIdx+len(closeTag):])
		return cleaned, nil
	}
	var dummy interface{}
	if err := json.Unmarshal([]byte(raw), &dummy); err != nil {
		slog.Warn("chat: [SEARCH] content is not valid JSON", "raw", raw[:min(len(raw), 100)], "error", err)
		return answer, nil
	}
	cleaned := strings.TrimSpace(answer[:lastOpen] + afterOpen[closeIdx+len(closeTag):])
	return cleaned, json.RawMessage(raw)
}

func parseFieldsFromAnswer(answer string) (string, json.RawMessage) {
	const openTag = "[FIELDS]"
	const closeTag = "[/FIELDS]"

	lastOpen := strings.LastIndex(answer, openTag)
	if lastOpen < 0 {
		return answer, nil
	}
	afterOpen := answer[lastOpen+len(openTag):]
	closeIdx := strings.Index(afterOpen, closeTag)
	if closeIdx < 0 {
		return answer, nil
	}
	raw := strings.TrimSpace(afterOpen[:closeIdx])
	// Handle empty [FIELDS][/FIELDS] — strip the tags, return no fields
	if raw == "" {
		cleaned := strings.TrimSpace(answer[:lastOpen] + afterOpen[closeIdx+len(closeTag):])
		return cleaned, nil
	}
	var dummy interface{}
	if err := json.Unmarshal([]byte(raw), &dummy); err != nil {
		slog.Warn("chat: [FIELDS] content is not valid JSON", "raw", raw[:min(len(raw), 100)], "error", err)
		return answer, nil
	}
	cleaned := strings.TrimSpace(answer[:lastOpen] + afterOpen[closeIdx+len(closeTag):])
	return cleaned, json.RawMessage(raw)
}

// resolveUserID extracts the user ID from the better-auth session cookie.
// Returns empty string only if no session cookie is present at all.
// If a cookie exists but the session can't be resolved, logs a warning.
func (h *ChatHandler) resolveUserID(r *http.Request) string {
	if userID := h.resolveUserIDViaAuth(r); userID != "" {
		return userID
	}
	slog.Debug("resolveUserID: auth service failed, falling back to DB lookup")
	cookie, ok := sessionCookie(r)
	if !ok {
		slog.Warn("resolveUserID: no supported session cookie found")
		return ""
	}
	token := rawSessionToken(cookie)
	if token == "" {
		slog.Warn("resolveUserID: empty token from cookie", "cookie_name", cookie.Name)
		return ""
	}
	type dbSession struct {
		UserID string `gorm:"column:userId"`
	}
	var s dbSession
	err := h.db.Table("\"session\"").Where("token = ? AND \"expiresAt\" > NOW()", token).First(&s).Error
	if err != nil {
		slog.Warn("resolveUserID: session not found in DB")
		return ""
	}
	slog.Info("resolveUserID: found user via DB", "userID", s.UserID)
	return s.UserID
}

func (h *ChatHandler) resolveUserIDViaAuth(r *http.Request) string {
	authReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, h.authURL+"/api/auth/user-id", nil)
	if err != nil {
		slog.Warn("resolveUserIDViaAuth: failed to create request", "error", err)
		return ""
	}
	addSessionCookie(authReq, r)
	client := &http.Client{Timeout: 3 * time.Second}
	authResp, err := client.Do(authReq)
	if err != nil {
		slog.Warn("resolveUserIDViaAuth: auth service unreachable", "error", err)
		return ""
	}
	defer authResp.Body.Close()
	if authResp.StatusCode != http.StatusOK {
		slog.Warn("resolveUserIDViaAuth: auth service returned non-OK", "status", authResp.StatusCode)
		return ""
	}
	var result struct {
		UserID string `json:"userId"`
	}
	if err := json.NewDecoder(authResp.Body).Decode(&result); err != nil {
		slog.Warn("resolveUserIDViaAuth: failed to decode response", "error", err)
		return ""
	}
	slog.Info("resolveUserIDViaAuth: resolved user", "userID", result.UserID)
	return result.UserID
}

// saveConversation persists a pair of messages (user + assistant) to the messages table.
func (h *ChatHandler) saveConversation(userID, convID, convType string, reqMsg, respMsg string, fields json.RawMessage, metadata map[string]interface{}) (string, error) {
	if convID != "" {
		var existing core.Conversation
		if err := h.db.First(&existing, "id = ? AND user_id = ?", convID, userID).Error; err != nil {
			slog.Warn("saveConversation: conversation not found or not owned, creating new", "convID", convID, "userID", userID, "error", err)
			convID = ""
		} else {
			for _, msg := range []core.Message{
				{ConversationID: convID, Role: "user", Content: reqMsg},
				{ConversationID: convID, Role: "assistant", Content: respMsg},
			} {
				if err := h.db.Create(&msg).Error; err != nil {
					return "", err
				}
			}

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
