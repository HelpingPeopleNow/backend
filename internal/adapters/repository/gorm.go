package repository

import (
	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
	"gorm.io/gorm"
)

// GormPromptRepository is the outbound adapter — implements ports.PromptRepository
// via GORM. The hexagon never imports this package.
type GormPromptRepository struct {
	db *gorm.DB
}

func NewGormPromptRepository(db *gorm.DB) ports.PromptRepository {
	return &GormPromptRepository{db: db}
}

func (r *GormPromptRepository) Create(prompt *core.Prompt) error {
	return r.db.Create(prompt).Error
}

func (r *GormPromptRepository) GetByID(id uint) (*core.Prompt, error) {
	var prompt core.Prompt
	err := r.db.First(&prompt, id).Error
	return &prompt, err
}

func (r *GormPromptRepository) List() ([]core.Prompt, error) {
	var prompts []core.Prompt
	err := r.db.Order("created_at DESC").Find(&prompts).Error
	return prompts, err
}

func (r *GormPromptRepository) Update(prompt *core.Prompt) error {
	return r.db.Save(prompt).Error
}

func (r *GormPromptRepository) Delete(id uint) error {
	return r.db.Delete(&core.Prompt{}, id).Error
}
