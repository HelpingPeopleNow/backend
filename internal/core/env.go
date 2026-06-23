package core

import (
	"log/slog"
	"os"
	"strconv"
)

// GetEnvFloat reads a float64 from an environment variable key, falling
// back to fallback if absent or invalid. Invalid values log a warning so
// operators notice typos rather than silently hitting the fallback.
//
// Used by findWorkersVector (VECTOR_SEARCH_PLAN §8.7) for
//   - VECTOR_SEARCH_MIN_SCORE       (Pitfall #4 fix: was hardcoded)
//   - VECTOR_SEARCH_MIN_TOP_SCORE   (Idea B / N1: top score gate)
func GetEnvFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		slog.Warn("env: invalid float, using fallback",
			"key", key, "value", v, "error", err, "fallback", fallback)
		return fallback
	}
	return f
}

// GetEnvBool reads a boolean via standard truthy strings
// ("1", "true", "yes", case-insensitive). Anything else is false.
//
// VECTOR_SEARCH_PLAN §8.7 Idea B kill switch.
func GetEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	switch v {
	case "1", "true", "TRUE", "True", "yes", "YES", "Yes":
		return true
	case "0", "false", "FALSE", "False", "no", "NO", "No":
		return false
	default:
		slog.Warn("env: invalid bool, using fallback",
			"key", key, "value", v, "fallback", fallback)
		return fallback
	}
}
