package domain

import "time"

// Prompt represents a saved prompt template (domain entity).
type Prompt struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Title     string    `json:"title" gorm:"size:255;not null"`
	Content   string    `json:"content" gorm:"type:text;not null"`
	Category  string    `json:"category" gorm:"size:100;default:''"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
