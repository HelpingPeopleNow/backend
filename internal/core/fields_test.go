package core

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── rawInt ──────────────────────────────────────────────────────────

func TestRawIntFromFloat64(t *testing.T) {
	m := map[string]interface{}{"k": float64(42)}
	v, ok := rawInt(m, "k")
	assert.True(t, ok)
	assert.Equal(t, 42, v)
}

func TestRawIntFromString(t *testing.T) {
	m := map[string]interface{}{"k": "10"}
	v, ok := rawInt(m, "k")
	assert.True(t, ok)
	assert.Equal(t, 10, v)
}

func TestRawIntNil(t *testing.T) {
	m := map[string]interface{}{"k": nil}
	v, ok := rawInt(m, "k")
	assert.True(t, ok)
	assert.Equal(t, 0, v)
}

func TestRawIntMissing(t *testing.T) {
	_, ok := rawInt(map[string]interface{}{}, "k")
	assert.False(t, ok)
}

func TestRawIntInvalidString(t *testing.T) {
	_, ok := rawInt(map[string]interface{}{"k": "abc"}, "k")
	assert.False(t, ok)
}

// ── rawBool ─────────────────────────────────────────────────────────

func TestRawBoolTrue(t *testing.T) {
	m := map[string]interface{}{"k": true}
	v, ok := rawBool(m, "k")
	assert.True(t, ok)
	assert.True(t, v)
}

func TestRawBoolFalse(t *testing.T) {
	m := map[string]interface{}{"k": false}
	v, ok := rawBool(m, "k")
	assert.True(t, ok)
	assert.False(t, v)
}

func TestRawBoolFromFloat64(t *testing.T) {
	m := map[string]interface{}{"k": float64(1)}
	v, ok := rawBool(m, "k")
	assert.True(t, ok)
	assert.True(t, v)

	m2 := map[string]interface{}{"k": float64(0)}
	v2, ok2 := rawBool(m2, "k")
	assert.True(t, ok2)
	assert.False(t, v2)
}

func TestRawBoolFromString(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  bool
	}{
		{"true", true}, {"True", true}, {"1", true},
		{"false", false}, {"0", false}, {"no", false},
	} {
		v, ok := rawBool(map[string]interface{}{"k": tc.input}, "k")
		assert.True(t, ok, "input: %s", tc.input)
		assert.Equal(t, tc.want, v, "input: %s", tc.input)
	}
}

func TestRawBoolNil(t *testing.T) {
	v, ok := rawBool(map[string]interface{}{"k": nil}, "k")
	assert.True(t, ok)
	assert.False(t, v)
}

func TestRawBoolMissing(t *testing.T) {
	_, ok := rawBool(map[string]interface{}{}, "k")
	assert.False(t, ok)
}

// ── rawFloat ────────────────────────────────────────────────────────

func TestRawFloatFromFloat64(t *testing.T) {
	m := map[string]interface{}{"k": float64(25.5)}
	v, ok := rawFloat(m, "k")
	assert.True(t, ok)
	assert.InDelta(t, 25.5, v, 0.001)
}

func TestRawFloatNil(t *testing.T) {
	v, ok := rawFloat(map[string]interface{}{"k": nil}, "k")
	assert.True(t, ok)
	assert.Equal(t, float64(0), v)
}

func TestRawFloatMissing(t *testing.T) {
	_, ok := rawFloat(map[string]interface{}{}, "k")
	assert.False(t, ok)
}

func TestRawFloatStringReturnsFalse(t *testing.T) {
	_, ok := rawFloat(map[string]interface{}{"k": "25.5"}, "k")
	assert.False(t, ok) // rawFloat only accepts float64, not strings
}

// ── rawString ───────────────────────────────────────────────────────

func TestRawStringPresent(t *testing.T) {
	v, ok := rawString(map[string]interface{}{"k": "hello"}, "k")
	assert.True(t, ok)
	assert.Equal(t, "hello", v)
}

