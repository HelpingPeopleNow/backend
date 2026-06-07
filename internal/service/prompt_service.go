package service

import (
	"fmt"

	"github.com/HelpingPeopleNow/backend/internal/domain"
	"github.com/HelpingPeopleNow/backend/internal/repository"
)

// PromptService contains business logic for prompts (use cases).
type PromptService struct {
	repo repository.PromptRepository
}

func NewPromptService(repo repository.PromptRepository) *PromptService {
	return &PromptService{repo: repo}
}

func (s *PromptService) Create(title, content, category string) (*domain.Prompt, error) {
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	prompt := &domain.Prompt{
		Title:    title,
		Content:  content,
		Category: category,
	}
	if err := s.repo.Create(prompt); err != nil {
		return nil, fmt.Errorf("failed to create prompt: %w", err)
	}
	return prompt, nil
}

func (s *PromptService) GetByID(id uint) (*domain.Prompt, error) {
	return s.repo.GetByID(id)
}

func (s *PromptService) List() ([]domain.Prompt, error) {
	return s.repo.List()
}

func (s *PromptService) Update(id uint, title, content, category string) (*domain.Prompt, error) {
	prompt, err := s.repo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("prompt not found: %w", err)
	}
	if title != "" {
		prompt.Title = title
	}
	if content != "" {
		prompt.Content = content
	}
	if category != "" {
		prompt.Category = category
	}
	if err := s.repo.Update(prompt); err != nil {
		return nil, fmt.Errorf("failed to update prompt: %w", err)
	}
	return prompt, nil
}

func (s *PromptService) Delete(id uint) error {
	return s.repo.Delete(id)
}
