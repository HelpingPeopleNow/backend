package services

import (
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

func TestJsonUnmarshalValid(t *testing.T) {
	var m map[string]interface{}
	err := jsonUnmarshal([]byte(`{"key":"value"}`), &m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["key"] != "value" {
		t.Fatalf("expected value, got %v", m["key"])
	}
}

func TestJsonUnmarshalInvalid(t *testing.T) {
	var m map[string]interface{}
	err := jsonUnmarshal([]byte(`not json`), &m)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestWorkerCertsFromJSON(t *testing.T) {
	w := core.WorkerProfile{Certifications: `["GAS SAFE","NICEIC"]`}
	certs := workerCerts(w)
	if len(certs) != 2 {
		t.Fatalf("expected 2 certs, got %d", len(certs))
	}
	if certs[0] != "GAS SAFE" || certs[1] != "NICEIC" {
		t.Fatalf("unexpected certs: %v", certs)
	}
}

func TestWorkerCertsEmpty(t *testing.T) {
	w := core.WorkerProfile{Certifications: ""}
	certs := workerCerts(w)
	if certs != nil {
		t.Fatalf("expected nil for empty string, got %v", certs)
	}
}

func TestWorkerCertsInvalidJSON(t *testing.T) {
	w := core.WorkerProfile{Certifications: "not json"}
	certs := workerCerts(w)
	if certs != nil {
		t.Fatalf("expected nil for invalid JSON, got %v", certs)
	}
}
