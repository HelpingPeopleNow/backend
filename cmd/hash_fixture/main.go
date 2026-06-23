// Command hash_fixture prints the canonical field-name → text → SHA-256
// mapping for a small set of fixtures shared with
// helper/scripts/backfill_embeddings.py --self-test.
//
// Run:
//
//	cd backend && go run ./cmd/hash_fixture
//
// Compare its output against the Python self-test. If both list the same
// field_names and identical hashes, byte parity is intact.
package main

import (
	"fmt"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

func mustFields(row, city string) map[string]string {
	wp := &core.WorkerProfile{
		Profession:   row,
		City:         city,
		BusinessName: "",
	}
	return core.BuildFieldTexts(wp)
}

func fixture1() map[string]string {
	// Wire-shape matches worker.go::WorkerProfile JSON columns: raw JSON
	// strings for Certifications/Languages. BuildFieldTexts passes them
	// to joinJSONArray, which json.Unmarshals and ", ".joins.
	return core.BuildFieldTexts(&core.WorkerProfile{
		Profession:     "electricista",
		Bio:            "Electricista con 15 años de experiencia",
		Certifications: `["Licencia Tipo B", "Certificado de Segurança"]`,
		City:           "Madrid",
		Languages:      `["es", "en"]`,
	})
}

func fixture2() map[string]string {
	return core.BuildFieldTexts(&core.WorkerProfile{
		Profession:   "Plumber",
		City:         "Barcelona",
		BusinessName: "Fontanería Ríos",
	})
}

func main() {
	emit := func(idx int, fields map[string]string) {
		fmt.Printf("=== fixture %d ===\n", idx)
		for _, name := range []string{
			"profession", "profession_raw", "bio", "certifications",
			"city", "languages", "business_name",
		} {
			if text, ok := fields[name]; ok {
				fmt.Printf("  %s|%s|%s\n", name, text, core.HashField(text))
			}
		}
	}
	emit(1, fixture1())
	emit(2, fixture2())
	// Reference the helper — keeps the import used if WorkerProfile is
	// the only extern symbol referenced. Cheap typing-pulse safety net.
	_ = mustFields
}
