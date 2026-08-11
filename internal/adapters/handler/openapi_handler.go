package handler

import (
	"net/http"

	"github.com/HelpingPeopleNow/backend/openapi"
)

// openAPISpecHandler serves the embedded OpenAPI 3.0.3 document as raw
// YAML (spec-first contract, see infra/docs/SPEC.md §9). Mounted at
// /api/v1/openapi.yaml behind session auth so the contract is not
// publicly discoverable.
func OpenAPISpecHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		w.Header().Set("Content-Disposition", `inline; filename="`+openapi.SpecFileName+`"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(openapi.SpecYAML)
	})
}

// redocHTML is the Redoc standalone documentation page. It is a single
// static HTML file that loads the spec from /api/v1/openapi.yaml and
// renders the interactive docs in the browser. No asset pipeline or
// extra container needed — it ships inside the binary via the string
// below (the backend does not set a CSP header, so the CDN script tag
// loads normally).
const redocHTML = `<!DOCTYPE html>
<html>
  <head>
    <title>HelpingPeopleNow API Docs</title>
    <meta charset="utf-8"/>
    <meta name="viewport" content="width=device-width, initial-scale=1"/>
    <link href="https://fonts.googleapis.com/css?family=Montserrat:300,400,700|Roboto:300,400,700" rel="stylesheet"/>
    <style>body { margin: 0; padding: 0; }</style>
  </head>
  <body>
    <redoc spec-url="/api/v1/openapi.yaml"></redoc>
    <script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"></script>
  </body>
</html>
`

// openAPIDocsHandler serves the interactive Redoc UI at /api/v1/docs.
// In production buildMux wraps it in Admin so the API contract stays
// internal (public consumers get the spec via the frontend instead).
func OpenAPIDocsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(redocHTML))
	})
}
