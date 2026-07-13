package core

import "strings"

// ProfessionAliases maps lowercase, trimmed aliases to canonical English
// profession names. It is the single source of truth for profession
// normalization across the backend (search query normalization and
// embedding text normalization).
//
// Keep this map in sync with helper/scripts/backfill_embeddings.py so
// batch backfill and runtime queries canonicalize to the same strings.
var ProfessionAliases = map[string]string{
	// Electrician
	"electricista": "Electrician",
	"electrician":  "Electrician",
	"electric":     "Electrician",
	// Plumber
	"fontanero": "Plumber",
	"plomero":   "Plumber",
	"plumber":   "Plumber",
	// Cleaner
	"limpiador":  "Cleaner",
	"limpiadora": "Cleaner",
	"limpieza":   "Cleaner",
	"cleaner":    "Cleaner",
	"cleaning":   "Cleaner",
	// Handyman
	"manitas":   "Handyman",
	"handyman":  "Handyman",
	"handy man": "Handyman",
	// Carpenter
	"carpintero": "Carpenter",
	"carpenter":  "Carpenter",
	// Painter
	"pintor":   "Painter",
	"pintura":  "Painter",
	"painter":  "Painter",
	"painting": "Painter",
	// Landscaper
	"jardinero":  "Landscaper",
	"landscaper": "Landscaper",
	"gardener":   "Landscaper",
	// Roofer
	"tejador": "Roofer",
	"tejado":  "Roofer",
	"techo":   "Roofer",
	"roofer":  "Roofer",
	"roofing": "Roofer",
	// HVAC Technician
	"clima":              "HVAC Technician",
	"aire acondicionado": "HVAC Technician",
	"hvac":               "HVAC Technician",
	"hvac technician":    "HVAC Technician",
}

// NormalizeProfession returns the canonical English profession name for a
// given input. The lookup is case-insensitive and trims surrounding
// whitespace. Unknown values are returned unchanged.
func NormalizeProfession(p string) string {
	key := strings.ToLower(strings.TrimSpace(p))
	if canonical, ok := ProfessionAliases[key]; ok {
		return canonical
	}
	return p
}
