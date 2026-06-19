package core

import (
	"encoding/json"
	"log/slog"
	"strings"
)

// ParseFields extracts and parses a [FIELDS]...[/FIELDS] block from an LLM answer.
// Returns the cleaned answer and a JSON raw message containing the parsed fields.
// Returns nil for the fields if no valid block is found.
func ParseFields(answer string) (string, json.RawMessage) {
	return parseTaggedBlock(answer, "[FIELDS]", "[/FIELDS]")
}

// ParseSearch extracts and parses a [SEARCH]...[/SEARCH] block from an LLM answer.
func ParseSearch(answer string) (string, json.RawMessage) {
	return parseTaggedBlock(answer, "[SEARCH]", "[/SEARCH]")
}

func parseTaggedBlock(answer, openTag, closeTag string) (string, json.RawMessage) {
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
	if raw == "" {
		cleaned := strings.TrimSpace(answer[:lastOpen] + afterOpen[closeIdx+len(closeTag):])
		return cleaned, nil
	}
	var dummy interface{}
	if err := json.Unmarshal([]byte(raw), &dummy); err != nil {
		slog.Warn("parseTaggedBlock: content is not valid JSON",
			"open_tag", openTag,
			"raw", truncate(raw, 100),
			"error", err)
		return answer, nil
	}
	cleaned := strings.TrimSpace(answer[:lastOpen] + afterOpen[closeIdx+len(closeTag):])
	return cleaned, json.RawMessage(raw)
}

// ParseFieldsMap returns the parsed fields as a map for easier handling.
// Returns an empty map if no fields block is present.
func ParseFieldsMap(answer string) (string, map[string]interface{}) {
	cleaned, raw := ParseFields(answer)
	if raw == nil {
		return cleaned, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return cleaned, nil
	}
	return cleaned, m
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
