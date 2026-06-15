package core

import "time"

// ClientProfile holds basic contact details for a client user.
// One-to-one with user.id (UserID is the FK).
type ClientProfile struct {
	ID               string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID           string    `gorm:"type:text;uniqueIndex;not null" json:"user_id"`
	FullName         string    `json:"full_name"`
	Phone            string    `json:"phone"`
	City             string    `json:"city"`
	Address          string    `json:"address"`
	Bio              string    `json:"bio"`
	PreferredContact string    `json:"preferred_contact"`
	PropertyType     string    `json:"property_type"`
	Notes            string    `json:"notes"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ClientProfileDTO is the JSON-facing shape returned to the frontend.
type ClientProfileDTO struct {
	ID               string    `json:"id"`
	UserID           string    `json:"user_id"`
	FullName         string    `json:"full_name"`
	Phone            string    `json:"phone"`
	City             string    `json:"city"`
	Address          string    `json:"address"`
	Bio              string    `json:"bio"`
	PreferredContact string    `json:"preferred_contact"`
	PropertyType     string    `json:"property_type"`
	Notes            string    `json:"notes"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
