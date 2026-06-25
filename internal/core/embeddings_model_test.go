package core

import (
	"testing"
)

func TestConversationIsNew(t *testing.T) {
	c := Conversation{}
	if !c.IsNew() {
		t.Fatal("empty ID should be new")
	}
	c.ID = "some-id"
	if c.IsNew() {
		t.Fatal("non-empty ID should not be new")
	}
}

func TestDirectConversationTableName(t *testing.T) {
	dc := DirectConversation{}
	if dc.TableName() != "direct_conversations" {
		t.Fatalf("expected direct_conversations, got %q", dc.TableName())
	}
}

func TestDirectConversationIsActive(t *testing.T) {
	dc := DirectConversation{Status: "active"}
	if !dc.IsActive() {
		t.Fatal("expected active")
	}
	dc.Status = "blocked"
	if dc.IsActive() {
		t.Fatal("blocked should not be active")
	}
}

func TestDirectConversationIsBlocked(t *testing.T) {
	dc := DirectConversation{Status: "blocked"}
	if !dc.IsBlocked() {
		t.Fatal("expected blocked")
	}
	dc.Status = "active"
	if dc.IsBlocked() {
		t.Fatal("active should not be blocked")
	}
}

func TestDirectMessageTableName(t *testing.T) {
	dm := DirectMessage{}
	if dm.TableName() != "direct_messages" {
		t.Fatalf("expected direct_messages, got %q", dm.TableName())
	}
}

func TestPhoneNumberNew(t *testing.T) {
	p := NewPhoneNumber("  +34600123456  ")
	if p.String() != "+34600123456" {
		t.Fatalf("expected trimmed phone, got %q", p.String())
	}
}

func TestPhoneNumberIsEmpty(t *testing.T) {
	p := NewPhoneNumber("")
	if !p.IsEmpty() {
		t.Fatal("empty phone should be empty")
	}
	p2 := NewPhoneNumber("+34600123456")
	if p2.IsEmpty() {
		t.Fatal("non-empty phone should not be empty")
	}
}

func TestMoneyAmount(t *testing.T) {
	m := NewMoney(25.5)
	if m.Amount() != 25.5 {
		t.Fatalf("expected 25.5, got %v", m.Amount())
	}
}

func TestMoneyString(t *testing.T) {
	m := NewMoney(25.5)
	s := m.String()
	if s == "" {
		t.Fatal("expected non-empty string")
	}
}

func TestMoneyStringZero(t *testing.T) {
	m := NewMoney(0)
	if m.String() != "—" {
		t.Fatalf("expected —, got %q", m.String())
	}
}

func TestHashField(t *testing.T) {
	h := HashField("hello")
	if len(h) != 64 {
		t.Fatalf("expected 64-char hex, got %d chars", len(h))
	}
	// Deterministic
	h2 := HashField("hello")
	if h != h2 {
		t.Fatal("HashField should be deterministic")
	}
}

func TestJoinJSONArray(t *testing.T) {
	result := joinJSONArray(`["GAS SAFE","NICEIC"]`)
	if result != "GAS SAFE, NICEIC" {
		t.Fatalf("expected 'GAS SAFE, NICEIC', got %q", result)
	}
}

func TestJoinJSONArrayEmpty(t *testing.T) {
	if joinJSONArray("") != "" {
		t.Fatal("expected empty for empty input")
	}
}

func TestJoinJSONArrayInvalid(t *testing.T) {
	result := joinJSONArray("not json")
	if result != "not json" {
		t.Fatalf("expected raw fallback, got %q", result)
	}
}

func TestBuildFieldTextsFull(t *testing.T) {
	wp := &WorkerProfile{
		Profession:     "plumber",
		Bio:            "10 years",
		Certifications: `["GAS SAFE"]`,
		City:           "Madrid",
		Languages:      `["Spanish"]`,
		BusinessName:   "Bob's",
	}
	texts := BuildFieldTexts(wp)
	if texts["profession"] != "Plumber" {
		t.Fatalf("expected Plumber, got %q", texts["profession"])
	}
	if texts["profession_raw"] != "plumber" {
		t.Fatalf("expected profession_raw=plumber, got %q", texts["profession_raw"])
	}
	if texts["bio"] != "10 years" {
		t.Fatalf("expected bio, got %q", texts["bio"])
	}
	if texts["certifications"] != "GAS SAFE" {
		t.Fatalf("expected certifications, got %q", texts["certifications"])
	}
	if texts["city"] != "Madrid" {
		t.Fatalf("expected city, got %q", texts["city"])
	}
	if texts["languages"] != "Spanish" {
		t.Fatalf("expected languages, got %q", texts["languages"])
	}
	if texts["business_name"] != "Bob's" {
		t.Fatalf("expected business_name, got %q", texts["business_name"])
	}
}

func TestBuildFieldTextsEmpty(t *testing.T) {
	wp := &WorkerProfile{}
	texts := BuildFieldTexts(wp)
	if len(texts) != 0 {
		t.Fatalf("expected empty map for empty profile, got %d entries", len(texts))
	}
}

func TestBuildFieldTextsUnnormalizedProfession(t *testing.T) {
	// "Painter" is already the canonical form — no profession_raw should be added
	wp := &WorkerProfile{Profession: "Painter"}
	texts := BuildFieldTexts(wp)
	if texts["profession"] != "Painter" {
		t.Fatalf("expected Painter, got %q", texts["profession"])
	}
	if _, ok := texts["profession_raw"]; ok {
		t.Fatal("expected no profession_raw for already-canonical profession")
	}
}

func TestNormalizeProfessionForEmbedding(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"electricista", "Electrician"},
		{"Electricista", "Electrician"},
		{"plumber", "Plumber"},
		{"fontanero", "Plumber"},
		{"cleaner", "Cleaner"},
		{"limpieza", "Cleaner"},
		{"handyman", "Handyman"},
		{"manitas", "Handyman"},
		{"carpenter", "Carpenter"},
		{"carpintero", "Carpenter"},
		{"painter", "Painter"},
		{"pintor", "Painter"},
		{"landscaper", "Landscaper"},
		{"jardinero", "Landscaper"},
		{"roofer", "Roofer"},
		{"tejado", "Roofer"},
		{"hvac", "HVAC Technician"},
		{"clima", "HVAC Technician"},
		{"unknown_trade", "unknown_trade"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeProfessionForEmbedding(tc.input)
			if got != tc.expected {
				t.Fatalf("normalizeProfessionForEmbedding(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestWorkerEmbeddingTableName(t *testing.T) {
	we := WorkerEmbedding{}
	if we.TableName() != "worker_embeddings" {
		t.Fatalf("expected worker_embeddings, got %q", we.TableName())
	}
}

func TestFieldWeightsKeys(t *testing.T) {
	// Ensure FieldWeights covers the expected fields
	expectedKeys := []string{"profession", "profession_raw", "bio", "certifications", "city", "languages", "business_name"}
	for _, key := range expectedKeys {
		if _, ok := FieldWeights[key]; !ok {
			t.Fatalf("FieldWeights missing key %q", key)
		}
	}
}
