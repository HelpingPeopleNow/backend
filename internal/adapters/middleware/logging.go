package middleware

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/adapters/handler"
)

func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		slog.Info("request started",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		)
		next.ServeHTTP(rec, r)

		duration := time.Since(start)
		statusStr := strconv.Itoa(rec.status)
		slog.Info("request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", duration.Milliseconds(),
		)
		handler.IncrHTTPRequests(r.Method, r.URL.Path, statusStr)
		handler.ObserveHTTPDuration(r.Method, r.URL.Path, duration.Seconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.status = code
	rec.ResponseWriter.WriteHeader(code)
}
