package core

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type WorkerProfile struct {
	ID               string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID           string    `gorm:"type:text;uniqueIndex;not null" json:"user_id"`
	Profession       string    `json:"profession"`
	BusinessName     string    `json:"business_name"`
	Slug             string    `gorm:"type:text;index" json:"slug"`
	Bio              string    `json:"bio"`
	Phone            string    `json:"phone"`
	City             string    `json:"city"`
	ServiceRadiusKm  int       `json:"service_radius_km"`
	Address          string    `json:"address"`
	HourlyRate       float64   `json:"hourly_rate"`
	MinimumCharge    float64   `json:"minimum_charge"`
	FreeEstimate     bool      `json:"free_estimate"`
	YearsExperience  int       `json:"years_experience"`
	Certifications   string    `json:"certifications"`
	HasInsurance     bool      `json:"has_insurance"`
	Languages        string    `json:"languages"`
	EmergencyService bool      `json:"emergency_service"`
	Website          string    `json:"website"`
	SocialLinks      string    `json:"social_links"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type WorkerProfileDTO struct {
	ID               string       `json:"id"`
	UserID           string       `json:"user_id"`
	Profession       string       `json:"profession"`
	BusinessName     string       `json:"business_name"`
	Slug             string       `json:"slug"`
	Bio              string       `json:"bio"`
	Phone            string       `json:"phone"`
	City             string       `json:"city"`
	ServiceRadiusKm  int          `json:"service_radius_km"`
	Address          string       `json:"address"`
	HourlyRate       float64      `json:"hourly_rate"`
	MinimumCharge    float64      `json:"minimum_charge"`
	FreeEstimate     bool         `json:"free_estimate"`
	YearsExperience  int          `json:"years_experience"`
	Certifications   []string     `json:"certifications"`
	HasInsurance     bool         `json:"has_insurance"`
	Languages        []string     `json:"languages"`
	EmergencyService bool         `json:"emergency_service"`
	Website          string       `json:"website"`
	SocialLinks      []SocialLink `json:"social_links"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
}

type SocialLink struct {
	Platform string `json:"platform"`
	URL      string `json:"url"`
}

func (w WorkerProfile) Badges() []string {
	var badges []string
	if w.HasInsurance {
		badges = append(badges, "✓ Insured")
	}
	if w.EmergencyService {
		badges = append(badges, "⚡ Emergency")
	}
	if w.FreeEstimate {
		badges = append(badges, "📋 Free Estimate")
	}
	return badges
}

func (w WorkerProfile) FormattedRate() string {
	return NewMoney(w.HourlyRate).PerHour()
}

func (w *WorkerProfile) MergeFields(fields map[string]interface{}) {
	if v, ok := rawString(fields, "profession"); ok {
		w.Profession = v
	}
	if v, ok := rawString(fields, "business_name"); ok {
		w.BusinessName = v
	}
	if v, ok := rawString(fields, "bio"); ok {
		w.Bio = v
	}
	if v, ok := rawString(fields, "phone"); ok {
		w.Phone = v
	}
	if v, ok := rawString(fields, "city"); ok {
		w.City = v
	}
	if v, ok := rawString(fields, "address"); ok {
		w.Address = v
	}
	if v, ok := rawString(fields, "website"); ok {
		w.Website = v
	}

	if v, ok := rawFloat(fields, "hourly_rate"); ok {
		w.HourlyRate = v
	}
	if v, ok := rawFloat(fields, "minimum_charge"); ok {
		w.MinimumCharge = v
	}
	if v, ok := rawInt(fields, "service_radius_km"); ok {
		w.ServiceRadiusKm = v
	}
	if v, ok := rawInt(fields, "years_experience"); ok {
		w.YearsExperience = v
	}

	if v, ok := rawBool(fields, "free_estimate"); ok {
		w.FreeEstimate = v
	}
	if v, ok := rawBool(fields, "has_insurance"); ok {
		w.HasInsurance = v
	}
	if v, ok := rawBool(fields, "emergency_service"); ok {
		w.EmergencyService = v
	}

	w.Certifications = mergeJSONArray(fields, "certifications", w.Certifications)
	w.Languages = mergeJSONArray(fields, "languages", w.Languages)
	w.SocialLinks = mergeSocialLinks(fields, w.SocialLinks)
}

func (w WorkerProfile) ToDTO() WorkerProfileDTO {
	var certs, langs []string
	_ = json.Unmarshal([]byte(w.Certifications), &certs)
	_ = json.Unmarshal([]byte(w.Languages), &langs)
	var social []SocialLink
	_ = json.Unmarshal([]byte(w.SocialLinks), &social)
	if certs == nil {
		certs = []string{}
	}
	if langs == nil {
		langs = []string{}
	}
	if social == nil {
		social = []SocialLink{}
	}
	return WorkerProfileDTO{
		ID:               w.ID,
		UserID:           w.UserID,
		Profession:       w.Profession,
		BusinessName:     w.BusinessName,
		Slug:             w.Slug,
		Bio:              w.Bio,
		Phone:            w.Phone,
		City:             w.City,
		ServiceRadiusKm:  w.ServiceRadiusKm,
		Address:          w.Address,
		HourlyRate:       w.HourlyRate,
		MinimumCharge:    w.MinimumCharge,
		FreeEstimate:     w.FreeEstimate,
		YearsExperience:  w.YearsExperience,
		Certifications:   certs,
		HasInsurance:     w.HasInsurance,
		Languages:        langs,
		EmergencyService: w.EmergencyService,
		Website:          w.Website,
		SocialLinks:      social,
		CreatedAt:        w.CreatedAt,
		UpdatedAt:        w.UpdatedAt,
	}
}

