package core

import "time"

// SystemPrompt represents the singleton row in the system_prompts table.
// Each column is a different service's system prompt.
// New prompts are added by adding new columns at the database level.
type SystemPrompt struct {
	ID             uint      `gorm:"primaryKey"`
	HelperPrompt   string    `gorm:"column:helper_prompt;type:text;not null;default:''"`
	FrontendPrompt string    `gorm:"column:frontend_prompt;type:text;not null;default:''"`
	BackendPrompt  string    `gorm:"column:backend_prompt;type:text;not null;default:''"`
	CreatedAt      time.Time `gorm:"autoCreateTime"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime"`
}

// TableName overrides the default table name.
func (SystemPrompt) TableName() string {
	return "system_prompts"
}
