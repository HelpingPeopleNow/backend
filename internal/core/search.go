package core

// WorkerSearchFilters is the parameter struct passed from SearchService to
// ProfileRepository.FindWorkers. Extended for vector search per
// VECTOR_SEARCH_PLAN §8.6: QueryVector carries the 768-dim vector produced
// by llm.Embed (real-time per search request).
type WorkerSearchFilters struct {
	Profession       string
	City             string
	EmergencyOnly    bool
	FreeEstimateOnly bool
	InsuredOnly      bool

	// QueryVector is populated by SearchService.Search after Pass 1
	// extracts the search params and we Embed() the raw user message.
	// Repository detects non-nil and switches to the vector branch.
	// Length MUST equal the configured embedding model dim (768 for
	// granite-embedding:278m); a mismatch is treated as a hard error
	// because persisting or comparing mismatched-dim vectors is a
	// silent-failure trap.
	QueryVector []float32
}
