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

// handleLLMError translates an LLM/helper error into a client response
// (P1-3 audit, F7). The raw error is kept out of the response body so we
// don't leak internal detail; operators see the full error in slog.
//
// It also increments chat_llm_errors_total (provider, error_type) so a
// sustained LLM failure is visible independent of overall request volume —
// see infra/docs/OBSERVABILITY_AUDIT_REPORT.md §1.4. Previously this counter
// was defined but never incremented (dead metric).
func handleLLMError(w http.ResponseWriter, err error, provider string) {
	errStr := err.Error()
	errType := classifyLLMError(errStr)
	IncrChatLLMErrors(provider, errType)

	if errType == "rate_limit" {
		writeJSON(w, http.StatusOK, map[string]string{
			"answer": "I'm temporarily rate-limited. Please try again in a minute.",
		})
		return
	}
	writeError(w, http.StatusServiceUnavailable, "helper service temporarily unavailable")
	slog.Error("handler: llm error", "error", err, "provider", provider, "error_type", errType)
}

// classifyLLMError buckets a raw LLM/helper error string into a small,
// fixed set of label values for chat_llm_errors_total. Keeping the set
// small avoids unbounded cardinality on the error_type label.
func classifyLLMError(errStr string) string {
	lower := strings.ToLower(errStr)
	switch {
	case strings.Contains(errStr, "RATE_LIMIT") || strings.Contains(errStr, "429") || strings.Contains(lower, "rate limit"):
		return "rate_limit"
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout") || strings.Contains(lower, "context deadline"):
		return "timeout"
	case strings.Contains(lower, "unavailable") || strings.Contains(lower, "connection refused") || strings.Contains(lower, "no such host") || strings.Contains(lower, "circuit breaker"):
		return "unreachable"
	default:
		return "unknown"
	}
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