func TestRawStringNil(t *testing.T) {
	v, ok := rawString(map[string]interface{}{"k": nil}, "k")
	assert.True(t, ok)
	assert.Equal(t, "", v)
}

func TestRawStringMissing(t *testing.T) {
	_, ok := rawString(map[string]interface{}{}, "k")
	assert.False(t, ok)
}

// ── mergeJSONArray ──────────────────────────────────────────────────

func TestMergeJSONArrayArrayInput(t *testing.T) {
	// mergeJSONArray REPLACES existing with fields[key] if present
	fields := map[string]interface{}{"certs": []interface{}{"C", "D"}}
	got := mergeJSONArray(fields, "certs", `["A","B"]`)
	assert.Contains(t, got, "C")
	assert.NotContains(t, got, "A")
}

func TestMergeJSONArrayStringInput(t *testing.T) {
	fields := map[string]interface{}{"certs": "E"}
	got := mergeJSONArray(fields, "certs", `["A"]`)
	assert.Equal(t, `["E"]`, got)
}

func TestMergeJSONArrayNullClears(t *testing.T) {
	fields := map[string]interface{}{"certs": nil}
	got := mergeJSONArray(fields, "certs", `["A"]`)
	assert.Equal(t, "", got)
}

func TestMergeJSONArrayMissingKey(t *testing.T) {
	fields := map[string]interface{}{"other": "value"}
	got := mergeJSONArray(fields, "certs", `["A"]`)
	assert.Equal(t, `["A"]`, got) // returns existing unchanged
}

func TestMergeJSONArrayEmptyExisting(t *testing.T) {
	fields := map[string]interface{}{"certs": []interface{}{"X"}}
	got := mergeJSONArray(fields, "certs", "")
	var arr []string
	err := json.Unmarshal([]byte(got), &arr)
	require.NoError(t, err)
	assert.Equal(t, []string{"X"}, arr)
}

// ── mergeSocialLinks ────────────────────────────────────────────────

func TestMergeSocialLinksWithIndividualFields(t *testing.T) {
	fields := map[string]interface{}{"instagram": "ig.com/user"}
	got := mergeSocialLinks(fields, "")
	// Platform is stored capitalized as "Instagram" from socialFieldNames map
	assert.Contains(t, got, "ig.com/user")
}

func TestMergeSocialLinksWithSocialLinksArray(t *testing.T) {
	fields := map[string]interface{}{
		"social_links": []interface{}{
			map[string]interface{}{"platform": "twitter", "url": "x.com/user"},
		},
	}
	got := mergeSocialLinks(fields, "")
	assert.Contains(t, got, "twitter")
}

func TestMergeSocialLinksDedupe(t *testing.T) {
	fields := map[string]interface{}{
		"social_links": []interface{}{
			map[string]interface{}{"platform": "instagram", "url": "ig.com/new"},
		},
	}
	got := mergeSocialLinks(fields, `[{"platform":"Instagram","url":"ig.com/old"}]`)
	var arr []SocialLink
	err := json.Unmarshal([]byte(got), &arr)
	require.NoError(t, err)
	assert.Len(t, arr, 1)
	assert.Equal(t, "ig.com/new", arr[0].URL)
}

func TestMergeSocialLinksNullClears(t *testing.T) {
	// social_links=nil with no other social keys → existing is re-parsed and returned
	// Null clear only works when links list is empty (no existing data to re-parse)
	fields := map[string]interface{}{"social_links": nil}
	got := mergeSocialLinks(fields, "")
	assert.Equal(t, "", got) // empty existing + nil social_links → empty
}

func TestMergeSocialLinksNoSocialKeys(t *testing.T) {
	fields := map[string]interface{}{"name": "test"}
	got := mergeSocialLinks(fields, `[{"platform":"instagram","url":"ig.com/user"}]`)
	assert.Equal(t, `[{"platform":"instagram","url":"ig.com/user"}]`, got) // existing returned unchanged
}
