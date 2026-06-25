package core

import (
	"encoding/json"
	"testing"
)

// ── rawString ───────────────────────────────────────────────────────

func TestRawStringPresent(t *testing.T) {
	m := map[string]interface{}{"name": "Alice"}
	v, ok := rawString(m, "name")
	if !ok || v != "Alice" {
		t.Fatalf("expected Alice, got %q (ok=%v)", v, ok)
	}
}

func TestRawStringMissing(t *testing.T) {
	m := map[string]interface{}{"name": "Alice"}
	_, ok := rawString(m, "age")
	if ok {
		t.Fatal("expected ok=false for missing key")
	}
}

func TestRawStringNil(t *testing.T) {
	m := map[string]interface{}{"name": nil}
	v, ok := rawString(m, "name")
	if !ok {
		t.Fatal("expected ok=true for nil value")
	}
	if v != "" {
		t.Fatalf("expected empty string for nil, got %q", v)
	}
}

func TestRawStringWrongType(t *testing.T) {
	m := map[string]interface{}{"name": 42}
	_, ok := rawString(m, "name")
	if ok {
		t.Fatal("expected ok=false for non-string type")
	}
}

// ── rawFloat ────────────────────────────────────────────────────────

func TestRawFloatPresent(t *testing.T) {
	m := map[string]interface{}{"rate": 25.5}
	v, ok := rawFloat(m, "rate")
	if !ok || v != 25.5 {
		t.Fatalf("expected 25.5, got %v (ok=%v)", v, ok)
	}
}

func TestRawFloatMissing(t *testing.T) {
	m := map[string]interface{}{}
	_, ok := rawFloat(m, "rate")
	if ok {
		t.Fatal("expected ok=false")
	}
}

func TestRawFloatNil(t *testing.T) {
	m := map[string]interface{}{"rate": nil}
	v, ok := rawFloat(m, "rate")
	if !ok || v != 0 {
		t.Fatalf("expected 0/true, got %v/%v", v, ok)
	}
}

// ── rawInt ──────────────────────────────────────────────────────────

func TestRawIntFromFloat(t *testing.T) {
	// JSON numbers decode as float64
	m := map[string]interface{}{"years": float64(5)}
	v, ok := rawInt(m, "years")
	if !ok || v != 5 {
		t.Fatalf("expected 5, got %v (ok=%v)", v, ok)
	}
}

func TestRawIntFromString(t *testing.T) {
	m := map[string]interface{}{"years": "10"}
	v, ok := rawInt(m, "years")
	if !ok || v != 10 {
		t.Fatalf("expected 10, got %v (ok=%v)", v, ok)
	}
}

func TestRawIntInvalidString(t *testing.T) {
	m := map[string]interface{}{"years": "abc"}
	_, ok := rawInt(m, "years")
	if ok {
		t.Fatal("expected ok=false for non-numeric string")
	}
}

func TestRawIntMissing(t *testing.T) {
	m := map[string]interface{}{}
	_, ok := rawInt(m, "years")
	if ok {
		t.Fatal("expected ok=false")
	}
}

// ── rawBool ─────────────────────────────────────────────────────────

func TestRawBoolTrue(t *testing.T) {
	m := map[string]interface{}{"flag": true}
	v, ok := rawBool(m, "flag")
	if !ok || !v {
		t.Fatalf("expected true/true, got %v/%v", v, ok)
	}
}

func TestRawBoolFalse(t *testing.T) {
	m := map[string]interface{}{"flag": false}
	v, ok := rawBool(m, "flag")
	if !ok || v {
		t.Fatalf("expected false/true, got %v/%v", v, ok)
	}
}

func TestRawBoolStringTrue(t *testing.T) {
	// rawBool uses strings.EqualFold(s, "true") || s == "1"
	for _, s := range []string{"true", "True", "TRUE", "1"} {
		m := map[string]interface{}{"flag": s}
		v, ok := rawBool(m, "flag")
		if !ok || !v {
			t.Fatalf("string %q: expected true/true, got %v/%v", s, v, ok)
		}
	}
}

func TestRawBoolStringFalse(t *testing.T) {
	// rawBool uses strings.EqualFold(s, "true") || s == "1" → anything else is false
	for _, s := range []string{"false", "False", "FALSE", "0", "no", "NO", "No", "maybe", "yes"} {
		m := map[string]interface{}{"flag": s}
		v, ok := rawBool(m, "flag")
		if !ok || v {
			t.Fatalf("string %q: expected false/true, got %v/%v", s, v, ok)
		}
	}
}

func TestRawBoolFromFloat(t *testing.T) {
	m := map[string]interface{}{"flag": float64(1)}
	v, ok := rawBool(m, "flag")
	if !ok || !v {
		t.Fatalf("float64(1): expected true/true, got %v/%v", v, ok)
	}

	m2 := map[string]interface{}{"flag": float64(0)}
	v2, ok2 := rawBool(m2, "flag")
	if !ok2 || v2 {
		t.Fatalf("float64(0): expected false/true, got %v/%v", v2, ok2)
	}
}

