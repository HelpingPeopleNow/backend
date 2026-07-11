package repository

import (
	"context"
	"log/slog"
	"sync"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
	"gorm.io/gorm"
)

type GormSystemPromptRepository struct {
	db    *gorm.DB
	mu    sync.RWMutex
	cache *core.SystemPrompt
}

func NewGormSystemPromptRepository(db *gorm.DB) ports.SystemPromptRepository {
	return &GormSystemPromptRepository{db: db}
}

func (r *GormSystemPromptRepository) Get(ctx context.Context) (*core.SystemPrompt, error) {
	r.mu.RLock()
	if r.cache != nil {
		defer r.mu.RUnlock()
		return r.cache, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cache != nil {
		return r.cache, nil
	}

	slog.Debug("system-prompt: cache miss, loading from DB")
	var sp core.SystemPrompt
	if err := r.db.WithContext(ctx).First(&sp).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			empty := &core.SystemPrompt{}
			r.cache = empty
			return empty, nil
		}
		return nil, err
	}
	r.cache = &sp
	return r.cache, nil
}

func (r *GormSystemPromptRepository) Update(ctx context.Context, column string, value string) (*core.SystemPrompt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var sp core.SystemPrompt
	if err := r.db.WithContext(ctx).FirstOrCreate(&sp).Error; err != nil {
		return nil, err
	}

	updates := map[string]interface{}{
		column:       value,
		"updated_at": gorm.Expr("NOW()"),
	}
	if err := r.db.WithContext(ctx).Model(&sp).Updates(updates).Error; err != nil {
		return nil, err
	}

	var refreshed core.SystemPrompt
	if err := r.db.WithContext(ctx).First(&refreshed).Error; err != nil {
		return nil, err
	}
	r.cache = &refreshed
	slog.Info("system-prompt: updated", "column", column)
	return r.cache, nil
}
