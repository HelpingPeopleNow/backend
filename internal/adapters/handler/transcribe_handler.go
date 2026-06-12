package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"time"
)

// TranscribeHandler proxies audio to the helper's whisper endpoint.
type TranscribeHandler struct {
	helperURL string
}

type transcribeResponse struct {
	Text     string `json:"text"`
	Language string `json:"language"`
}

func NewTranscribeHandler() *TranscribeHandler {
	helperURL := os.Getenv("HELPER_TRANSCRIBE_URL")
	if helperURL == "" {
		helperURL = "http://helpingpeoplenow-helper:8085"
	}
	return &TranscribeHandler{helperURL: helperURL}
}

func (h *TranscribeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		slog.Warn("transcribe: invalid method", "method", r.Method)
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Validate session (same pattern as chat handler)
	if userID := h.resolveUserID(r); userID == "" {
		slog.Warn("transcribe: unauthorized")
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Parse multipart form (max 10MB)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		slog.Warn("transcribe: failed to parse multipart", "error", err)
		http.Error(w, `{"error":"failed to parse audio upload"}`, http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("audio")
	if err != nil {
		slog.Warn("transcribe: no audio field", "error", err)
		http.Error(w, `{"error":"no audio file provided"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	slog.Info("transcribe request", "filename", header.Filename, "size", header.Size)

	// Forward to helper's transcribe endpoint
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("audio", header.Filename)
	if err != nil {
		slog.Error("transcribe: failed to create form file", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if _, err := io.Copy(part, file); err != nil {
		slog.Error("transcribe: failed to copy audio", "error", err)
		http.Error(w, `{"error":"failed to read audio"}`, http.StatusInternalServerError)
		return
	}
	writer.Close()

	helperReq, err := http.NewRequest(http.MethodPost, h.helperURL+"/api/v1/transcribe", &body)
	if err != nil {
		slog.Error("transcribe: failed to create helper request", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	helperReq.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(helperReq)
	if err != nil {
		slog.Error("transcribe: helper request failed", "error", err)
		http.Error(w, `{"error":"transcription service unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Error("transcribe: helper returned error", "status", resp.StatusCode, "body", string(respBody))
		http.Error(w, fmt.Sprintf(`{"error":"transcription failed"}`), http.StatusBadGateway)
		return
	}

	var result transcribeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Error("transcribe: failed to decode helper response", "error", err)
		http.Error(w, `{"error":"failed to decode transcription"}`, http.StatusInternalServerError)
		return
	}

	slog.Info("transcribe done", "text_len", len(result.Text), "language", result.Language)
	json.NewEncoder(w).Encode(result)
}

// resolveUserID extracts user ID from the session cookie.
func (h *TranscribeHandler) resolveUserID(r *http.Request) string {
	cookie, ok := sessionCookie(r)
	if !ok {
		return ""
	}
	token := rawSessionToken(cookie)
	if token == "" {
		return ""
	}
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
