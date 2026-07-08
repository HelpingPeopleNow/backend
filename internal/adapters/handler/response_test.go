package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/ports"
	"github.com/stretchr/testify/assert"
)

func TestHandleLLMErrorRateLimit(t *testing.T) {
	for _, msg := range []string{
		"RATE_LIMIT: try again",
		"error 429 too many requests",
		"rate limit exceeded",
		"Rate Limit exceeded",
	} {
		rec := httptest.NewRecorder()
		handleLLMError(rec, errors.New(msg))
		assert.Equal(t, http.StatusOK, rec.Code, "msg: %s", msg)

		var resp map[string]string
		json.NewDecoder(rec.Body).Decode(&resp)
		assert.Contains(t, resp["answer"], "rate-limited", "msg: %s", msg)
	}
}

// TestHandleLLMErrorGeneric regression-tests P1-3 (audit F7): the raw
// error message must NOT leak into the response body. Operators still see
// the full error via slog.
func TestHandleLLMErrorGeneric(t *testing.T) {
	rec := httptest.NewRecorder()
	handleLLMError(rec, errors.New("something broke"))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	assert.Equal(t, "helper service temporarily unavailable", resp["error"],
		"P1-3 audit: response body must hold a static message, not raw err.Error()")
	assert.NotContains(t, resp["error"], "something broke",
		"P1-3 audit: raw error detail must NOT appear in response body")
	assert.NotContains(t, rec.Body.String(), "something broke")
}

func TestParseIntParamValid(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/?limit=25", nil)
	assert.Equal(t, 25, parseIntParam(req, "limit", 10))
}

func TestParseIntParamMissing(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	assert.Equal(t, 10, parseIntParam(req, "limit", 10))
}

func TestParseIntParamInvalid(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/?limit=abc", nil)
	assert.Equal(t, 10, parseIntParam(req, "limit", 10))
}

func TestConvertHistory(t *testing.T) {
	messages := []chatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}
	pairs := convertHistory(messages)
	assert.Len(t, pairs, 2)
	assert.Equal(t, ports.MessagePair{Role: "user", Content: "hello"}, pairs[0])
	assert.Equal(t, ports.MessagePair{Role: "assistant", Content: "hi there"}, pairs[1])
}

func TestConvertHistoryEmpty(t *testing.T) {
	pairs := convertHistory(nil)
	assert.Empty(t, pairs)
}
