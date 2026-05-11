package middleware

import (
	"log"
	"net/http"
	"time"
)

func WithRequestLog(logger *log.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rw, r)

		logger.Printf("http %s %s status=%d duracao_ms=%d", r.Method, r.URL.Path, rw.status, time.Since(start).Milliseconds())
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

