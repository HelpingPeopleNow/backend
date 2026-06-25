package services

import (
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── jsonUnmarshal ───────────────────────────────────────────────────

func TestJsonUnmarshalValid(t *testing.T) {
	var m map[string]interface{}
	err := jsonUnmarshal([]byte(`{"key":"value"}`), &m)
	require.NoError(t, err)
	assert.Equal(t, "value", m["key"])
}

func TestJsonUnmarshalInvalid(t *testing.T) {
	var m map[string]interface{}
	err := jsonUnmarshal([]byte(`not json`), &m)
	assert.Error(t, err)
}

// ── workerCerts ─────────────────────────────────────────────────────

func TestWorkerCertsFromJSON(t *testing.T) {
	w := core.WorkerProfile{Certifications: `["GAS SAFE","NICEIC"]`}
	certs := workerCerts(w)
	assert.Equal(t, []string{"GAS SAFE", "NICEIC"}, certs)
}

func TestWorkerCertsEmpty(t *testing.T) {
	w := core.WorkerProfile{}
	assert.Empty(t, workerCerts(w))
}

func TestWorkerCertsInvalidJSON(t *testing.T) {
	w := core.WorkerProfile{Certifications: "not json"}
	assert.Empty(t, workerCerts(w))
}
