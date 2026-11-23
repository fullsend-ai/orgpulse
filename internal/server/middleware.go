package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
	"time"

	"github.com/fullsend-ai/orgpulse/internal/logging"
)

// responseWriter wraps http.ResponseWriter to capture status code and bytes written.
type responseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

// newResponseWriter creates a new responseWriter.
func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

// WriteHeader captures the status code and writes it.
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Write captures the bytes written.
func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += n
	return n, err
}

// Unwrap returns the underlying http.ResponseWriter.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// generateRequestID generates a request ID using crypto/rand.
// Returns a 16-byte hex string (32 characters).
func generateRequestID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		// Fallback to hex-encoded timestamp if crypto/rand fails
		// Use only the first byte of the nanos to avoid overflow issues
		return hex.EncodeToString([]byte{byte(time.Now().UnixNano() & 0xFF)})
	}
	return hex.EncodeToString(b)
}

// validRequestID limits client-supplied IDs to what tracing systems emit
// (UUIDs, hex, dotted or colon-separated trace IDs).
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// withRequestID extracts or generates X-Request-ID, sets response header, and injects into context.
// A client-supplied ID that is too long or has unexpected characters is replaced.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if !validRequestID.MatchString(reqID) {
			reqID = generateRequestID()
		}

		// Set response header
		w.Header().Set("X-Request-ID", reqID)

		// Inject into context
		ctx := logging.WithRequestID(r.Context(), reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDMiddleware is an alias for withRequestID.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return withRequestID(next)
}

// withLogging wraps the handler to log request completion with timing and metrics.
// Suppresses 200 OK logs for /health/live, /health/ready, /metrics.
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Determine if this is a health/metrics endpoint that should be suppressed
		path := r.URL.Path
		isHealthOrMetrics := path == "/health/live" || path == "/health/ready" || path == "/metrics"

		// Wrap response writer to capture metrics
		wrapped := newResponseWriter(w)

		start := time.Now()
		next.ServeHTTP(wrapped, r)
		duration := time.Since(start)

		// Suppress 200 OK for health/metrics endpoints
		if isHealthOrMetrics && wrapped.statusCode == http.StatusOK {
			return
		}

		// Get logger and request ID from context (may have been modified by upstream middleware)
		logger := logging.FromContext(r.Context())
		requestID := logging.RequestIDFromContext(r.Context())

		// Log request completion
		logger.Info("request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration_ms", duration.Milliseconds(),
			"bytes_written", wrapped.bytesWritten,
			"request_id", requestID)
	})
}

// RequestLogMiddleware is an alias for withLogging.
func RequestLogMiddleware(next http.Handler) http.Handler {
	return withLogging(next)
}
