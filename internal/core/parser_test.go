package core

import (
	"encoding/json"
	"testing"
)

func TestParseFieldsValid(t *testing.T) {
	answer := `Hello! I'm a plumber.
[FIELDS]{"profession":"plumber","city":"Madrid"}[/FIELDS]`
	cleaned, raw := ParseFields(answer)
	if raw == nil {
		t.Fatal("expected non-nil raw fields")
	}
	if cleaned != "Hello! I'm a plumber." {
		t.Fatalf("unexpected cleaned text: %q", cleaned)
	}
}

func TestParseFieldsMultipleBlocksLastWins(t *testing.T) {
	answer := `First attempt.
[FIELDS]{"profession":"electrician"}[/FIELDS]
Second attempt.
[FIELDS]{"profession":"plumber","city":"Madrid"}[/FIELDS]`
	_, raw := ParseFields(answer)
	if raw == nil {
		t.Fatal("expected non-nil raw fields")
	}
	// LastIndex means the second block should win.
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if m["profession"] != "plumber" {
		t.Fatalf("expected last block to win, got profession=%v", m["profession"])
	}
}

func TestParseFieldsNoBlock(t *testing.T) {
	answer := "Hello, I'm a plumber."
	cleaned, raw := ParseFields(answer)
	if raw != nil {
		t.Fatal("expected nil raw fields for no block")
	}
	if cleaned != answer {
		t.Fatalf("cleaned text should equal input when no block present")
	}
}

func TestParseFieldsInvalidJSON(t *testing.T) {
	answer := `Hello!
[FIELDS]{not valid json}[/FIELDS]`
	cleaned, raw := ParseFields(answer)
	// Invalid JSON should return original answer, nil fields.
	if raw != nil {
		t.Fatal("expected nil raw fields for invalid JSON")
	}
	if cleaned != answer {
		t.Fatalf("cleaned text should equal input for invalid JSON block")
	}
}

func TestParseFieldsEmptyTags(t *testing.T) {
	answer := `Hello![FIELDS][/FIELDS]`
	cleaned, raw := ParseFields(answer)
	if raw != nil {
		t.Fatal("expected nil raw fields for empty tags")
	}
	if cleaned != "Hello!" {
		t.Fatalf("unexpected cleaned text: %q", cleaned)
	}
}

func TestParseFieldsWithTextAround(t *testing.T) {
	answer := `Sure, here's what I know:
[FIELDS]{"profession":"cleaner"}[/FIELDS]
Let me know if you need anything else!`
	cleaned, raw := ParseFields(answer)
	if raw == nil {
		t.Fatal("expected non-nil raw fields")
	}
	expected := "Sure, here's what I know:\n\nLet me know if you need anything else!"
	if cleaned != expected {
		t.Fatalf("unexpected cleaned text:\n got: %q\nwant: %q", cleaned, expected)
	}
}

func TestParseSearchValid(t *testing.T) {
	answer := `I'll search for plumbers in your area.
[SEARCH]{"profession":"plumber","city":"Madrid"}[/SEARCH]`
	cleaned, raw := ParseSearch(answer)
	if raw == nil {
		t.Fatal("expected non-nil raw search")
	}
	if cleaned != "I'll search for plumbers in your area." {
		t.Fatalf("unexpected cleaned text: %q", cleaned)
	}
}

func TestParseSearchNoBlock(t *testing.T) {
	answer := "Hello! How can I help you?"
	cleaned, raw := ParseSearch(answer)
	if raw != nil {
		t.Fatal("expected nil raw search for no block")
	}
	if cleaned != answer {
		t.Fatalf("cleaned text should equal input")
	}
}

func TestParseSearchMultipleBlocksLastWins(t *testing.T) {
	answer := `First.
[SEARCH]{"profession":"electrician"}[/SEARCH]
Updated.
[SEARCH]{"profession":"plumber","city":"Barcelona"}[/SEARCH]`
	_, raw := ParseSearch(answer)
	if raw == nil {
		t.Fatal("expected non-nil raw search")
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if m["city"] != "Barcelona" {
		t.Fatalf("expected last block to win, got city=%v", m["city"])
	}
}

func TestParseFieldsMapWithFields(t *testing.T) {
	answer := `Hi!
[FIELDS]{"profession":"plumber","phone":"555-1234"}[/FIELDS]`
	cleaned, m := ParseFieldsMap(answer)
	if m == nil {
		t.Fatal("expected non-nil map")
	}
	if m["profession"] != "plumber" {
		t.Fatalf("expected profession=plumber, got %v", m["profession"])
	}
	if m["phone"] != "555-1234" {
		t.Fatalf("expected phone=555-1234, got %v", m["phone"])
	}
	if cleaned != "Hi!" {
		t.Fatalf("unexpected cleaned text: %q", cleaned)
	}
}

func TestParseFieldsMapNoFields(t *testing.T) {
	answer := "Hello, how are you?"
	cleaned, m := ParseFieldsMap(answer)
	if m != nil {
		t.Fatalf("expected nil map for no fields, got %v", m)
	}
	if cleaned != answer {
		t.Fatalf("cleaned text should equal input")
	}
}

func TestParseFieldsMapInvalidJSON(t *testing.T) {
	answer := `[FIELDS]{bad json}[/FIELDS]`
	cleaned, m := ParseFieldsMap(answer)
	if m != nil {
		t.Fatalf("expected nil map for invalid JSON, got %v", m)
	}
	if cleaned != answer {
		t.Fatalf("cleaned text should equal input for invalid JSON")
	}
}
