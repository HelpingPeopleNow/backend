package ports

import (
	"context"

	"github.com/HelpingPeopleNow/backend/internal/core"

	"gorm.io/gorm"
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

// RawQuerier is a tiny extension to ProfileRepository giving callers
// (currently SearchService's currentWorkerFloor) a generic gorm-style raw
// query hook without leaking *gorm.DB into the service layer.
//
// Used by P2 (third-pass review) cache invalidation — we need
// SELECT MAX(updated_at) FROM worker_profiles via the same DB session
// the services already hold, but services can't hold *gorm.DB per
// the hexagonal rules.
type RawQuerier interface {
	RawQuery(ctx context.Context, sql string, values ...interface{}) *gorm.DB
}

// FindResult is what FindWorkers returns so the caller (SearchService) can
// know WHICH branch the repository ACTUALLY used — not just the intent.
//
// VECTOR_SEARCH_PLAN §8.7 / fourth-pass review item #2: SearchService.Search
// used to log `branch` set BEFORE FindWorkers ran, so a vector→ILIE fallback
// log line would silently lie. FindResult.Branch is the truth post-fact.
type FindResult struct {
	Workers  []core.WorkerProfile
	Branch   string  // "vector" | "ilike" | "ilike_disabled_via_env" | "ilike_fallback"
	TopScore float64 // Best max_cosine across the result set; 0 if branch != "vector".
}

type ProfileRepository interface {
	// RawQuerier is embedded so SearchService.currentWorkerFloor (P2)
	// can call s.profiles.RawQuery(...) without a separate dependency.
	// The mockProfiles in chat_id_test.go gains a matching stub.
	RawQuerier

	GetWorkerProfile(ctx context.Context, userID string) (*core.WorkerProfile, error)
	GetWorkerProfileByID(ctx context.Context, profileID string) (*core.WorkerProfile, error)
	UpsertWorkerProfile(ctx context.Context, userID string, fields map[string]interface{}) error
	DeleteWorkerProfile(ctx context.Context, userID string) error

	GetClientProfile(ctx context.Context, userID string) (*core.ClientProfile, error)
	UpsertClientProfile(ctx context.Context, userID string, fields map[string]interface{}) error
	DeleteClientProfile(ctx context.Context, userID string) error

	FindWorkers(ctx context.Context, filters core.WorkerSearchFilters) (FindResult, error)

	GetUserEmail(ctx context.Context, userID string) (string, error)

	FindBySlug(ctx context.Context, slug string) (*core.WorkerProfile, error)
	FindLatestWithSlug(ctx context.Context, limit int) ([]core.WorkerProfile, error)

	// Worker embeddings (vector search) — deferred per-user debounce,
	// model-bound hash skip, staleness sweeper, all in IntakeService.
	UpsertWorkerEmbedding(ctx context.Context, userID, fieldName string, embedding []float32, textHash string) error
	GetWorkerEmbeddingHashes(ctx context.Context, userID string) (map[string]EmbeddingMeta, error)
	DeleteWorkerEmbedding(ctx context.Context, userID, fieldName string) error
	FindStaleWorkerIDs(ctx context.Context) ([]string, error)
}
