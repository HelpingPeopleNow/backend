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
	db          *gorm.DB
	mu          sync.RWMutex
	cache       *core.SystemPrompt
	invalidator ports.SystemPromptInvalidator
}

func NewGormSystemPromptRepository(db *gorm.DB) ports.SystemPromptRepository {
	return &GormSystemPromptRepository{db: db}
}

// SetInvalidator wires the cross-replica invalidation publisher. Called
// from main.buildMux after the Redis-backed invalidator is constructed.
// nil is allowed (single-replica / dev): Update then skips the publish.
func (r *GormSystemPromptRepository) SetInvalidator(inv ports.SystemPromptInvalidator) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.invalidator = inv
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

	// SPOF GAP B: notify sibling replicas so their in-memory caches
	// reload. Failure here is non-fatal — the local cache is already
	// current; a missed pub/sub event just leaves sibling caches stale
	// until the next manual restart or a TTL fallback.
	if r.invalidator != nil {
		if err := r.invalidator.PublishInvalidation(ctx, column); err != nil {
			slog.Warn("system-prompt: cross-replica invalidation publish failed", "column", column, "error", err)
		}
	}
	return r.cache, nil
}

// InvalidateCache reloads the singleton row from the DB into the
// in-memory cache. Called by the prompt-invalidator goroutine when it
// receives a cross-replica invalidation event.
func (r *GormSystemPromptRepository) InvalidateCache(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var refreshed core.SystemPrompt
	if err := r.db.WithContext(ctx).First(&refreshed).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			r.cache = &core.SystemPrompt{}
			return nil
		}
		return err
	}
	r.cache = &refreshed
	slog.Info("system-prompt: cache invalidated by cross-replica event")
	return nil
}
