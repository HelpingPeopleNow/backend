package services

import (
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

func TestNormalizeProfessionElectrician(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"electricista", "electrician"},
		{"Electricista", "electrician"},
		{"electrician", "electrician"},
		{"fontanero", "plumber"},
		{"plomero", "plumber"},
		{"plumber", "plumber"},
		{"limpiador", "cleaner"},
		{"limpieza", "cleaner"},
		{"cleaner", "cleaner"},
		{"manitas", "handyman"},
		{"handyman", "handyman"},
		{"carpintero", "carpintero"},
		{"carpenter", "carpintero"},
		{"pintor", "painter"},
		{"painter", "painter"},
		{"jardinero", "landscaper"},
		{"landscaper", "landscaper"},
		{"tejador", "roofer"},
		{"roofer", "roofer"},
		{"clima", "hvac technician"},
		{"hvac", "hvac technician"},
		{"unknown_trade", "unknown_trade"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeProfession(tc.input)
			if got != tc.expected {
				t.Fatalf("normalizeProfession(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestBuildWorkerSummariesEmpty(t *testing.T) {
	result := buildWorkerSummaries(nil, "need a plumber")
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !contains(result, "No workers matched") {
		t.Fatalf("expected 'No workers matched' in result, got %q", result)
	}
	if !contains(result, "need a plumber") {
		t.Fatalf("expected original message in result")
	}
}

func TestBuildWorkerSummariesWithData(t *testing.T) {
	workers := []core.WorkerProfile{
		{
			BusinessName: "Bob's Plumbing",
			Profession:   "plumber",
			City:         "Madrid",
			HourlyRate:   25,
			Phone:        "+34600123456",
			Bio:          "10 years experience",
			HasInsurance: true,
		},
	}
	result := buildWorkerSummaries(workers, "need a plumber")
	if !contains(result, "Bob's Plumbing") {
		t.Fatalf("expected business name in result")
	}
	if !contains(result, "+34600123456") {
		t.Fatalf("expected phone in result")
	}
	if !contains(result, "insured") {
		t.Fatalf("expected 'insured' in result")
	}
}

func TestSearchFiltersFromJSON(t *testing.T) {
	raw := []byte(`{"profession":"plumber","city":"Madrid","emergency":true,"free_estimate":false,"insured":true}`)
	filters := searchFiltersFromJSON(raw)

	if filters.Profession != "plumber" {
		t.Fatalf("profession: got %q", filters.Profession)
	}
	if filters.City != "Madrid" {
		t.Fatalf("city: got %q", filters.City)
	}
	if !filters.EmergencyOnly {
		t.Fatal("emergency_only: expected true")
	}
	if filters.FreeEstimateOnly {
		t.Fatal("free_estimate_only: expected false")
	}
	if !filters.InsuredOnly {
		t.Fatal("insured_only: expected true")
	}
}

func TestSearchFiltersFromEmptyJSON(t *testing.T) {
	filters := searchFiltersFromJSON([]byte(`{}`))
	if filters.Profession != "" {
		t.Fatalf("profession: expected empty, got %q", filters.Profession)
	}
}

func TestSearchFiltersFromInvalidJSON(t *testing.T) {
	filters := searchFiltersFromJSON([]byte(`not json`))
	if filters.Profession != "" {
		t.Fatalf("profession: expected empty for invalid JSON, got %q", filters.Profession)
	}
}

// contains is a simple string contains helper.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
