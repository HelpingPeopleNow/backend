package core

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── ParseFields ─────────────────────────────────────────────────────

func TestParseFieldsValid(t *testing.T) {
	input := "Hello!\n[FIELDS]{\"profession\":\"plumber\",\"city\":\"Madrid\"}[/FIELDS]"
	text, raw := ParseFields(input)
	require.NotNil(t, raw)
	assert.Contains(t, string(raw), "plumber")
	// cleaned text has tag removed
	assert.Contains(t, text, "Hello!")
}

func TestParseFieldsMultipleBlocks(t *testing.T) {
	input := "[FIELDS]{\"a\":\"1\"}[/FIELDS]\nextra\n[FIELDS]{\"b\":\"2\"}[/FIELDS]"
	_, raw := ParseFields(input)
	require.NotNil(t, raw)
	assert.Contains(t, string(raw), "b")
	assert.NotContains(t, string(raw), "\"a\"")
}

func TestParseFieldsInvalidJSON(t *testing.T) {
	input := "[FIELDS]{not valid json}[/FIELDS]"
	_, raw := ParseFields(input)
	assert.Nil(t, raw)
}

func TestParseFieldsNone(t *testing.T) {
	_, raw := ParseFields("Hello! How can I help?")
	assert.Nil(t, raw)
}

func TestParseFieldsEmptyTags(t *testing.T) {
	_, raw := ParseFields("[FIELDS][/FIELDS]")
	assert.Nil(t, raw)
}

// ── ParseFieldsMap ──────────────────────────────────────────────────

func TestParseFieldsMapValid(t *testing.T) {
	input := "extra text [FIELDS]{\"profession\":\"plumber\"}[/FIELDS]"
	text, fields := ParseFieldsMap(input)
	require.NotNil(t, fields)
	assert.Equal(t, "plumber", fields["profession"])
	// cleaned text has the tag removed, other text preserved
	assert.Equal(t, "extra text", text)
}

func TestParseFieldsMapNone(t *testing.T) {
	text, fields := ParseFieldsMap("Hello!")
	assert.Equal(t, "Hello!", text)
	assert.Nil(t, fields)
}

// ── ParseSearch ─────────────────────────────────────────────────────

func TestParseSearchValid(t *testing.T) {
	input := "I'll search...\n[SEARCH]{\"profession\":\"plumber\",\"city\":\"Madrid\"}[/SEARCH]"
	_, raw := ParseSearch(input)
	require.NotNil(t, raw)
	var m map[string]interface{}
	err := json.Unmarshal(raw, &m)
	require.NoError(t, err)
	assert.Equal(t, "plumber", m["profession"])
}

func TestParseSearchNone(t *testing.T) {
	_, raw := ParseSearch("Hello!")
	assert.Nil(t, raw)
}

// ── truncate ────────────────────────────────────────────────────────

func TestTruncateShort(t *testing.T) {
	assert.Equal(t, "hi", truncate("hi", 100))
}

func TestTruncateLong(t *testing.T) {
	// truncate just cuts, no "..." appended
	got := truncate("hello world", 5)
	assert.Equal(t, "hello", got)
}

func TestTruncateExact(t *testing.T) {
	got := truncate("hello", 5)
	assert.Equal(t, "hello", got)
}
