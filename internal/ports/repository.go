package ports

import "github.com/HelpingPeopleNow/backend/internal/core"

// PromptRepository is the outbound port — the hexagon defines what it needs,
// adapters implement it.
type PromptRepository interface {
	Create(prompt *core.PromptHelper) error
	GetByID(id uint) (*core.PromptHelper, error)
	List() ([]core.PromptHelper, error)
	Update(prompt *core.PromptHelper) error
	Delete(id uint) error
}
