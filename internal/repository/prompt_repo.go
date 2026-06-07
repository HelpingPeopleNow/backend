package repository

import (
	"github.com/HelpingPeopleNow/backend/internal/domain"
	"gorm.io/gorm"
)

// PromptRepository defines the interface for prompt persistence (port).
type PromptRepository interface {
	Create(prompt *domain.Prompt) error
	GetByID(id uint) (*domain.Prompt, error)
	List() ([]domain.Prompt, error)
	Update(prompt *domain.Prompt) error
	Delete(id uint) error
}

// GormPromptRepository is the GORM implementation of PromptRepository (adapter).
type GormPromptRepository struct {
	db *gorm.DB
}

func NewGormPromptRepository(db *gorm.DB) PromptRepository {
	return &GormPromptRepository{db: db}
}

func (r *GormPromptRepository) Create(prompt *domain.Prompt) error {
	return r.db.Create(prompt).Error
}

func (r *GormPromptRepository) GetByID(id uint) (*domain.Prompt, error) {
	var prompt domain.Prompt
	err := r.db.First(&prompt, id).Error
	return &prompt, err
}

func (r *GormPromptRepository) List() ([]domain.Prompt, error) {
	var prompts []domain.Prompt
	err := r.db.Order("created_at DESC").Find(&prompts).Error
	return prompts, err
}

func (r *GormPromptRepository) Update(prompt *domain.Prompt) error {
	return r.db.Save(prompt).Error
}

func (r *GormPromptRepository) Delete(id uint) error {
	return r.db.Delete(&domain.Prompt{}, id).Error
}
