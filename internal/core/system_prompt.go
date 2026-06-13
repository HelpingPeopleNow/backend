package core

import "time"

// SystemPrompt represents the singleton row in the system_prompts table.
type SystemPrompt struct {
	ID                          uint      `gorm:"primaryKey"`
	WorkerProfilePrompt         string    `gorm:"column:worker_profile_prompt;type:text;not null;default:''"`
	ClientProfilePrompt         string    `gorm:"column:client_profile_prompt;type:text;not null;default:''"`
	FindTraderSearchPrompt      string    `gorm:"column:find_trader_search_prompt;type:text;not null;default:''"`
	FindTraderPresentationPrompt string   `gorm:"column:find_trader_presentation_prompt;type:text;not null;default:''"`
	LLMProvider                 string    `gorm:"column:llm_provider;type:varchar(32);not null;default:''"`
	CreatedAt                   time.Time `gorm:"autoCreateTime"`
	UpdatedAt                   time.Time `gorm:"autoUpdateTime"`
}

// TableName overrides the default table name.
func (SystemPrompt) TableName() string {
	return "system_prompts"
}
