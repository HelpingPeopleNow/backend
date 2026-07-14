package core

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed professions.json
var professionsJSON []byte

// ProfessionAliases maps lowercase, trimmed aliases to canonical English
// profession names. It is the single source of truth for profession
// normalization across the backend (search query normalization and
// embedding text normalization).
//
// The map is loaded at package init from professions.json (same file the
// helper backfill script reads), so runtime queries and batch backfill
// canonicalize to the same strings. The two copies are kept in lock-step by
// the byte-parity CI gate (helper/scripts/test_byte_parity_gate.sh).
var ProfessionAliases = loadProfessionAliases()

func loadProfessionAliases() map[string]string {
	m := make(map[string]string)
	if err := json.Unmarshal(professionsJSON, &m); err != nil {
		// A malformed embedded asset is a build-time defect, not a
		// runtime condition — fail fast so it is caught in CI.
		panic(fmt.Sprintf("core: failed to load professions.json: %v", err))
	}
	return m
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
