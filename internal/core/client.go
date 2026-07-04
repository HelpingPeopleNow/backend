package core

import "time"

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
	Latitude         *float64  `json:"latitude,omitempty" gorm:"type:double precision"`
	Longitude        *float64  `json:"longitude,omitempty" gorm:"type:double precision"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

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
	Latitude         *float64  `json:"latitude,omitempty"`
	Longitude        *float64  `json:"longitude,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (c *ClientProfile) MergeFields(fields map[string]interface{}) {
	if v, ok := rawString(fields, "full_name"); ok {
		c.FullName = v
	}
	if v, ok := rawString(fields, "phone"); ok {
		c.Phone = v
	}
	if v, ok := rawString(fields, "city"); ok {
		c.City = v
	}
	if v, ok := rawString(fields, "address"); ok {
		c.Address = v
	}
	if v, ok := rawString(fields, "bio"); ok {
		c.Bio = v
	}
	if v, ok := rawString(fields, "preferred_contact"); ok {
		c.PreferredContact = v
	}
	if v, ok := rawString(fields, "property_type"); ok {
		c.PropertyType = v
	}
	if v, ok := rawString(fields, "notes"); ok {
		c.Notes = v
	}
	if v, ok := rawFloat(fields, "latitude"); ok {
		c.Latitude = &v
	}
	if v, ok := rawFloat(fields, "longitude"); ok {
		c.Longitude = &v
	}
}

func (c ClientProfile) ToDTO() ClientProfileDTO {
	return ClientProfileDTO{
		ID:               c.ID,
		UserID:           c.UserID,
		FullName:         c.FullName,
		Phone:            c.Phone,
		City:             c.City,
		Address:          c.Address,
		Bio:              c.Bio,
		PreferredContact: c.PreferredContact,
		PropertyType:     c.PropertyType,
		Notes:            c.Notes,
		Latitude:         c.Latitude,
		Longitude:        c.Longitude,
		CreatedAt:        c.CreatedAt,
		UpdatedAt:        c.UpdatedAt,
	}
}
