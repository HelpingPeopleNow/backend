package core

import "time"

// WorkerProfile holds all professional details for a worker user.
// One-to-one with user.id (UserId is the FK).
type WorkerProfile struct {
	ID               string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID           string    `gorm:"type:text;uniqueIndex;not null" json:"user_id"`
	Profession       string    `json:"profession"`
	BusinessName     string    `json:"business_name"`
	Bio              string    `json:"bio"`
	Phone            string    `json:"phone"`
	City             string    `json:"city"`
	ServiceRadiusKm  int       `json:"service_radius_km"`
	Address          string    `json:"address"`
	HourlyRate       float64   `json:"hourly_rate"`
	MinimumCharge    float64   `json:"minimum_charge"`
	FreeEstimate     bool      `json:"free_estimate"`
	YearsExperience  int       `json:"years_experience"`
	Certifications   string    `json:"certifications"`     // JSON array: ["Gas Cert", "Electrical License"]
	HasInsurance     bool      `json:"has_insurance"`
	Languages        string    `json:"languages"`          // JSON array: ["Spanish", "English"]
	EmergencyService bool      `json:"emergency_service"`
	Website          string    `json:"website"`
	SocialLinks      string    `json:"social_links"`       // JSON array: [{"platform":"Instagram","url":"..."}]
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// WorkerProfileDTO is the JSON-facing shape returned to the frontend.
type WorkerProfileDTO struct {
	ID               string    `json:"id"`
	UserID           string    `json:"user_id"`
	Profession       string    `json:"profession"`
	BusinessName     string    `json:"business_name"`
	Bio              string    `json:"bio"`
	Phone            string    `json:"phone"`
	City             string    `json:"city"`
	ServiceRadiusKm  int       `json:"service_radius_km"`
	Address          string    `json:"address"`
	HourlyRate       float64   `json:"hourly_rate"`
	MinimumCharge    float64   `json:"minimum_charge"`
	FreeEstimate     bool      `json:"free_estimate"`
	YearsExperience  int       `json:"years_experience"`
	Certifications   []string  `json:"certifications"`
	HasInsurance     bool      `json:"has_insurance"`
	Languages        []string  `json:"languages"`
	EmergencyService bool      `json:"emergency_service"`
	Website          string    `json:"website"`
	SocialLinks      []SocialLink `json:"social_links"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type SocialLink struct {
	Platform string `json:"platform"`
	URL      string `json:"url"`
}
