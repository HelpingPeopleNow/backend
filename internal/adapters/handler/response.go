package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/HelpingPeopleNow/backend/internal/ports"
)

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func handleLLMError(w http.ResponseWriter, err error) {
	errStr := err.Error()
	if strings.Contains(errStr, "RATE_LIMIT") || strings.Contains(errStr, "429") || strings.Contains(strings.ToLower(errStr), "rate limit") {
		writeJSON(w, http.StatusOK, map[string]string{
			"answer": "I'm temporarily rate-limited. Please try again in a minute.",
		})
		return
	}
	writeError(w, http.StatusServiceUnavailable, "helper service error: "+errStr)
	slog.Error("handler: llm error", "error", err)
}

func parseIntParam(r *http.Request, key string, fallback int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func convertHistory(messages []chatMessage) []ports.MessagePair {
	pairs := make([]ports.MessagePair, len(messages))
	for i, m := range messages {
		pairs[i] = ports.MessagePair{Role: m.Role, Content: m.Content}
	}
	return pairs
}
