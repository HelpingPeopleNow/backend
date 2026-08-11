// Package openapi embeds the hand-authored OpenAPI 3.0.3 specification
// for the HelpingPeopleNow backend API and exposes it to the HTTP
// layer. Keeping the spec inside the binary guarantees the served
// document always matches the deployed code — there is no separate
// artifact that can drift.
//
// Source of truth for the paths is main.go buildMux + README.md's route
// table; update openapi.yaml in the same commit as any route change.
// openapi_handler_test.go parses the YAML as a schema-lint gate so a
// malformed spec fails CI.
package openapi

import (
	_ "embed"
)

//go:embed openapi.yaml
var SpecYAML []byte

// SpecFileName is the canonical name of the embedded document, used as
// the Content-Disposition filename when serving it.
const SpecFileName = "openapi.yaml"
