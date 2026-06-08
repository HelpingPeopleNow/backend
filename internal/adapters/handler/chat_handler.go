package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"time"
)

// ChatHandler proxies questions to the helper service and returns answers.
type ChatHandler struct{}

func NewChatHandler() *ChatHandler {
	return &ChatHandler{}
}

type historyItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Message string       `json:"message"`
	History []historyItem `json:"history,omitempty"`
}

type chatResponse struct {
	Answer string `json:"answer"`
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

	helperURL := os.Getenv("HELPER_URL")
	if helperURL == "" {
		helperURL = "http://helpingpeoplenow-helper:8082"
	}

	// Build the body with history
	helperBody := map[string]any{
		"question": req.Message,
		"history":  req.History,
	}
	body, _ := json.Marshal(helperBody)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(helperURL+"/api/v1/ask", "application/json", bytes.NewReader(body))
	if err != nil {
		http.Error(w, `{"error":"helper service unreachable"}`, http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	var helperResp struct {
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&helperResp); err != nil {
		http.Error(w, `{"error":"invalid response from helper"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(chatResponse{Answer: helperResp.Answer})
}
