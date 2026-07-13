package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
	"github.com/pgvector/pgvector-go"
	gormpkg "gorm.io/gorm"
)

// Ensure GormProfileRepository implements both ProfileRepository and
// RawQuerier (added by P2 / third-pass review).
var (
	_ ports.ProfileRepository = (*GormProfileRepository)(nil)
	_ ports.RawQuerier        = (*GormProfileRepository)(nil)
)

// RawQuery exposes the underlying *gorm.DB to callers that need a generic
// raw SQL hook (e.g. SearchService.currentWorkerFloor). The result is a
// *gorm.DB — callers use .Scan, .Find, .Error directly, like the inline
// pattern in findWorkersVector. Returns nil rather than crashing if the
// underlying db is somehow nil (defensive — protects against accidental
// double-Connect failures).
func (r *GormProfileRepository) RawQuery(ctx context.Context, sql string, values ...interface{}) *gormpkg.DB {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Raw(sql, values...)
}

type GormProfileRepository struct {
	db *gormpkg.DB
}

func NewGormProfileRepository(db *gormpkg.DB) ports.ProfileRepository {
	return &GormProfileRepository{db: db}
}

func (r *GormProfileRepository) GetWorkerProfile(ctx context.Context, userID string) (*core.WorkerProfile, error) {
	var wp core.WorkerProfile
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&wp).Error
	if err != nil {
		if errors.Is(err, gormpkg.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &wp, nil
}

func (r *GormProfileRepository) GetWorkerProfileByID(ctx context.Context, profileID string) (*core.WorkerProfile, error) {
	var wp core.WorkerProfile
	err := r.db.WithContext(ctx).Where("id = ?", profileID).First(&wp).Error
	if err != nil {
		if errors.Is(err, gormpkg.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &wp, nil
}

func (r *GormProfileRepository) GetUserEmail(ctx context.Context, userID string) (string, error) {
	var email string
	err := r.db.WithContext(ctx).Table("\"user\"").Select("email").Where("id = ?", userID).Scan(&email).Error
	if err != nil {
		if errors.Is(err, gormpkg.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return email, nil
}

func (r *GormProfileRepository) FindBySlug(ctx context.Context, slug string) (*core.WorkerProfile, error) {
	var wp core.WorkerProfile
	err := r.db.WithContext(ctx).Where("slug = ?", slug).First(&wp).Error
	if err != nil {
		if errors.Is(err, gormpkg.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &wp, nil
}

func (r *GormProfileRepository) FindLatestWithSlug(ctx context.Context, limit int) ([]core.WorkerProfile, error) {
	var workers []core.WorkerProfile
	err := r.db.WithContext(ctx).
		Where("slug IS NOT NULL AND slug != ''").
		Order("GREATEST(updated_at, created_at) DESC").
		Limit(limit).
		Find(&workers).Error
	return workers, err
}

func (r *GormProfileRepository) UpsertWorkerProfile(ctx context.Context, userID string, fields map[string]interface{}) error {
	var existing core.WorkerProfile
	found := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&existing).Error == nil
	wp := existing
	if !found {
		wp = core.WorkerProfile{UserID: userID}
	}

	wp.MergeFields(fields)

	// Always generate slug if missing. Priority: business name → profession+city+shortID.
	if wp.Slug == "" {
		slug := core.GenerateSlug(wp.BusinessName)
		if slug == "" {
			shortID := wp.ID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			} else if shortID == "" {
				var buf [4]byte
				if _, err := rand.Read(buf[:]); err == nil {
					shortID = hex.EncodeToString(buf[:])
				}
			}
			slug = fmt.Sprintf("%s-%s-%s",
				core.Slugify(wp.Profession),
				core.Slugify(wp.City),
				shortID,
			)
		}
		baseSlug := slug
		for i := 2; i <= 1000; i++ {
			var existing core.WorkerProfile
			taken := r.db.WithContext(ctx).Where("slug = ?", slug).First(&existing).Error == nil
			if !taken {
				break
			}
			slug = fmt.Sprintf("%s-%d", baseSlug, i)
		}
		wp.Slug = slug
	}

	if found {
		if err := r.db.WithContext(ctx).Save(&wp).Error; err != nil {
			return fmt.Errorf("save worker profile: %w", err)
		}
		slog.Info("repository: worker profile saved", "user_id", userID, "profession", wp.Profession)
	} else {
		if err := r.db.WithContext(ctx).Create(&wp).Error; err != nil {
			return fmt.Errorf("create worker profile: %w", err)
		}
		slog.Info("repository: worker profile created", "user_id", userID, "profession", wp.Profession)
	}
	return nil
}

func (r *GormProfileRepository) DeleteWorkerProfile(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&core.WorkerProfile{}).Error
}

func (r *GormProfileRepository) GetClientProfile(ctx context.Context, userID string) (*core.ClientProfile, error) {
	var cp core.ClientProfile
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&cp).Error
	if err != nil {
		if errors.Is(err, gormpkg.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &cp, nil
}

func (r *GormProfileRepository) UpsertClientProfile(ctx context.Context, userID string, fields map[string]interface{}) error {
	var existing core.ClientProfile
	found := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&existing).Error == nil
	cp := existing
	if !found {
		cp = core.ClientProfile{UserID: userID}
	}

	cp.MergeFields(fields)

	if found {
		if err := r.db.WithContext(ctx).Save(&cp).Error; err != nil {
			return fmt.Errorf("save client profile: %w", err)
		}
		slog.Info("repository: client profile saved", "user_id", userID, "full_name", cp.FullName)
	} else {
		if err := r.db.WithContext(ctx).Create(&cp).Error; err != nil {
			return fmt.Errorf("create client profile: %w", err)
		}
		slog.Info("repository: client profile created", "user_id", userID, "full_name", cp.FullName)
	}
	return nil
}

func (r *GormProfileRepository) DeleteClientProfile(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&core.ClientProfile{}).Error
}

// FindWorkers branches ILIKE vs vector per VECTOR_SEARCH_PLAN §8.7.
// Returns FindResult so SearchService sees the ACTUAL branch used, not
// just the intent.
//
//   - VECTOR_SEARCH_ENABLED=false → force ILIKE (branch=ilike_disabled_via_env).
//   - QueryVector empty → ILIKE only (branch=ilike).
//   - findWorkersVector error OR no rows → degrade to ILIKE (branch=ilike_fallback).
//   - findWorkersVector success → return vector result with TopScore.
//
// VECTOR_SEARCH_MIN_TOP_SCORE (env var, default 0.5) is wired below:
// when the vector branch's top_score is below the threshold, FindWorkers
// falls back to ILIKE with branch="ilike_low_top_score".
func (r *GormProfileRepository) FindWorkers(ctx context.Context, filters core.WorkerSearchFilters) (ports.FindResult, error) {
	hasCoords := filters.Latitude != nil && filters.Longitude != nil
	var workers []core.WorkerProfile
	var branch string
	var topScore float64

	if !vectorSearchEnabled() {
		w, err := r.findWorkersILIKE(ctx, filters)
		if err != nil {
			return ports.FindResult{}, err
		}
		workers, branch, topScore = w, "ilike_disabled_via_env", 0
	} else if filters.EmbedFailed {
		// F4: embed failure — distinct branch so outage is visible in metrics
		w, err := r.findWorkersILIKE(ctx, filters)
		if err != nil {
			return ports.FindResult{}, err
		}
		workers, branch, topScore = w, "ilike_embed_failed", 0
	} else if len(filters.QueryVector) == 0 {
		w, err := r.findWorkersILIKE(ctx, filters)
		if err != nil {
			return ports.FindResult{}, err
		}
		workers, branch, topScore = w, "ilike", 0
	} else {
		w, sc, err := r.findWorkersVector(ctx, filters)
		if err != nil || len(w) == 0 {
			if err != nil {
				slog.Warn("repository: vector search failed, falling back to ILIKE", "error", err)
			}
			w2, err2 := r.findWorkersILIKE(ctx, filters)
			if err2 != nil {
				return ports.FindResult{}, err2
			}
			workers, branch, topScore = w2, "ilike_fallback", 0
		} else {
			workers, branch, topScore = w, "vector", sc
		}
	}

	// F7: wire VECTOR_SEARCH_MIN_TOP_SCORE — if vector score is below
	// the threshold, fall back to ILIKE so weak matches don't surface
	// as confident results.
	if branch == "vector" && topScore > 0 {
		minTopScore := 0.5
		if v := os.Getenv("VECTOR_SEARCH_MIN_TOP_SCORE"); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
				minTopScore = f
			}
		}
		if topScore < minTopScore {
			slog.Info("repository: vector top_score below threshold, falling back to ILIKE",
				"top_score", topScore, "min", minTopScore)
			w2, err2 := r.findWorkersILIKE(ctx, filters)
			if err2 != nil {
				return ports.FindResult{}, err2
			}
			workers, branch, topScore = w2, "ilike_low_top_score", 0
		}
	}

	// ── F14/F15 fix: unified distance computation + filtering ──
	//
	// For ILIKE results: DistanceKm is already populated by the SQL
	// Haversine expression (SELECT haversine(...) AS distance_km).
	// For vector results: DistanceKm is NULL; compute it here.
	if hasCoords {
		lat, lng := *filters.Latitude, *filters.Longitude
		for i := range workers {
			if workers[i].DistanceKm == nil && workers[i].Latitude != nil && workers[i].Longitude != nil {
				d := core.HaversineKm(lat, lng, *workers[i].Latitude, *workers[i].Longitude)
				workers[i].DistanceKm = &d
			}
		}
	}
	// F15: honor worker-declared service_radius_km — exclude workers
	// whose service area does not cover the client's GPS location.
	// Applied before MaxDistanceKm so both constraints narrow the set.
	if hasCoords {
		lat, lng := *filters.Latitude, *filters.Longitude
		filtered := workers[:0]
		for _, w := range workers {
			if w.Latitude != nil && w.Longitude != nil && w.ServiceRadiusKm > 0 {
				dist := core.HaversineKm(lat, lng, *w.Latitude, *w.Longitude)
				if dist > float64(w.ServiceRadiusKm) {
					continue // worker's service area doesn't reach client
				}
			}
			filtered = append(filtered, w)
		}
		workers = filtered
	}
	// F14: MaxDistanceKm filter — after service_radius_km narrowing.
	if hasCoords && filters.MaxDistanceKm != nil && *filters.MaxDistanceKm > 0 {
		maxD := float64(*filters.MaxDistanceKm)
		filtered := workers[:0]
		for _, w := range workers {
			if w.DistanceKm != nil && *w.DistanceKm <= maxD {
				filtered = append(filtered, w)
			} else if w.DistanceKm == nil {
				filtered = append(filtered, w) // keep unknown-distance workers
			}
		}
		workers = filtered
	}
	// F14: sort by distance (nearest first) — vector branch needs this;
	// ILIKE branch already ordered by distance_km in SQL but we re-sort
	// to handle workers with nil coords at the tail.
	if hasCoords {
		sort.Slice(workers, func(i, j int) bool {
			if workers[i].DistanceKm == nil {
				return false
			}
			if workers[j].DistanceKm == nil {
				return true
			}
			return *workers[i].DistanceKm < *workers[j].DistanceKm
		})
	}

	return ports.FindResult{Workers: workers, Branch: branch, TopScore: topScore}, nil
}

func vectorSearchEnabled() bool {
	return core.GetEnvBool("VECTOR_SEARCH_ENABLED", true)
}

func (r *GormProfileRepository) findWorkersILIKE(ctx context.Context, filters core.WorkerSearchFilters) ([]core.WorkerProfile, error) {
	hasCoords := filters.Latitude != nil && filters.Longitude != nil
	query := r.db.WithContext(ctx).Model(&core.WorkerProfile{})

	if filters.Profession != "" {
		query = query.Where("profession ILIKE ?", "%"+filters.Profession+"%")
	}
	if filters.City != "" {
		query = query.Where("city ILIKE ?", "%"+filters.City+"%")
	}
	if filters.EmergencyOnly {
		query = query.Where("emergency_service = true")
	}
	if filters.FreeEstimateOnly {
		query = query.Where("free_estimate = true")
	}
	if filters.InsuredOnly {
		query = query.Where("has_insurance = true")
	}

	if hasCoords {
		// F14 fix: Haversine distance computed in SQL and used for ordering.
		// MaxDistanceKm filter is NOT pushed to SQL here — workers without
		// coords must still be included in results. Filtering happens in
		// FindWorkers after both branches (ILIKE/vector) return.
		haversineExpr := `
			(6371 * acos(
				cos(radians(?)) * cos(radians(latitude)) *
				cos(radians(longitude) - radians(?)) +
				sin(radians(?)) * sin(radians(latitude))
			))`
		lat, lng := *filters.Latitude, *filters.Longitude
		query = query.Select("*, "+haversineExpr+" AS distance_km", lat, lng, lat)
		query = query.Order("distance_km ASC")
	} else if filters.City != "" {
		query = query.Order(gormpkg.Expr("CASE WHEN LOWER(city) = LOWER(?) THEN 0 ELSE 1 END, created_at DESC", filters.City))
	} else {
		query = query.Order("created_at DESC")
	}

	query = query.Limit(50)

	var workers []core.WorkerProfile
	if err := query.Find(&workers).Error; err != nil {
		return nil, err
	}
	return workers, nil
}

// findWorkersVector — Per-field hybrid KNN query.
// Pitfall #1: backtick raw string with real newlines (no concat).
// Pitfall #5 Phase A: boolean filters pushed into the inner subquery's WHERE.
// Pitfall #4: minCosine is env-wired via VECTOR_SEARCH_MIN_SCORE.
// Idea B kill switch + N1 score-gate live in FindWorkers() above.
//
// Returns (workers, topScore, err). topScore is the max_cosine of the
// highest-ranked worker in the result set, used by SearchService for the
// Idea C structured slog (`hpn_vector_search_top_score` histogram in
// Prometheus scraping — wired via ObserveVectorScore).
func (r *GormProfileRepository) findWorkersVector(ctx context.Context, filters core.WorkerSearchFilters) ([]core.WorkerProfile, float64, error) {
	if len(filters.QueryVector) == 0 {
		return nil, 0, fmt.Errorf("findWorkersVector called without QueryVector")
	}

	candidateLimit := 200
	minCosine := core.GetEnvFloat("VECTOR_SEARCH_MIN_SCORE", 0.3)

	boolWhere := ""
	if filters.EmergencyOnly {
		boolWhere += " AND wp.emergency_service = true"
	}
	if filters.FreeEstimateOnly {
		boolWhere += " AND wp.free_estimate = true"
	}
	if filters.InsuredOnly {
		boolWhere += " AND wp.has_insurance = true"
	}

	// weightExpr is interpolated INTO the `scored` CTE (FROM knn). At that
	// scope only the `knn` alias exists; `we` (worker_embeddings) is bound
	// inside the preceding `knn` CTE block. Referencing we.field_name here
	// produced "ERROR: missing FROM-clause entry for table \"we\"" (SQLSTATE
	// 42P01) and silently flipped the search to branch=ilike_fallback.
	weightExpr := `
		CASE knn.field_name
			WHEN 'profession'     THEN 1.0
			WHEN 'profession_raw' THEN 0.3
			WHEN 'bio'            THEN 0.8
			WHEN 'certifications' THEN 0.7
			WHEN 'city'           THEN 0.4
			WHEN 'languages'      THEN 0.3
			WHEN 'business_name'  THEN 0.3
			ELSE 0.0
		END`

	rawSQL := fmt.Sprintf(`
		WITH knn AS (
			SELECT
				we.user_id         AS user_id,
				we.field_name      AS field_name,
				we.embedding       AS embedding,
				(1 - (we.embedding <=> $3::vector)) AS cosine
			FROM worker_embeddings we
			JOIN worker_profiles wp ON wp.user_id = we.user_id
			WHERE TRUE %s
			ORDER BY we.embedding <=> $3::vector
			LIMIT $1
		),
		scored AS (
			SELECT
				knn.user_id,
				SUM((%s) * knn.cosine) / NULLIF(SUM(%s), 0) AS weighted_avg,
				MAX(knn.cosine) AS max_cosine
			FROM knn
			GROUP BY knn.user_id
			HAVING MAX(knn.cosine) > $2
		)
		SELECT wp.*, scored.max_cosine AS top_score
		FROM worker_profiles wp
		JOIN scored ON scored.user_id = wp.user_id
		ORDER BY scored.weighted_avg DESC, scored.max_cosine DESC, wp.created_at DESC
		LIMIT 50
	`, boolWhere, weightExpr, weightExpr)

	// Transient struct because the vector query needs the per-row top_score
	// alongside the worker profile columns. We can scan into a struct that
	// embeds core.WorkerProfile + Score.
	type scoredRow struct {
		core.WorkerProfile
		TopScore float64 `gorm:"column:top_score"`
	}
	var rows []scoredRow
	qvec := pgvector.NewVector(filters.QueryVector)
	if err := r.db.WithContext(ctx).Raw(rawSQL, candidateLimit, minCosine, qvec).Scan(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("vector raw query: %w", err)
	}

	workers := make([]core.WorkerProfile, len(rows))
	var topScore float64
	for i, r := range rows {
		workers[i] = r.WorkerProfile
		if r.TopScore > topScore {
			topScore = r.TopScore
		}
	}
	return workers, topScore, nil
}

// ── Worker embedding row CRUD ────────────────────────────────────

func (r *GormProfileRepository) UpsertWorkerEmbedding(
	ctx context.Context,
	userID, fieldName string,
	embedding []float32,
	textHash string,
) error {
	if len(embedding) == 0 {
		return fmt.Errorf("UpsertWorkerEmbedding: empty embedding vector")
	}
	// timestamptz — Plan showstopper #2 fix. Pairs with the struct
	// change in core/worker_embeddings.go so the column type matches
	// worker_profiles.updated_at for the staleness sweep comparison.
	now := time.Now()

	// Improvement #11: never split this into a separate UPDATE. The
	// INSERT ... ON CONFLICT ... DO UPDATE statement is atomic per row.
	const upsertSQL = `
		INSERT INTO worker_embeddings
			(user_id, field_name, embedding, model, text_hash, created_at, updated_at)
		VALUES
			($1, $2, $3::vector, 'granite-embedding:278m', $4, $5, $5)
		ON CONFLICT (user_id, field_name)
		DO UPDATE SET
			embedding  = EXCLUDED.embedding,
			model      = EXCLUDED.model,
			text_hash  = EXCLUDED.text_hash,
			updated_at = EXCLUDED.updated_at
		WHERE worker_embeddings.text_hash <> EXCLUDED.text_hash
		   OR worker_embeddings.model     <> EXCLUDED.model
	`
	if err := r.db.WithContext(ctx).Exec(
		upsertSQL,
		userID, fieldName, pgvector.NewVector(embedding), textHash, now,
	).Error; err != nil {
		return fmt.Errorf("upsert worker_embedding: %w", err)
	}
	return nil
}

func (r *GormProfileRepository) GetWorkerEmbeddingHashes(
	ctx context.Context,
	userID string,
) (map[string]ports.EmbeddingMeta, error) {
	out := make(map[string]ports.EmbeddingMeta)
	const q = `SELECT field_name, text_hash, model FROM worker_embeddings WHERE user_id = $1`
	type row struct {
		FieldName string `gorm:"column:field_name"`
		TextHash  string `gorm:"column:text_hash"`
		Model     string `gorm:"column:model"`
	}
	var rows []row
	if err := r.db.WithContext(ctx).Raw(q, userID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("get worker_embedding_hashes: %w", err)
	}
	for _, row := range rows {
		out[row.FieldName] = ports.EmbeddingMeta{
			Hash:  row.TextHash,
			Model: row.Model,
		}
	}
	return out, nil
}

func (r *GormProfileRepository) DeleteWorkerEmbedding(
	ctx context.Context,
	userID, fieldName string,
) error {
	return r.db.WithContext(ctx).
		Exec(`DELETE FROM worker_embeddings WHERE user_id = $1 AND field_name = $2`,
			userID, fieldName).Error
}

// FindStaleWorkerIDs identifies workers whose profile updated_at is newer
// than any embedding's updated_at — these need a re-embed sweep.
//
// Both timestamps are timestamptz now (Plan showstopper #2 fix), so the
// SQL `wp.updated_at > MAX(we.updated_at)` comparison is type-correct
// without explicit casting.
func (r *GormProfileRepository) FindStaleWorkerIDs(ctx context.Context) ([]string, error) {
	const q = `
		SELECT wp.user_id
		FROM worker_profiles wp
		LEFT JOIN worker_embeddings we ON we.user_id = wp.user_id
		GROUP BY wp.user_id, wp.updated_at
		HAVING MAX(we.updated_at) IS NULL
		    OR wp.updated_at > MAX(we.updated_at)
		LIMIT 50
	`
	var ids []string
	if err := r.db.WithContext(ctx).Raw(q).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("find stale worker IDs: %w", err)
	}
	return ids, nil
}
