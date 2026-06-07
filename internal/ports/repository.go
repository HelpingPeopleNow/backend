package ports

import "github.com/HelpingPeopleNow/backend/internal/core"

// PromptRepository is the outbound port — the hexagon defines what it needs,
// adapters implement it.
type PromptRepository interface {
	Create(prompt *core.Prompt) error
	GetByID(id uint) (*core.Prompt, error)
	List() ([]core.Prompt, error)
	Update(prompt *core.Prompt) error
	Delete(id uint) error
}