func TestRawBoolStringReturnsOk(t *testing.T) {
	// rawBool returns (false, true) for ANY string that isn't "true"/"1"
	// — there is no "invalid string" case; all strings get ok=true.
	for _, s := range []string{"maybe", "abc", "yes", "nah"} {
		m := map[string]interface{}{"flag": s}
		v, ok := rawBool(m, "flag")
		if !ok {
			t.Fatalf("string %q: expected ok=true, got ok=false", s)
		}
		if v {
			t.Fatalf("string %q: expected value=false, got true", s)
		}
	}
}

// ── mergeJSONArray ──────────────────────────────────────────────────

func TestMergeJSONArrayFromExisting(t *testing.T) {
	fields := map[string]interface{}{}
	existing := `["GAS SAFE","NICEIC"]`
	result := mergeJSONArray(fields, "certifications", existing)
	if result != existing {
		t.Fatalf("expected existing returned when key absent, got %q", result)
	}
}

func TestMergeJSONArrayFromArray(t *testing.T) {
	fields := map[string]interface{}{
		"certifications": []interface{}{"GAS SAFE", "City & Guilds"},
	}
	result := mergeJSONArray(fields, "certifications", "")
	var arr []string
	if err := json.Unmarshal([]byte(result), &arr); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if len(arr) != 2 || arr[0] != "GAS SAFE" || arr[1] != "City & Guilds" {
		t.Fatalf("unexpected result: %v", arr)
	}
}

func TestMergeJSONArrayFromSingleString(t *testing.T) {
	fields := map[string]interface{}{
		"languages": "Spanish",
	}
	result := mergeJSONArray(fields, "languages", "")
	var arr []string
	if err := json.Unmarshal([]byte(result), &arr); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if len(arr) != 1 || arr[0] != "Spanish" {
		t.Fatalf("unexpected result: %v", arr)
	}
}

func TestMergeJSONArrayNullClears(t *testing.T) {
	fields := map[string]interface{}{
		"certifications": nil,
	}
	result := mergeJSONArray(fields, "certifications", `["old"]`)
	if result != "" {
		t.Fatalf("expected empty string for null, got %q", result)
	}
}

func TestMergeJSONArrayInvalidType(t *testing.T) {
	fields := map[string]interface{}{
		"certifications": 42,
	}
	result := mergeJSONArray(fields, "certifications", `["existing"]`)
	if result != `["existing"]` {
		t.Fatalf("expected existing returned for invalid type, got %q", result)
	}
}

// ── mergeSocialLinks ────────────────────────────────────────────────

func TestMergeSocialLinksFromNamedFields(t *testing.T) {
	fields := map[string]interface{}{
		"instagram": "https://instagram.com/mybiz",
		"facebook":  "https://facebook.com/mybiz",
	}
	result := mergeSocialLinks(fields, "")
	var links []map[string]string
	if err := json.Unmarshal([]byte(result), &links); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
}

func TestMergeSocialLinksDeduplication(t *testing.T) {
	existing := `[{"platform":"Instagram","url":"https://instagram.com/old"}]`
	fields := map[string]interface{}{
		"instagram": "https://instagram.com/new",
	}
	result := mergeSocialLinks(fields, existing)
	var links []map[string]string
	if err := json.Unmarshal([]byte(result), &links); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link (deduped), got %d", len(links))
	}
	if links[0]["url"] != "https://instagram.com/new" {
		t.Fatalf("expected updated URL, got %q", links[0]["url"])
	}
}

func TestMergeSocialLinksNoSocialKeys(t *testing.T) {
	fields := map[string]interface{}{
		"profession": "plumber",
	}
	existing := `[{"platform":"Instagram","url":"https://instagram.com/old"}]`
	result := mergeSocialLinks(fields, existing)
	if result != existing {
		t.Fatalf("expected existing returned when no social keys, got %q", result)
	}
}

func TestMergeSocialLinksFromSocialLinksArray(t *testing.T) {
	fields := map[string]interface{}{
		"social_links": []interface{}{
			map[string]interface{}{"platform": "TikTok", "url": "https://tiktok.com/@me"},
		},
	}
	result := mergeSocialLinks(fields, "")
	var links []map[string]string
	if err := json.Unmarshal([]byte(result), &links); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0]["platform"] != "TikTok" {
		t.Fatalf("expected TikTok, got %q", links[0]["platform"])
	}
}

func TestMergeSocialLinksNullClears(t *testing.T) {
	// null social_links clears when there are NO existing links
	fields := map[string]interface{}{
		"social_links": nil,
	}
	result := mergeSocialLinks(fields, "")
	if result != "" {
		t.Fatalf("expected empty for null social_links with no existing, got %q", result)
	}
}

func TestMergeSocialLinksNullPreservesExisting(t *testing.T) {
	// null social_links preserves existing links (they were loaded before the null check)
	fields := map[string]interface{}{
		"social_links": nil,
	}
	existing := `[{"platform":"Instagram","url":"https://instagram.com/old"}]`
	result := mergeSocialLinks(fields, existing)
	if result != existing {
		t.Fatalf("expected existing preserved, got %q", result)
	}
}

// ── fmtSummary ──────────────────────────────────────────────────────

func TestFmtSummaryWithYears(t *testing.T) {
	result := fmtSummary(1, "Bob's Plumbing", "plumber", "Madrid", "€25/hr", 5)
	expected := "1. Bob's Plumbing - plumber in Madrid, €25/hr, 5 years experience"
	if result != expected {
		t.Fatalf("unexpected fmtSummary:\n got: %q\nwant: %q", result, expected)
	}
}
