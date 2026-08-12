package ports

import (
	"context"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

// SystemPromptRepository is the singleton-row cache + persistence
// interface. The repo is process-local: when N backend replicas serve
// concurrently, an admin `PUT /api/v1/system-prompts/<col>` only updates
// the cache of the replica that received the request (SPOF GAP B).
// Cross-replica invalidation is handled by an injected Publisher (see
// internal/adapters/realtime/prompt_invalidator.go) that fans the
// invalidation out over Redis pub/sub.
type SystemPromptRepository interface {
	Get(ctx context.Context) (*core.SystemPrompt, error)
	Update(ctx context.Context, column string, value string) (*core.SystemPrompt, error)
	// InvalidateCache reloads the in-memory cache from the DB. Called
	// by the invalidator goroutine on receipt of a cross-replica
	// invalidation event. Safe for concurrent calls.
	InvalidateCache(ctx context.Context) error
}

// SystemPromptInvalidator is the cross-replica invalidation channel
// surface. The implementation is a Redis pub/sub publisher
// (see internal/adapters/realtime/prompt_invalidator.go). Repos
// delegate to it from Update so every PUT notifies sibling replicas.
type SystemPromptInvalidator interface {
	PublishInvalidation(ctx context.Context, column string) error
}
