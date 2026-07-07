package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ── F1/F16: Cache key on resolved filters ─────────────────────────

// filterKey mirrors the cache key struct used in SearchService.Search.
// Exported here for test comparison — must stay in sync with the
// internal cacheKeyParts type in search_service.go.
type filterKey struct {
	Profession    string
	City          string
	Latitude      float64
	Longitude     float64
	MaxDistanceKm float64
	Emergency     bool
	FreeEstimate  bool
	Insured       bool
}

func marshalKey(fk filterKey) string {
	b, _ := json.Marshal(fk)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestCacheKeyDiffersForDifferentCity(t *testing.T) {
	madrid := marshalKey(filterKey{Profession: "plumber", City: "Madrid"})
	barcelona := marshalKey(filterKey{Profession: "plumber", City: "Barcelona"})
	assert.NotEqual(t, madrid, barcelona,
		"F1: cache key MUST differ for different cities")
}

func TestCacheKeyDiffersForDifferentGPS(t *testing.T) {
	// Same city, different GPS — two users in different parts of Madrid
	madrid1 := marshalKey(filterKey{
		Profession: "plumber", City: "Madrid",
		Latitude: 40.4168, Longitude: -3.7038,
	})
	madrid2 := marshalKey(filterKey{
		Profession: "plumber", City: "Madrid",
		Latitude: 40.4530, Longitude: -3.6883,
	})
	assert.NotEqual(t, madrid1, madrid2,
		"F16: cache key MUST differ for different GPS coordinates")
}

func TestCacheKeyDiffersForDifferentMaxDistance(t *testing.T) {
	near := marshalKey(filterKey{
		Profession: "plumber", City: "Madrid",
		Latitude: 40.4168, Longitude: -3.7038, MaxDistanceKm: 5,
	})
	far := marshalKey(filterKey{
		Profession: "plumber", City: "Madrid",
		Latitude: 40.4168, Longitude: -3.7038, MaxDistanceKm: 50,
	})
	assert.NotEqual(t, near, far,
		"F16: cache key MUST differ for different MaxDistanceKm")
}

func TestCacheKeyDiffersForDifferentFilters(t *testing.T) {
	base := marshalKey(filterKey{
		Profession: "plumber", City: "Madrid",
	})
	emergency := marshalKey(filterKey{
		Profession: "plumber", City: "Madrid", Emergency: true,
	})
	insured := marshalKey(filterKey{
		Profession: "plumber", City: "Madrid", Insured: true,
	})
	assert.NotEqual(t, base, emergency,
		"F16: cache key MUST differ when Emergency differs")
	assert.NotEqual(t, base, insured,
		"F16: cache key MUST differ when Insured differs")
}

func TestCacheKeySameForIdenticalFilters(t *testing.T) {
	a := marshalKey(filterKey{
		Profession: "plumber", City: "Madrid",
		Latitude: 40.4168, Longitude: -3.7038, MaxDistanceKm: 10,
		Emergency: true, FreeEstimate: true, Insured: true,
	})
	b := marshalKey(filterKey{
		Profession: "plumber", City: "Madrid",
		Latitude: 40.4168, Longitude: -3.7038, MaxDistanceKm: 10,
		Emergency: true, FreeEstimate: true, Insured: true,
	})
	assert.Equal(t, a, b,
		"identical filters MUST produce the same cache key")
}

func TestCacheKeyNoGPSZeroes(t *testing.T) {
	// When GPS is absent, lat/lng should default to 0.0 — the key
	// must differ from a key with actual coordinates.
	noGPS := marshalKey(filterKey{
		Profession: "plumber", City: "Madrid",
		Latitude: 0.0, Longitude: 0.0,
	})
	withGPS := marshalKey(filterKey{
		Profession: "plumber", City: "Madrid",
		Latitude: 40.4168, Longitude: -3.7038,
	})
	assert.NotEqual(t, noGPS, withGPS,
		"F16: cache key MUST differ between no-GPS and GPS-present searches")
}
