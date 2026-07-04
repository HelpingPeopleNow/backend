package core

import (
	"math"
	"testing"
)

func TestHaversineKm_SamePoint(t *testing.T) {
	d := HaversineKm(40.4168, -3.7038, 40.4168, -3.7038)
	if d != 0 {
		t.Errorf("same point should be 0 km, got %f", d)
	}
}

func TestHaversineKm_MadridToBarcelona(t *testing.T) {
	// Madrid (40.4168, -3.7038) → Barcelona (41.3874, 2.1686)
	// Expected ~505 km
	d := HaversineKm(40.4168, -3.7038, 41.3874, 2.1686)
	if math.Abs(d-505) > 20 {
		t.Errorf("Madrid→Barcelona expected ~505 km, got %f", d)
	}
}

func TestHaversineKm_KnownDistance(t *testing.T) {
	// London (51.5074, -0.1278) → Paris (48.8566, 2.3522)
	// Expected ~343 km
	d := HaversineKm(51.5074, -0.1278, 48.8566, 2.3522)
	if math.Abs(d-343) > 15 {
		t.Errorf("London→Paris expected ~343 km, got %f", d)
	}
}

func TestHaversineKm_Symmetric(t *testing.T) {
	d1 := HaversineKm(40.4168, -3.7038, 41.3874, 2.1686)
	d2 := HaversineKm(41.3874, 2.1686, 40.4168, -3.7038)
	if math.Abs(d1-d2) > 0.001 {
		t.Errorf("Haversine should be symmetric: %f != %f", d1, d2)
	}
}
