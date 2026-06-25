package core

import (
	"encoding/json"
	"testing"
)

func TestMergeFieldsWorkerAllFields(t *testing.T) {
	fields := map[string]interface{}{
		"profession":         "plumber",
		"business_name":      "Bob's Plumbing",
		"bio":                "10 years experience",
		"phone":              "+34600123456",
		"city":               "Madrid",
		"address":            "Calle Mayor 1",
		"website":            "https://bobs.plumbing",
		"hourly_rate":        float64(30),
		"minimum_charge":     float64(50),
		"service_radius_km":  float64(25),
		"years_experience":   float64(10),
		"free_estimate":      true,
		"has_insurance":      true,
		"emergency_service":  true,
		"certifications":     []interface{}{"GAS SAFE"},
		"languages":          []interface{}{"Spanish", "English"},
	}
	w := &WorkerProfile{}
	w.MergeFields(fields)

	if w.Profession != "plumber" {
		t.Fatalf("profession: got %q", w.Profession)
	}
	if w.BusinessName != "Bob's Plumbing" {
		t.Fatalf("business_name: got %q", w.BusinessName)
	}
	if w.Bio != "10 years experience" {
		t.Fatalf("bio: got %q", w.Bio)
	}
	if w.Phone != "+34600123456" {
		t.Fatalf("phone: got %q", w.Phone)
	}
	if w.City != "Madrid" {
		t.Fatalf("city: got %q", w.City)
	}
	if w.Address != "Calle Mayor 1" {
		t.Fatalf("address: got %q", w.Address)
	}
	if w.Website != "https://bobs.plumbing" {
		t.Fatalf("website: got %q", w.Website)
	}
	if w.HourlyRate != 30 {
		t.Fatalf("hourly_rate: got %v", w.HourlyRate)
	}
	if w.MinimumCharge != 50 {
		t.Fatalf("minimum_charge: got %v", w.MinimumCharge)
	}
	if w.ServiceRadiusKm != 25 {
		t.Fatalf("service_radius_km: got %v", w.ServiceRadiusKm)
	}
	if w.YearsExperience != 10 {
		t.Fatalf("years_experience: got %v", w.YearsExperience)
	}
	if !w.FreeEstimate {
		t.Fatal("free_estimate: expected true")
	}
	if !w.HasInsurance {
		t.Fatal("has_insurance: expected true")
	}
	if !w.EmergencyService {
		t.Fatal("emergency_service: expected true")
	}

	var certs []string
	_ = json.Unmarshal([]byte(w.Certifications), &certs)
	if len(certs) != 1 || certs[0] != "GAS SAFE" {
		t.Fatalf("certifications: got %v", certs)
	}

	var langs []string
	_ = json.Unmarshal([]byte(w.Languages), &langs)
	if len(langs) != 2 || langs[0] != "Spanish" || langs[1] != "English" {
		t.Fatalf("languages: got %v", langs)
	}
}

func TestMergeFieldsWorkerPartialUpdate(t *testing.T) {
	w := &WorkerProfile{
		Profession:    "plumber",
		BusinessName:  "Old Name",
		HourlyRate:    25,
		HasInsurance:  true,
		Certifications: `["OLD CERT"]`,
	}
	fields := map[string]interface{}{
		"profession":   "electrician",
		"hourly_rate":  float64(35),
		"has_insurance": false,
	}
	w.MergeFields(fields)

	if w.Profession != "electrician" {
		t.Fatalf("profession: got %q", w.Profession)
	}
	if w.BusinessName != "Old Name" {
		t.Fatalf("business_name should be preserved, got %q", w.BusinessName)
	}
	if w.HourlyRate != 35 {
		t.Fatalf("hourly_rate: got %v", w.HourlyRate)
	}
	if w.HasInsurance {
		t.Fatal("has_insurance: expected false")
	}
}

func TestMergeFieldsWorkerStringTypesCoerce(t *testing.T) {
	w := &WorkerProfile{}
	fields := map[string]interface{}{
		"service_radius_km": "15",
		"years_experience":  "8",
	}
	w.MergeFields(fields)

	if w.ServiceRadiusKm != 15 {
		t.Fatalf("service_radius_km from string: got %v", w.ServiceRadiusKm)
	}
	if w.YearsExperience != 8 {
		t.Fatalf("years_experience from string: got %v", w.YearsExperience)
	}
}

func TestMergeFieldsWorkerInvalidTypesIgnored(t *testing.T) {
	w := &WorkerProfile{
		Profession: "plumber",
		HourlyRate: 25,
	}
	fields := map[string]interface{}{
		"profession":  42,       // int, not string → ignored
		"hourly_rate": "abc",   // string, not float → ignored
		"has_insurance": "yes", // string, not bool → ignored
	}
	w.MergeFields(fields)

	if w.Profession != "plumber" {
		t.Fatalf("profession should be preserved, got %q", w.Profession)
	}
	if w.HourlyRate != 25 {
		t.Fatalf("hourly_rate should be preserved, got %v", w.HourlyRate)
	}
}

func TestMergeFieldsWorkerNullClears(t *testing.T) {
	w := &WorkerProfile{
		Phone: "555-1234",
		Bio:   "Old bio",
	}
	fields := map[string]interface{}{
		"phone": nil,
		"bio":   nil,
	}
	w.MergeFields(fields)

	if w.Phone != "" {
		t.Fatalf("phone should be cleared, got %q", w.Phone)
	}
	if w.Bio != "" {
		t.Fatalf("bio should be cleared, got %q", w.Bio)
	}
}

