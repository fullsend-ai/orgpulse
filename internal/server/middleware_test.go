package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fullsend-ai/orgpulse/internal/logging"
)

// TestRequestIDMiddleware_Generation tests that a request ID is generated when not provided.
func TestRequestIDMiddleware_Generation(t *testing.T) {
	nextCalled := false
	var capturedCtx context.Context

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		capturedCtx = r.Context()
		// Verify request ID is in context
		reqID := logging.RequestIDFromContext(r.Context())
		if reqID == "" {
			t.Error("expected request ID in context")
		}
		// Write response
		w.Header().Set("X-Request-ID", reqID)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	mw := RequestIDMiddleware(next)
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	mw.ServeHTTP(w, req)

	if !nextCalled {
		t.Fatal("expected next handler to be called")
	}

	// Verify response header was set
	respID := w.Header().Get("X-Request-ID")
	if respID == "" {
		t.Fatal("expected X-Request-ID header to be set")
	}

	// Verify context has the same ID
	reqID := logging.RequestIDFromContext(capturedCtx)
	if reqID != respID {
		t.Errorf("expected same ID in context and header, got context=%s header=%s", reqID, respID)
	}

	// Verify it's a valid hex string (32 chars for 16 bytes)
	if len(respID) != 32 {
		t.Errorf("expected 32-char hex string, got length %d", len(respID))
	}
}

// TestRequestIDMiddleware_Preservation tests that existing X-Request-ID is preserved.
func TestRequestIDMiddleware_Preservation(t *testing.T) {
	nextCalled := false
	expectedID := "existing-request-id-12345"

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		reqID := logging.RequestIDFromContext(r.Context())
		if reqID != expectedID {
			t.Errorf("expected preserved ID %s, got %s", expectedID, reqID)
		}
		w.WriteHeader(http.StatusOK)
	})

	mw := RequestIDMiddleware(next)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", expectedID)
	w := httptest.NewRecorder()

	mw.ServeHTTP(w, req)

	if !nextCalled {
		t.Fatal("expected next handler to be called")
	}

	// Verify response header preserves the original ID
	respID := w.Header().Get("X-Request-ID")
	if respID != expectedID {
		t.Errorf("expected response header to preserve ID %s, got %s", expectedID, respID)
	}
}

// TestRequestLogMiddleware_SuppressHealthLogs tests that 200 OK on health endpoints is suppressed.
func TestRequestLogMiddleware_SuppressHealthLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Wrap with logging using a custom logger
	mw := withLogging(next)

	// Create context with logger
	ctx := logging.WithLogger(context.Background(), logger)

	// Test /health/live
	req := httptest.NewRequest("GET", "/health/live", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	output := buf.String()
	if strings.Contains(output, "request completed") {
		t.Error("expected health endpoint log to be suppressed, but got log entry")
	}
	buf.Reset()

	// Test /health/ready
	req = httptest.NewRequest("GET", "/health/ready", nil)
	req = req.WithContext(ctx)
	w = httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	output = buf.String()
	if strings.Contains(output, "request completed") {
		t.Error("expected health endpoint log to be suppressed, but got log entry")
	}
	buf.Reset()

	// Test /metrics
	req = httptest.NewRequest("GET", "/metrics", nil)
	req = req.WithContext(ctx)
	w = httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	output = buf.String()
	if strings.Contains(output, "request completed") {
		t.Error("expected metrics endpoint log to be suppressed, but got log entry")
	}
}

// TestRequestLogMiddleware_NonSuppression tests that 503 on /health/ready is NOT suppressed.
func TestRequestLogMiddleware_NonSuppression(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("Service Unavailable"))
	})

	mw := withLogging(next)

	ctx := logging.WithLogger(context.Background(), logger)

	req := httptest.NewRequest("GET", "/health/ready", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	output := buf.String()
	if !strings.Contains(output, "request completed") {
		t.Error("expected non-200 log entry for /health/ready")
	}

	// Verify log contains expected fields
	var logEntry map[string]any
	if err := json.Unmarshal([]byte(output), &logEntry); err != nil {
		t.Fatalf("expected JSON log, got: %s", output)
	}

	if logEntry["status"].(float64) != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %v", logEntry["status"])
	}
}

