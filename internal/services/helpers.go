package services

import (
	"encoding/json"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

func workerCerts(w core.WorkerProfile) []string {
	var certs []string
	_ = json.Unmarshal([]byte(w.Certifications), &certs)
	return certs
}
