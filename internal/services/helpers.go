package services

import (
	"encoding/json"
	"log/slog"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

func workerCerts(w core.WorkerProfile) []string {
	var certs []string
	if err := json.Unmarshal([]byte(w.Certifications), &certs); err != nil {
		slog.Warn("workerCerts: unmarshal failed", "error", err)
	}
	return certs
}
