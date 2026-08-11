package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HelpingPeopleNow/backend/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestOpenAPISpecServesValidYAML is the schema-lint gate: it serves the
// embedded spec through the handler and parses the body as YAML with
// the structural checks that matter (openapi version, info, non-empty
// path map). A malformed spec (bad indentation, truncated $ref, empty
// paths) fails here — and therefore fails CI — before it can be served.
// Full semantic linting (duplicate operationIds, $ref resolution) is the
// redocly/spectral step in CI (see infra/docs/SPEC.md §9).
func TestOpenAPISpecServesValidYAML(t *testing.T) {
	rec := httptest.NewRecorder()
	OpenAPISpecHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil))

	require.Equal(t, http.StatusOK, rec.Code, "spec endpoint must return 200")
	assert.Equal(t, "application/yaml", rec.Header().Get("Content-Type"))

	var doc struct {
		OpenAPI string `yaml:"openapi"`
		Info    struct {
			Title   string `yaml:"title"`
			Version string `yaml:"version"`
		} `yaml:"info"`
		Paths map[string]any `yaml:"paths"`
	}
	err := yaml.Unmarshal(rec.Body.Bytes(), &doc)
	require.NoError(t, err, "embedded spec must parse as YAML")

	assert.True(t, strings.HasPrefix(doc.OpenAPI, "3.0."), "must be OpenAPI 3.0.x, got %q", doc.OpenAPI)
	assert.NotEmpty(t, doc.Info.Title)
	assert.NotEmpty(t, doc.Info.Version)
	require.NotEmpty(t, doc.Paths, "paths must not be empty")

	// The contract must cover the core buildMux routes — if someone
	// adds a route without documenting it, this test fails loudly.
	for _, p := range []string{"/api/v1/chat", "/api/v1/worker/profile", "/api/v1/workers/", "/health", "/readyz", "/api/v1/openapi.yaml", "/api/v1/docs"} {
		_, ok := doc.Paths[p]
		assert.True(t, ok, "paths must document %s", p)
	}
}

// TestOpenAPISpecMatchesEmbedded ensures the handler serves the exact
// bytes embedded via go:embed — no transcription drift between the
// openapi package and the handler.
func TestOpenAPISpecMatchesEmbedded(t *testing.T) {
	rec := httptest.NewRecorder()
	OpenAPISpecHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil))

	assert.Equal(t, string(openapi.SpecYAML), rec.Body.String())
}

// TestOpenAPISpecRejectsNonGet pins the method restriction on the spec
// endpoint.
func TestOpenAPISpecRejectsNonGet(t *testing.T) {
	rec := httptest.NewRecorder()
	OpenAPISpecHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/openapi.yaml", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// TestOpenAPIDocsServesRedoc pins the docs UI: it must return the
// Redoc HTML page that loads the spec URL.
func TestOpenAPIDocsServesRedoc(t *testing.T) {
	rec := httptest.NewRecorder()
	OpenAPIDocsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/docs", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	body := rec.Body.String()
	assert.Contains(t, body, "redoc", "docs page must be the Redoc UI")
	assert.Contains(t, body, "/api/v1/openapi.yaml", "Redoc page must reference the spec URL")
}

// TestOpenAPIDocsRejectsNonGet pins the method restriction on the docs
// endpoint.
func TestOpenAPIDocsRejectsNonGet(t *testing.T) {
	rec := httptest.NewRecorder()
	OpenAPIDocsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/docs", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
