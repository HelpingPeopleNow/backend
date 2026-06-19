package core

import "time"

type SystemPrompt struct {
	ID                           uint      `gorm:"primaryKey" json:"id"`
	WorkerProfilePrompt          string    `gorm:"column:worker_profile_prompt;type:text;not null;default:''"`
	ClientProfilePrompt          string    `gorm:"column:client_profile_prompt;type:text;not null;default:''"`
	FindTraderSearchPrompt       string    `gorm:"column:find_trader_search_prompt;type:text;not null;default:''"`
	FindTraderPresentationPrompt string    `gorm:"column:find_trader_presentation_prompt;type:text;not null;default:''"`
	LLMProvider                  string    `gorm:"column:llm_provider;type:varchar(32);not null;default:''"`
	CreatedAt                    time.Time `gorm:"autoCreateTime"`
	UpdatedAt                    time.Time `gorm:"autoUpdateTime"`
}

func (SystemPrompt) TableName() string {
	return "system_prompts"
}

func (sp SystemPrompt) EffectiveWorkerPrompt() string {
	if sp.WorkerProfilePrompt != "" {
		return sp.WorkerProfilePrompt
	}
	return ""
}

func (sp SystemPrompt) EffectiveClientPrompt() string {
	if sp.ClientProfilePrompt != "" {
		return sp.ClientProfilePrompt
	}
	return ""
}

func (sp SystemPrompt) EffectiveFindTraderPresentation() string {
	if sp.FindTraderPresentationPrompt != "" {
		return sp.FindTraderPresentationPrompt
	}
	return "You are a helpful assistant. Present search results conversationally."
}
