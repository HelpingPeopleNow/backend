package core

import (
	"testing"
)

// Every alias in ProfessionAliases (including duplicates that already
// share canonical form) must round-trip through NormalizeProfession.
func TestNormalizeProfessionAllAliases(t *testing.T) {
	for alias, canonical := range ProfessionAliases {
		t.Run(alias, func(t *testing.T) {
			got := NormalizeProfession(alias)
			if got != canonical {
				t.Errorf("NormalizeProfession(%q) = %q, want %q", alias, got, canonical)
			}
		})
	}
}

// Casing must not affect normalization — every known alias is stored
// lowercase in ProfessionAliases and NormalizeProfession lowercases its
// input. Walks several canonical pairs and verifies the canonical name
// is returned regardless of input casing.
func TestNormalizeProfessionCaseInsensitive(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Electrician family.
		{"electricista", "Electrician"},
		{"Electricista", "Electrician"},
		{"ELECTRICISTA", "Electrician"},
		{"electrician", "Electrician"},
		{"Electrician", "Electrician"},
		{"ELECTRICIAN", "Electrician"},
		{"electric", "Electrician"},
		// Plumber family.
		{"plumber", "Plumber"},
		{"Plumber", "Plumber"},
		{"PLUMBER", "Plumber"},
		{"plomero", "Plumber"},
		{"Plomero", "Plumber"},
		{"fontanero", "Plumber"},
		// Cleaner family.
		{"cleaner", "Cleaner"},
		{"Cleaning", "Cleaner"},
		{"limpieza", "Cleaner"},
		{"limpiador", "Cleaner"},
		{"limpiadora", "Cleaner"},
		// Handyman family.
		{"handyman", "Handyman"},
		{"Handyman", "Handyman"},
		{"manitas", "Handyman"},
		// Multi-word aliases.
		{"hvac", "HVAC Technician"},
		{"HVAC", "HVAC Technician"},
		{"hvac technician", "HVAC Technician"},
		{"HVAC Technician", "HVAC Technician"},
		{"clima", "HVAC Technician"},
		{"aire acondicionado", "HVAC Technician"},
		{"Aire Acondicionado", "HVAC Technician"},
		{"handy man", "Handyman"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeProfession(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeProfession(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// Whitespace trimming is part of the contract — leading/trailing spaces
// must not change normalization. Internal spaces are preserved within
// multi-word aliases like "handy man" by the TrimSpace call.
//
// All-whitespace input is NOT in the alias map, so per the "unknown
// values are returned unchanged" contract, the input is returned as-is
// (without trimming). Documented here so future changes don't
// regress the behavior.
func TestNormalizeProfessionWhitespace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"leading_trailing_spaces_plumber", "  plumber  ", "Plumber"},
		{"tab_around_plumber", "\tplumber\t", "Plumber"},
		{"newline_around_limpiador", "\nlimpiador\n", "Cleaner"},
		{"spaces_around_electrician_unknown", "   Electrician   ", "Electrician"},
		// All-whitespace: not in the alias map, returned unchanged per
		// the "unknown values returned unchanged" contract. The function
		// does NOT collapse this to "".
		{"all_whitespace_unchanged", "   ", "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeProfession(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeProfession(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// Unknown professions MUST be returned unchanged (no panic, no
// lowercasing). This preserves the user's original wording in the
// downstream filters.
func TestNormalizeProfessionUnknown(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"massage therapist", "massage therapist"},
		{"locksmith", "locksmith"},
		{"Window Cleaner", "Window Cleaner"},
		{"not a real trade", "not a real trade"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeProfession(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeProfession(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// Parity regression: NormalizeProfession and NormalizeProfessionForEmbedding
// must return IDENTICAL strings for every known alias. This catches the
// sign of "Electrician" vs "electrician" drift between the two code
// paths, which would silently break ILIKE ↔ vector matching.
func TestNormalizeProfessionParityWithEmbedding(t *testing.T) {
	// Walk the union of inputs that both functions accept. For each
	// lowercase alias, also test the Title-cased form so we cover the
	// uppercase-skip-the-lowercasing path.
	for alias := range ProfessionAliases {
		checkPair(t, alias)
		checkPair(t, titleCase(alias))
	}
	// Also include a representative multi-word cased input.
	checkPair(t, "Handy Man")
	checkPair(t, "HVAC Technician")
	checkPair(t, "Aire Acondicionado")
}

func checkPair(t *testing.T, raw string) {
	t.Helper()
	searchVal := NormalizeProfession(raw)
	embedVal := NormalizeProfessionForEmbedding(raw)
	if searchVal != embedVal {
		t.Errorf("parity drift for %q: NormalizeProfession=%q, NormalizeProfessionForEmbedding=%q",
			raw, searchVal, embedVal)
	}
}

// titleCase uppercases the first ASCII letter of the alias. We avoid
// strings.Title (deprecated since Go 1.18) and the x/text dependency
// for a single call site.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 'a' - 'A'
	}
	return string(b)
}
