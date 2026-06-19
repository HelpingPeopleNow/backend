package ports

import (
	"context"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

type SystemPromptRepository interface {
	Get(ctx context.Context) (*core.SystemPrompt, error)
	Update(ctx context.Context, column string, value string) (*core.SystemPrompt, error)
}