// TestRequestLogMiddleware_LogFields tests that all expected log fields are present.
func TestRequestLogMiddleware_LogFields(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("test response"))
	})

	mw := withLogging(next)

	// Generate a known request ID
	ctx := logging.WithLogger(context.Background(), logger)
	ctx = logging.WithRequestID(ctx, "test-request-id-12345")

	req := httptest.NewRequest("POST", "/api/test", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	output := buf.String()
	if !strings.Contains(output, "request completed") {
		t.Fatalf("expected log entry, got: %s", output)
	}

	var logEntry map[string]any
	if err := json.Unmarshal([]byte(output), &logEntry); err != nil {
		t.Fatalf("expected JSON log, got: %s", output)
	}

	// Verify all expected fields
	expectedFields := []string{"method", "path", "status", "duration_ms", "bytes_written", "request_id"}
	for _, field := range expectedFields {
		if _, ok := logEntry[field]; !ok {
			t.Errorf("expected field %s in log entry, got: %v", field, logEntry)
		}
	}

	if logEntry["method"] != "POST" {
		t.Errorf("expected method POST, got %v", logEntry["method"])
	}
	if logEntry["path"] != "/api/test" {
		t.Errorf("expected path /api/test, got %v", logEntry["path"])
	}
	if logEntry["status"].(float64) != http.StatusOK {
		t.Errorf("expected status 200, got %v", logEntry["status"])
	}
	if logEntry["bytes_written"].(float64) != float64(len("test response")) {
		t.Errorf("expected bytes_written %d, got %v", len("test response"), logEntry["bytes_written"])
	}
	if logEntry["request_id"] != "test-request-id-12345" {
		t.Errorf("expected request_id test-request-id-12345, got %v", logEntry["request_id"])
	}
}

// TestResponseWriter tests the responseWriter functionality.
func TestResponseWriter(t *testing.T) {
	// Create a response recorder as the underlying writer
	rr := httptest.NewRecorder()

	// Wrap it
	rw := newResponseWriter(rr)

	// Test WriteHeader
	rw.WriteHeader(http.StatusNotFound)
	if rw.statusCode != http.StatusNotFound {
		t.Errorf("expected statusCode 404, got %d", rw.statusCode)
	}

	// Test Write
	n, err := rw.Write([]byte("not found"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if rw.bytesWritten != n {
		t.Errorf("expected bytesWritten %d, got %d", n, rw.bytesWritten)
	}

	// Test Unwrap
	unwrapped := rw.Unwrap()
	if unwrapped != rr {
		t.Error("Unwrap did not return underlying ResponseWriter")
	}

	// Verify the underlying recorder got the writes
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected recorder status 404, got %d", rr.Code)
	}
	if rr.Body.String() != "not found" {
		t.Errorf("expected body 'not found', got %q", rr.Body.String())
	}
}

// TestGenerateRequestID tests the request ID generation.
func TestGenerateRequestID(t *testing.T) {
	id := generateRequestID()

	// Should be 32 characters (16 bytes hex-encoded)
	if len(id) != 32 {
		t.Errorf("expected 32-char hex string, got length %d", len(id))
	}

	// Should be valid hex
	_, err := io.ReadFull(strings.NewReader(id), make([]byte, 16))
	if err != nil {
		t.Errorf("generated ID is not valid hex: %v", err)
	}

	// Generate multiple IDs and verify uniqueness
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := generateRequestID()
		if ids[id] {
			t.Error("duplicate request ID generated")
		}
		ids[id] = true
	}
}

// TestMiddlewareChaining tests that both middleware can be chained.
func TestMiddlewareChaining(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := logging.RequestIDFromContext(r.Context())
		if reqID == "" {
			t.Error("expected request ID in context")
		}
		w.Header().Set("X-Request-ID", reqID)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Chain both middleware - RequestIDMiddleware must be outer to propagate context
	mw := RequestIDMiddleware(RequestLogMiddleware(next))

	ctx := logging.WithLogger(context.Background(), logger)

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	mw.ServeHTTP(w, req)

	// Verify request ID header was set
	respID := w.Header().Get("X-Request-ID")
	if respID == "" {
		t.Fatal("expected X-Request-ID header")
	}

	// Verify log was written (this is not a health endpoint)
	output := buf.String()
	if !strings.Contains(output, "request completed") {
		t.Fatalf("expected log entry, got: %s", output)
	}

	// Verify log contains the request ID
	var logEntry map[string]any
	if err := json.Unmarshal([]byte(output), &logEntry); err != nil {
		t.Fatalf("expected JSON log, got: %s", output)
	}
	if logEntry["request_id"] != respID {
		t.Errorf("expected same request_id in log and header")
	}
}

// TestResponseWriterUnwrap tests that Unwrap works correctly with httptest.ResponseRecorder.
func TestResponseWriterUnwrapResponseRecorder(t *testing.T) {
	rr := httptest.NewRecorder()
	rw := newResponseWriter(rr)

	// Write through the wrapper
	rw.WriteHeader(http.StatusAccepted)
	_, _ = rw.Write([]byte("accepted"))

	// Unwrap and verify
	unwrapped := rw.Unwrap().(*httptest.ResponseRecorder)

	if unwrapped.Code != http.StatusAccepted {
		t.Errorf("expected unwrapped Code 202, got %d", unwrapped.Code)
	}
	if unwrapped.Body.String() != "accepted" {
		t.Errorf("expected unwrapped Body 'accepted', got %q", unwrapped.Body.String())
	}
}