type FindTraderCard struct {
	ID               string   `json:"id"`
	Profession       string   `json:"profession"`
	BusinessName     string   `json:"business_name"`
	Slug             string   `json:"slug"`
	Bio              string   `json:"bio"`
	City             string   `json:"city"`
	Phone            string   `json:"phone"`
	HourlyRate       float64  `json:"hourly_rate"`
	FreeEstimate     bool     `json:"free_estimate"`
	YearsExperience  int      `json:"years_experience"`
	Certifications   []string `json:"certifications"`
	HasInsurance     bool     `json:"has_insurance"`
	EmergencyService bool     `json:"emergency_service"`
}

func (w WorkerProfile) ToFindTraderCard() FindTraderCard {
	var certs []string
	_ = json.Unmarshal([]byte(w.Certifications), &certs)
	if certs == nil {
		certs = []string{}
	}
	return FindTraderCard{
		ID:               w.ID,
		Profession:       w.Profession,
		BusinessName:     w.BusinessName,
		Slug:             w.Slug,
		Bio:              w.Bio,
		City:             w.City,
		Phone:            w.Phone,
		HourlyRate:       w.HourlyRate,
		FreeEstimate:     w.FreeEstimate,
		YearsExperience:  w.YearsExperience,
		Certifications:   certs,
		HasInsurance:     w.HasInsurance,
		EmergencyService: w.EmergencyService,
	}
}

func FindTraderCards(workers []WorkerProfile) []FindTraderCard {
	cards := make([]FindTraderCard, 0, len(workers))
	for _, w := range workers {
		cards = append(cards, w.ToFindTraderCard())
	}
	return cards
}

func (w WorkerProfile) SearchSummary(index int) string {
	name := w.BusinessName
	if name == "" {
		name = w.Profession
	}
	rate := w.FormattedRate()
	if w.YearsExperience > 0 {
		return fmtSummary(index, name, w.Profession, w.City, rate, w.YearsExperience)
	}
	return fmt.Sprintf("%d. %s - %s in %s, %s", index, name, w.Profession, w.City, rate)
}

// ── Slug helpers ───────────────────────────────────────────────────

func GenerateSlug(businessName string) string {
	s := strings.ToLower(businessName)
	s = strings.TrimSpace(s)
	s = regexp.MustCompile(`[^a-z0-9\s-]`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`[\s]+`).ReplaceAllString(s, "-")
	s = regexp.MustCompile(`[-]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 50 {
		s = s[:50]
		s = strings.TrimRight(s, "-")
	}
	return s
}

func ValidateSlug(slug string) bool {
	return regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`).MatchString(slug)
}

// ── Public DTO ─────────────────────────────────────────────────────

type WorkerPublicDTO struct {
	ID               string       `json:"id"`
	Slug             string       `json:"slug"`
	Profession       string       `json:"profession"`
	BusinessName     string       `json:"business_name"`
	Bio              string       `json:"bio"`
	City             string       `json:"city"`
	ServiceRadiusKm  int          `json:"service_radius_km"`
	HourlyRate       float64      `json:"hourly_rate"`
	MinimumCharge    float64      `json:"minimum_charge"`
	FreeEstimate     bool         `json:"free_estimate"`
	YearsExperience  int          `json:"years_experience"`
	Certifications   []string     `json:"certifications"`
	HasInsurance     bool         `json:"has_insurance"`
	Languages        []string     `json:"languages"`
	EmergencyService bool         `json:"emergency_service"`
	Website          string       `json:"website"`
	SocialLinks      []SocialLink `json:"social_links"`
	CreatedAt        time.Time    `json:"created_at"`
}

func WorkerProfileToPublicDTO(wp *WorkerProfile) WorkerPublicDTO {
	var certs []string
	_ = json.Unmarshal([]byte(wp.Certifications), &certs)
	var langs []string
	_ = json.Unmarshal([]byte(wp.Languages), &langs)
	var links []SocialLink
	_ = json.Unmarshal([]byte(wp.SocialLinks), &links)
	if certs == nil {
		certs = []string{}
	}
	if langs == nil {
		langs = []string{}
	}
	if links == nil {
		links = []SocialLink{}
	}
	return WorkerPublicDTO{
		ID:               wp.ID,
		Slug:             wp.Slug,
		Profession:       wp.Profession,
		BusinessName:     wp.BusinessName,
		Bio:              wp.Bio,
		City:             wp.City,
		ServiceRadiusKm:  wp.ServiceRadiusKm,
		HourlyRate:       wp.HourlyRate,
		MinimumCharge:    wp.MinimumCharge,
		FreeEstimate:     wp.FreeEstimate,
		YearsExperience:  wp.YearsExperience,
		Certifications:   certs,
		HasInsurance:     wp.HasInsurance,
		Languages:        langs,
		EmergencyService: wp.EmergencyService,
		Website:          wp.Website,
		SocialLinks:      links,
		CreatedAt:        wp.CreatedAt,
	}
}
