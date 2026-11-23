package server

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fullsend-ai/orgpulse/internal/logging"
)

func allowWorkflow(*http.Request, []byte) error { return nil }

// TestWorkflowProductionProxyHandler_RequestIDForwarding tests that the request ID is forwarded to upstream.
func TestWorkflowProductionProxyHandler_RequestIDForwarding(t *testing.T) {
	// Create a test upstream server that captures the request headers
	var capturedHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("upstream response"))
	}))
	defer upstream.Close()

	// Create context with request ID
	ctx := context.Background()
	reqID := "test-request-id-12345"
	ctx = logging.WithRequestID(ctx, reqID)

	// Use mux to properly set up path values
	mux := http.NewServeMux()
	mux.HandleFunc("GET /workflows/production/{name}", workflowProductionProxyHandler(upstream.URL, allowWorkflow))

	// Create a request that will match the pattern
	req := httptest.NewRequest("GET", "/workflows/production/test-workflow", nil)
	req = req.WithContext(ctx)
	req.Header.Set("X-Request-ID", reqID)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Verify response
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify request ID was forwarded to upstream server
	if capturedHeaders == nil {
		t.Fatal("expected headers to be captured")
	}
	forwardedID := capturedHeaders.Get("X-Request-ID")
	if forwardedID != reqID {
		t.Errorf("expected X-Request-ID %q, got %q", reqID, forwardedID)
	}
}

// TestWorkflowProductionProxyHandler_AuthorizationForwarding tests that Authorization header is forwarded.
func TestWorkflowProductionProxyHandler_AuthorizationForwarding(t *testing.T) {
	// Create a test upstream server that captures the request headers
	var capturedHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("upstream response"))
	}))
	defer upstream.Close()

	// Create context with request ID
	ctx := context.Background()
	ctx = logging.WithRequestID(ctx, "test-request-id")

	// Create request with Authorization header
	authHeader := "Bearer my-secret-token"

	// Use mux to properly set up path values
	mux := http.NewServeMux()
	mux.HandleFunc("GET /workflows/production/{name}", workflowProductionProxyHandler(upstream.URL, allowWorkflow))

	req := httptest.NewRequest("GET", "/workflows/production/test-workflow", nil)
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", authHeader)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Verify Authorization header was forwarded
	if capturedHeaders == nil {
		t.Fatal("expected headers to be captured")
	}
	forwardedAuth := capturedHeaders.Get("Authorization")
	if forwardedAuth != authHeader {
		t.Errorf("expected Authorization %q, got %q", authHeader, forwardedAuth)
	}
}

// TestWorkflowProductionProxyHandler_NoAuthorizationForwarding tests that missing Authorization is handled.
func TestWorkflowProductionProxyHandler_NoAuthorizationForwarding(t *testing.T) {
	// Create a test upstream server that captures the request headers
	var capturedHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("upstream response"))
	}))
	defer upstream.Close()

	// Create context with request ID
	ctx := context.Background()
	ctx = logging.WithRequestID(ctx, "test-request-id")

	// Use mux to properly set up path values
	mux := http.NewServeMux()
	mux.HandleFunc("GET /workflows/production/{name}", workflowProductionProxyHandler(upstream.URL, allowWorkflow))

	req := httptest.NewRequest("GET", "/workflows/production/test-workflow", nil)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Verify no Authorization header was forwarded
	if capturedHeaders == nil {
		t.Fatal("expected headers to be captured")
	}
	forwardedAuth := capturedHeaders.Get("Authorization")
	if forwardedAuth != "" {
		t.Errorf("expected no Authorization header, got %q", forwardedAuth)
	}
}

// TestWorkflowProductionProxyHandler_Logging tests that logging includes request_id.
func TestWorkflowProductionProxyHandler_Logging(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	// Create a test upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("upstream response"))
	}))
	defer upstream.Close()

	// Create context with logger and request ID
	ctx := logging.WithLogger(context.Background(), logger)
	reqID := "test-request-id-logging"
	ctx = logging.WithRequestID(ctx, reqID)

	// Use mux to properly set up path values
	mux := http.NewServeMux()
	mux.HandleFunc("GET /workflows/production/{name}", workflowProductionProxyHandler(upstream.URL, allowWorkflow))

	req := httptest.NewRequest("GET", "/workflows/production/test-workflow", nil)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Verify response
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Check log output for request_id
	logOutput := buf.String()
	if !strings.Contains(logOutput, "request_id") {
		t.Error("expected log to contain 'request_id' field")
	}
	if !strings.Contains(logOutput, reqID) {
		t.Errorf("expected log to contain request_id %q, got: %s", reqID, logOutput)
	}
}

// TestWorkflowProductionProxyHandler_InvalidName tests that invalid workflow names are rejected.
func TestWorkflowProductionProxyHandler_InvalidName(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("upstream response"))
	}))
	defer upstream.Close()

	ctx := context.Background()
	ctx = logging.WithRequestID(ctx, "test-request-id")

	// Use mux to properly set up path values
	mux := http.NewServeMux()
	mux.HandleFunc("GET /workflows/production/{name}", workflowProductionProxyHandler(upstream.URL, allowWorkflow))

	// Test with invalid name (contains special char that's not allowed)
	req := httptest.NewRequest("GET", "/workflows/production/invalid_name", nil)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// This should actually pass since underscore is valid in the regex
	// Let's test with a truly invalid name
	req2 := httptest.NewRequest("GET", "/workflows/production/invalid!name", nil)
	req2 = req2.WithContext(ctx)

	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)

	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid name, got %d", w2.Code)
	}
}

// TestWorkflowProductionProxyHandler_OptionsRequest tests that OPTIONS requests are handled.
func TestWorkflowProductionProxyHandler_OptionsRequest(t *testing.T) {
	// This tests the preflight request handling (server.go handles CORS)
	// The proxy itself should still process the request normally
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	ctx := context.Background()
	ctx = logging.WithRequestID(ctx, "test-request-id")

	// Use mux to properly set up path values
	mux := http.NewServeMux()
	mux.HandleFunc("OPTIONS /workflows/production/{name}", workflowProductionProxyHandler(upstream.URL, allowWorkflow))

	req := httptest.NewRequest("OPTIONS", "/workflows/production/test-workflow", nil)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// OPTIONS should be forwarded to upstream
	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204 for OPTIONS, got %d", w.Code)
	}
}
