package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/pgvector/pgvector-go"
)

// WorkerEmbedding is the per-field embedding row (VECTOR_SEARCH_PLAN §6.2).
// One row per (user_id, field_name) pair. The composite primary key matches
// the §6.2 schema. The embedding column uses pgvector.Vector — its
// GormDataType() returns "vector" with no explicit dimension suffix; the
// pilot migration in postgres.go sets vector(768) explicitly via ALTER/TYPE
// or column-rewrite. If we ever switch embedding models with a different
// dimensionality, bump both the tag and the migration.
type WorkerEmbedding struct {
	UserID    string `gorm:"type:text;primaryKey;not null" json:"user_id"`
	FieldName string `gorm:"type:text;primaryKey;not null" json:"field_name"`

	// pgvector.Vector's GormDataType() returns "vector" for unspecified dims.
	// We pin dimensions via the migration (CREATE TABLE / ALTER COLUMN below)
	// rather than the struct tag (pgvector.Vector doesn't expose a typed tag
	// argument from Go), so an explicit `CREATE TABLE ... embedding vector(768)`
	// keeps the column shape fixed.
	Embedding pgvector.Vector `gorm:"type:vector;not null" json:"-"`

	// Model that produced this row. Improvement #2 binds hash-skip to model
	// so switching models forces a re-embed rather than silently mixing
	// latent spaces.
	Model string `gorm:"type:text;not null;default:'granite-embedding:278m'" json:"model"`

	// SHA-256 hex digest of the field text that was embedded. Used for
	// change detection in reembedWorker.
	TextHash string `gorm:"type:text;not null" json:"text_hash"`

	// Timestamps are timestamptz (paired with worker_profiles.updated_at
	// in the staleness sweeper query — Plan showstopper #2). The custom
	// trigger in database/postgres.go keeps updated_at fresh on UPDATE.
	CreatedAt time.Time `gorm:"type:timestamptz;not null;default:now()" json:"created_at"`
	UpdatedAt time.Time `gorm:"type:timestamptz;not null;default:now()" json:"updated_at"`
}

// TableName pins the table — see §6.2.
func (WorkerEmbedding) TableName() string { return "worker_embeddings" }

// sha256Hex computes a SHA-256 hex digest of text — used by reembedWorker
// for change detection. Always lowercase hex, length 64.
func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// HashField is the exported alias so callers in different packages don't
// need to import an unexported function. Returns the same value as
// sha256Hex — they are deliberately identical.
func HashField(text string) string { return sha256Hex(text) }

// joinJSONArray extracts []string from the JSON-string column the rest of
// the codebase uses for certifications/languages/social_links, then joins
// with ", ". Empty input returns "" (and thus no embedding row produced).
func joinJSONArray(jsonStr string) string {
	if jsonStr == "" {
		return ""
	}
	var arr []string
	if err := json.Unmarshal([]byte(jsonStr), &arr); err != nil {
		// Fall back to raw text — at least the words are embedded.
		return jsonStr
	}
	out := ""
	for i, s := range arr {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

// BuildFieldTexts maps a WorkerProfile to the field name → embedding text
// map consumed by reembedWorker. Idea E (fourth-pass review) applies
// NormalizeProfessionForEmbedding() so the most-searched field has exact
// cosine == 1.0 for "electricista" ↔ "Electrician" pairs. Empty fields
// are excluded entirely (no row produced).
func BuildFieldTexts(wp *WorkerProfile) map[string]string {
	fields := map[string]string{}

	if p := wp.Profession; p != "" {
		normalized := NormalizeProfessionForEmbedding(p)
		fields["profession"] = normalized
		// P5/N7 (third-pass review): keep raw as profession_raw at lower
		// weight so unrecognized professions still rank in vector space.
		if normalized != p {
			fields["profession_raw"] = p
		}
	}
	if wp.Bio != "" {
		fields["bio"] = wp.Bio
	}
	if certs := joinJSONArray(wp.Certifications); certs != "" {
		fields["certifications"] = certs
	}
	if wp.City != "" {
		fields["city"] = wp.City
	}
	if langs := joinJSONArray(wp.Languages); langs != "" {
		fields["languages"] = langs
	}
	if wp.BusinessName != "" {
		fields["business_name"] = wp.BusinessName
	}
	return fields
}

// FieldWeights maps each embeddable field to its contribution weight in
// the hybrid search score. Tuned per VECTOR_SEARCH_PLAN §5.2. Hardcoded
// for V1 (N4 / D of fourth-pass review keeps this in code rather than
// surfacing admin-tunable weights until search volume grows).
var FieldWeights = map[string]float64{
	"profession":     1.0,
	"profession_raw": 0.3,
	"bio":            0.8,
	"certifications": 0.7,
	"city":           0.4,
	"languages":      0.3,
	"business_name":  0.3,
}

// NormalizeProfessionForEmbedding delegates to the shared
// NormalizeProfession so search queries and embedding text canonicalize
// to identical strings. Kept as a separate exported name to avoid
// changing existing callers in worker_embeddings.go and reembed tests.
func NormalizeProfessionForEmbedding(p string) string {
	return NormalizeProfession(p)
}
