package service

import (
	"fmt"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
)

// PromptService implements the application use cases (hexagon business logic).
// It depends only on the ports.PromptRepository interface — never on an adapter.
type PromptService struct {
	repo ports.PromptRepository
}

func NewPromptService(repo ports.PromptRepository) *PromptService {
	return &PromptService{repo: repo}
}

func (s *PromptService) Create(title, content, category string) (*core.Prompt, error) {
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	prompt := &core.Prompt{
		Title:    title,
		Content:  content,
		Category: category,
	}
	if err := s.repo.Create(prompt); err != nil {
		return nil, fmt.Errorf("failed to create prompt: %w", err)
	}
	return prompt, nil
}

func (s *PromptService) GetByID(id uint) (*core.Prompt, error) {
	return s.repo.GetByID(id)
}

func (s *PromptService) List() ([]core.Prompt, error) {
	return s.repo.List()
}

func (s *PromptService) Update(id uint, title, content, category string) (*core.Prompt, error) {
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
