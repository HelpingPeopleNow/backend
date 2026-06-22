package ports

import (
	"context"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

// EmbeddingMeta describes a hashed embedding row + the model that produced
// it. Defined as a Go type ALIAS (type X = struct{…}) rather than a defined
// type (type X struct{…}) so chat_id_test.go's mockProfiles can use an
// anonymous-struct signature that is byte-identical to this. A defined type
// would NOT be structurally interchangeable with the anon-struct form and
// would break compilation of the test mock.
type EmbeddingMeta = struct {
	Hash  string
	Model string
}

type ProfileRepository interface {
	GetWorkerProfile(ctx context.Context, userID string) (*core.WorkerProfile, error)
	UpsertWorkerProfile(ctx context.Context, userID string, fields map[string]interface{}) error
	DeleteWorkerProfile(ctx context.Context, userID string) error

	GetClientProfile(ctx context.Context, userID string) (*core.ClientProfile, error)
	UpsertClientProfile(ctx context.Context, userID string, fields map[string]interface{}) error
	DeleteClientProfile(ctx context.Context, userID string) error

	FindWorkers(ctx context.Context, filters core.WorkerSearchFilters) ([]core.WorkerProfile, error)

	// Worker embeddings (vector search) — see Improvements #1, #2 and §8.8
	// in infra/docs/VECTOR_SEARCH_PLAN.md. Stub implementations are live on
	// GormProfileRepository; real SQL lands when Improvement #3 (worker_
	// embeddings table via GORM AutoMigrate) ships.
	UpsertWorkerEmbedding(ctx context.Context, userID, fieldName string, embedding []float32, textHash string) error
	GetWorkerEmbeddingHashes(ctx context.Context, userID string) (map[string]EmbeddingMeta, error)
	DeleteWorkerEmbedding(ctx context.Context, userID, fieldName string) error
	FindStaleWorkerIDs(ctx context.Context) ([]string, error)
}
