package core

import "time"

// SystemPrompt represents the singleton row in the system_prompts table.
type SystemPrompt struct {
	ID           uint      `gorm:"primaryKey"`
	HelperPrompt string    `gorm:"column:helper_prompt;type:text;not null;default:''"`
	CreatedAt    time.Time `gorm:"autoCreateTime"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime"`
}

// TableName overrides the default table name.
func (SystemPrompt) TableName() string {
	return "system_prompts"
}
