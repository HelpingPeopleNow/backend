package core

// WorkerSearchFilters is the parameter struct passed from SearchService to
// ProfileRepository.FindWorkers. Extended for vector search per
// VECTOR_SEARCH_PLAN §8.6: QueryVector carries the 768-dim vector produced
// by llm.Embed (real-time per search request).
//
// GPS precedence (highest → lowest):
//  1. Request latitude/longitude (browser geolocation, most current)
//  2. Stored client profile latitude/longitude
//  3. nil → no distance sorting, no proximity filter
//
// City precedence (highest → lowest):
//  1. City extracted from the user's message by Pass-1 LLM
//  2. Stored client profile city
//  3. "" → no city filter
//
// MaxDistanceKm is extracted from the Pass-1 LLM JSON when the user
// mentions a maximum distance; 0 or absent means no cap.
type WorkerSearchFilters struct {
	Profession       string
	City             string
	EmergencyOnly    bool
	FreeEstimateOnly bool
	InsuredOnly      bool

	// Latitude/Longitude from the client's GPS — when both are non-nil,
	// the repository adds Haversine distance to results and orders by
	// nearest first. Falls back to city-text matching when absent.
	// Source precedence is documented above.
	Latitude  *float64
	Longitude *float64
	// MaxDistanceKm caps results when coordinates are provided.
	// 0 means no cap (all workers returned, sorted by distance).
	MaxDistanceKm *int

	// QueryVector is populated by SearchService.Search after Pass 1
	// extracts the search params and we Embed() the raw user message.
	// Repository detects non-nil and switches to the vector branch.
	// Length MUST equal the configured embedding model dim (768 for
	// granite-embedding:278m); a mismatch is treated as a hard error
	// because persisting or comparing mismatched-dim vectors is a
	// silent-failure trap.
	QueryVector []float32

	// F4: EmbedFailed is set by SearchService when the Embed call fails.
	// FindWorkers returns branch="ilike_embed_failed" instead of plain
	// "ilike" so embedding outages are visible in metrics.
	EmbedFailed bool
}
