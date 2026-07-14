package sentiment

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var codeFenceRx = regexp.MustCompile("(?s)```(?:json)?\\n?(.*?)\\n?```")

// SentimentResult is the expected JSON shape returned by the LLM.
type SentimentResult struct {
	Score  *int16 `json:"score"`
	Reason string `json:"reason"`
}

// ParseScore extracts a 0-10 integer score and a short reason from the
// LLM response. It strips optional markdown code fences, validates the
// score range, and truncates the reason to 120 characters.
func ParseScore(raw string) (int16, string, error) {
	clean := strings.TrimSpace(raw)
	if match := codeFenceRx.FindStringSubmatch(clean); len(match) > 1 {
		clean = strings.TrimSpace(match[1])
	}

	var res SentimentResult
	if err := json.Unmarshal([]byte(clean), &res); err != nil {
		return 0, "", fmt.Errorf("sentiment: parse json: %w", err)
	}

	if res.Score == nil {
		return 0, "", fmt.Errorf("sentiment: missing score")
	}

	if *res.Score < 0 || *res.Score > 10 {
		return 0, "", fmt.Errorf("sentiment: score out of range: %d", *res.Score)
	}

	reason := strings.TrimSpace(res.Reason)
	if len([]rune(reason)) > 120 {
		reason = string([]rune(reason)[:120])
	}

	return *res.Score, reason, nil
}