func TestMergeFieldsWorkerSocialLinks(t *testing.T) {
	w := &WorkerProfile{}
	fields := map[string]interface{}{
		"instagram": "https://instagram.com/mybiz",
		"facebook":  "https://facebook.com/mybiz",
	}
	w.MergeFields(fields)

	var links []SocialLink
	_ = json.Unmarshal([]byte(w.SocialLinks), &links)
	if len(links) != 2 {
		t.Fatalf("expected 2 social links, got %d", len(links))
	}
}

func TestWorkerBadges(t *testing.T) {
	w := WorkerProfile{HasInsurance: true, EmergencyService: true, FreeEstimate: true}
	badges := w.Badges()
	if len(badges) != 3 {
		t.Fatalf("expected 3 badges, got %d", len(badges))
	}
}

func TestWorkerBadgesEmpty(t *testing.T) {
	w := WorkerProfile{}
	badges := w.Badges()
	if len(badges) != 0 {
		t.Fatalf("expected 0 badges, got %d", len(badges))
	}
}

func TestWorkerFormattedRate(t *testing.T) {
	w := WorkerProfile{HourlyRate: 25}
	rate := w.FormattedRate()
	if rate != "€25/hr" {
		t.Fatalf("expected €25/hr, got %q", rate)
	}
}

func TestWorkerFormattedRateZero(t *testing.T) {
	w := WorkerProfile{HourlyRate: 0}
	rate := w.FormattedRate()
	if rate != "—" {
		t.Fatalf("expected —, got %q", rate)
	}
}

func TestWorkerToDTO(t *testing.T) {
	w := WorkerProfile{
		ID:             "id-1",
		UserID:         "user-1",
		Profession:     "plumber",
		Certifications: `["GAS SAFE"]`,
		Languages:      `["Spanish"]`,
		SocialLinks:    `[{"platform":"Instagram","url":"https://ig.com/me"}]`,
	}
	dto := w.ToDTO()
	if dto.ID != "id-1" {
		t.Fatalf("id: got %q", dto.ID)
	}
	if len(dto.Certifications) != 1 || dto.Certifications[0] != "GAS SAFE" {
		t.Fatalf("certifications: got %v", dto.Certifications)
	}
	if len(dto.Languages) != 1 || dto.Languages[0] != "Spanish" {
		t.Fatalf("languages: got %v", dto.Languages)
	}
	if len(dto.SocialLinks) != 1 || dto.SocialLinks[0].Platform != "Instagram" {
		t.Fatalf("social_links: got %v", dto.SocialLinks)
	}
}

func TestWorkerToDTOEmptyArrays(t *testing.T) {
	w := WorkerProfile{}
	dto := w.ToDTO()
	if dto.Certifications == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if dto.Languages == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if dto.SocialLinks == nil {
		t.Fatal("expected empty slice, got nil")
	}
}

func TestWorkerSearchSummary(t *testing.T) {
	w := WorkerProfile{
		BusinessName:   "Bob's Plumbing",
		Profession:     "plumber",
		City:           "Madrid",
		HourlyRate:     25,
		YearsExperience: 5,
	}
	summary := w.SearchSummary(1)
	expected := "1. Bob's Plumbing - plumber in Madrid, €25/hr, 5 years experience"
	if summary != expected {
		t.Fatalf("unexpected summary:\n got: %q\nwant: %q", summary, expected)
	}
}

func TestWorkerSearchSummaryNoBusinessName(t *testing.T) {
	w := WorkerProfile{
		Profession: "plumber",
		City:       "Madrid",
		HourlyRate: 25,
	}
	summary := w.SearchSummary(1)
	expected := "1. plumber - plumber in Madrid, €25/hr"
	if summary != expected {
		t.Fatalf("unexpected summary:\n got: %q\nwant: %q", summary, expected)
	}
}

func TestFindTraderCards(t *testing.T) {
	workers := []WorkerProfile{
		{ID: "w1", Profession: "plumber", Certifications: `["GAS SAFE"]`},
		{ID: "w2", Profession: "electrician"},
	}
	cards := FindTraderCards(workers)
	if len(cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(cards))
	}
	if cards[0].ID != "w1" {
		t.Fatalf("card 0 id: got %q", cards[0].ID)
	}
	if len(cards[0].Certifications) != 1 || cards[0].Certifications[0] != "GAS SAFE" {
		t.Fatalf("card 0 certs: got %v", cards[0].Certifications)
	}
}

func TestWorkerToFindTraderCard(t *testing.T) {
	w := WorkerProfile{
		ID:               "w1",
		Profession:       "plumber",
		BusinessName:     "Bob's",
		Bio:              "10 years",
		City:             "Madrid",
		Phone:            "+34600123456",
		HourlyRate:       25,
		FreeEstimate:     true,
		YearsExperience:  10,
		Certifications:   `["GAS SAFE","NICEIC"]`,
		HasInsurance:     true,
		EmergencyService: true,
	}
	card := w.ToFindTraderCard()
	if card.ID != "w1" {
		t.Fatalf("id: got %q", card.ID)
	}
	if len(card.Certifications) != 2 {
		t.Fatalf("expected 2 certs, got %d", len(card.Certifications))
	}
	if !card.HasInsurance {
		t.Fatal("expected HasInsurance=true")
	}
}

func TestWorkerToFindTraderCardEmptyCerts(t *testing.T) {
	w := WorkerProfile{}
	card := w.ToFindTraderCard()
	if card.Certifications == nil {
		t.Fatal("expected empty slice, got nil")
	}
}
