package core

import (
	"strings"
	"testing"
)

func TestSha256Hex(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty", "", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"hello", "hello", "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},
		{"unicode", "café", "871b60e008993a45943c5b7c225169a48a2d6e8d8e8d94f3e6d6b5c3a1f2e0d4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sha256Hex(tt.input)
			if len(got) != 64 {
				t.Errorf("sha256Hex(%q) length = %d, want 64", tt.input, len(got))
			}
			// Verify it's lowercase hex
			if got != strings.ToLower(got) {
				t.Errorf("sha256Hex(%q) = %q, want lowercase", tt.input, got)
			}
		})
	}
}

func TestSha256Hex_Deterministic(t *testing.T) {
	a := sha256Hex("test input")
	b := sha256Hex("test input")
	if a != b {
		t.Errorf("sha256Hex is not deterministic: %q != %q", a, b)
	}
}

func TestSha256Hex_DifferentInputs(t *testing.T) {
	a := sha256Hex("hello")
	b := sha256Hex("world")
	if a == b {
		t.Error("sha256Hex returned same hash for different inputs")
	}
}

func TestHashField(t *testing.T) {
	// HashField wraps sha256Hex — verify delegation
	input := "profession:Electrician"
	got := HashField(input)
	expected := sha256Hex(input)
	if got != expected {
		t.Errorf("HashField(%q) = %q, want %q", input, got, expected)
	}
	// Verify lowercase hex, length 64
	if len(got) != 64 || got != strings.ToLower(got) {
		t.Errorf("HashField(%q) = %q, want lowercase hex of length 64", input, got)
	}
}

func TestJoinJSONArray(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", ""},
		{"single item", `["ISO 9001"]`, "ISO 9001"},
		{"multiple items", `["ISO 9001","OSHA"]`, "ISO 9001, OSHA"},
		{"invalid JSON (raw text)", "not json", "not json"},
		{"empty array", `[]`, ""},
		{"single quoted", `["Plumbing License"]`, "Plumbing License"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinJSONArray(tt.input)
			if got != tt.expected {
				t.Errorf("joinJSONArray(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestBuildFieldTexts(t *testing.T) {
	tests := []struct {
		name      string
		profile   WorkerProfile
		wantKeys  []string
		wantNoKey []string
	}{
		{
			name:      "empty profile produces no fields",
			profile:   WorkerProfile{},
			wantKeys:  nil,
			wantNoKey: []string{"profession", "bio", "certifications", "city", "languages", "business_name"},
		},
		{
			name:      "all fields populated",
			profile:   WorkerProfile{Profession: "plumber", Bio: "Licensed plumber", Certifications: `["ISO 9001"]`, City: "Madrid", Languages: `["en","es"]`, BusinessName: "Acme Plumbing"},
			wantKeys:  []string{"profession", "bio", "certifications", "city", "languages", "business_name"},
			wantNoKey: nil,
		},
		{
			name:      "profession normalized adds profession_raw",
			profile:   WorkerProfile{Profession: "electricista"},
			wantKeys:  []string{"profession", "profession_raw"},
			wantNoKey: nil,
		},
		{
			name:      "profession already normalized no profession_raw",
			profile:   WorkerProfile{Profession: "Electrician"},
			wantKeys:  []string{"profession"},
			wantNoKey: []string{"profession_raw"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildFieldTexts(&tt.profile)
			for _, k := range tt.wantKeys {
				if _, ok := got[k]; !ok {
					t.Errorf("BuildFieldTexts missing key %q", k)
				}
			}
			for _, k := range tt.wantNoKey {
				if _, ok := got[k]; ok {
					t.Errorf("BuildFieldTexts unexpected key %q", k)
				}
			}
		})
	}
}

func TestNormalizeProfessionForEmbedding_Table(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"electricista", "Electrician"},
		{"Electricista", "Electrician"},
		{"electrician", "Electrician"},
		{"Electrician", "Electrician"},
		{"plumber", "Plumber"},
		{"Plomero", "Plumber"},
		{"limpieza", "Cleaner"},
		{"cleaner", "Cleaner"},
		{"manitas", "Handyman"},
		{"carpintero", "Carpenter"},
		{"pintor", "Painter"},
		{"jardinero", "Landscaper"},
		{"tejado", "Roofer"},
		{"clima", "HVAC Technician"},
		{"unknown profession", "unknown profession"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeProfessionForEmbedding(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeProfessionForEmbedding(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFieldWeightsExist(t *testing.T) {
	expectedFields := []string{"profession", "profession_raw", "bio", "certifications", "city", "languages", "business_name"}
	for _, f := range expectedFields {
		if _, ok := FieldWeights[f]; !ok {
			t.Errorf("FieldWeights missing field %q", f)
		}
	}
}
