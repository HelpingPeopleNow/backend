package ports

import (
	"context"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

type ProfileRepository interface {
	GetWorkerProfile(ctx context.Context, userID string) (*core.WorkerProfile, error)
	UpsertWorkerProfile(ctx context.Context, userID string, fields map[string]interface{}) error
	DeleteWorkerProfile(ctx context.Context, userID string) error

	GetClientProfile(ctx context.Context, userID string) (*core.ClientProfile, error)
	UpsertClientProfile(ctx context.Context, userID string, fields map[string]interface{}) error
	DeleteClientProfile(ctx context.Context, userID string) error

	FindWorkers(ctx context.Context, filters core.WorkerSearchFilters) ([]core.WorkerProfile, error)
}
