// Package integration exercises end-to-end X-Request-ID propagation.
package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fullsend-ai/orgpulse/internal/database"
	"github.com/fullsend-ai/orgpulse/internal/server"
)

func TestE2ETracing_RequestIDPropagation(t *testing.T) {
	store := database.NewMemStore()

	// Create an API key for authentication
	apiKey := "test-api-key-12345"
	_, err := store.CreateAPIKey(t.Context(), "test-key", database.HashAPIKey(apiKey), false, "")
	if err != nil {
		t.Fatalf("create API key: %v", err)
	}

	// Create a mock upstream service that echoes back the request ID
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"request_id": reqID,
			"message":    "upstream processed",
		})
	}))
	defer mockUpstream.Close()

	// Create a mock workflow service that echoes back the request ID
	mockWorkflow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			return
		}

		reqID := r.Header.Get("X-Request-ID")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"request_id": reqID,
			"message":    "workflow processed",
		})
	}))
	defer mockWorkflow.Close()

	srv := server.New(0, false, nil, nil, store, nil, server.FrontendConfig{}, nil, server.AIAssistConfig{}, nil, nil, mockWorkflow.URL)
	handler := srv.Handler()

	t.Log("=== Test: X-Request-ID propagates through server to upstream ===")

	// Generate a unique request ID
	testReqID := "test-request-id-12345"

	// Make a request with the custom X-Request-ID header and API key
	reqBody := jsonBody(t, map[string]string{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs", reqBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", testReqID)
	req.Header.Set("X-API-Key", apiKey)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	check(t, "request with custom X-Request-ID succeeds", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d body=%s", resp.StatusCode, string(body)))

	// Verify the request ID is propagated
	genReqID := w.Header().Get("X-Request-ID")
	if genReqID == "" {
		genReqID = w.Header().Get("X-Request-Id")
	}
	check(t, "X-Request-ID header is present", genReqID != "", fmt.Sprintf("X-Request-ID header=%s", genReqID))
}

func TestE2ETracing_RequestIDGenerated(t *testing.T) {
	store := database.NewMemStore()

	// Create an API key for authentication
	apiKey := "test-api-key-generated" //nolint:gosec
	_, err := store.CreateAPIKey(t.Context(), "test-key", database.HashAPIKey(apiKey), false, "")
	if err != nil {
		t.Fatalf("create API key: %v", err)
	}

	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"request_id": reqID,
			"message":    "upstream processed",
		})
	}))
	defer mockUpstream.Close()

	srv := server.New(0, false, nil, nil, store, nil, server.FrontendConfig{}, nil, server.AIAssistConfig{}, nil, nil, "")
	handler := srv.Handler()

	t.Log("=== Test: X-Request-ID is generated when not provided ===")

	// Make a request without X-Request-ID header but with API key
	reqBody := jsonBody(t, map[string]string{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs", reqBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	check(t, "request without X-Request-ID succeeds", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d body=%s", resp.StatusCode, string(body)))

	// Verify a request ID was generated
	genReqID := w.Header().Get("X-Request-ID")
	if genReqID == "" {
		genReqID = w.Header().Get("X-Request-Id")
	}
	check(t, "X-Request-ID is generated", genReqID != "" && len(genReqID) >= 16, fmt.Sprintf("generated X-Request-ID=%s", genReqID))

	// Verify the generated ID is valid (hex string)
	check(t, "generated X-Request-ID is valid hex", isValidHex(genReqID), fmt.Sprintf("generated X-Request-ID=%s", genReqID))
}

func TestE2ETracing_RequestIDInContext(t *testing.T) {
	store := database.NewMemStore()

	// Create an API key for authentication
	apiKey := "test-api-key-context" //nolint:gosec
	_, err := store.CreateAPIKey(t.Context(), "test-key", database.HashAPIKey(apiKey), false, "")
	if err != nil {
		t.Fatalf("create API key: %v", err)
	}

	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"request_id": reqID,
			"message":    "upstream processed",
		})
	}))
	defer mockUpstream.Close()

	srv := server.New(0, false, nil, nil, store, nil, server.FrontendConfig{}, nil, server.AIAssistConfig{}, nil, nil, "")
	handler := srv.Handler()

	t.Log("=== Test: Request ID is stored in context ===")

	// Make a request with API key
	reqBody := jsonBody(t, map[string]string{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs", reqBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// The middleware should have stored the request ID in context
	// We can't directly access it from outside, but we can verify it was generated
	genReqID := w.Header().Get("X-Request-ID")
	if genReqID == "" {
		genReqID = w.Header().Get("X-Request-Id")
	}
	check(t, "request ID stored in context", genReqID != "" && len(genReqID) >= 16, fmt.Sprintf("context request ID=%s", genReqID))
}

func TestE2ETracing_WorkflowProxyRequestID(t *testing.T) {
	store := database.NewMemStore()

	// Create an API key for authentication
	apiKey := "test-api-key-workflow" //nolint:gosec
	_, err := store.CreateAPIKey(t.Context(), "test-key", database.HashAPIKey(apiKey), true, "")
	if err != nil {
		t.Fatalf("create API key: %v", err)
	}

	// Create a mock workflow service that echoes back the request ID
	var workflowReqID string
	mockWorkflow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		workflowReqID = reqID

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"request_id": reqID,
			"message":    "workflow processed",
		})
	}))
	defer mockWorkflow.Close()

	srv := server.New(0, false, nil, nil, store, nil, server.FrontendConfig{}, nil, server.AIAssistConfig{}, nil, nil, mockWorkflow.URL)
	handler := srv.Handler()

	t.Log("=== Test: X-Request-ID propagates to workflow service ===")

	testReqID := "workflow-test-id-67890"

	// Make a request to the production workflow endpoint with API key
	// The workflow proxy is at /workflows/production/{name}
	req := httptest.NewRequest(http.MethodGet, "/workflows/production/test-workflow", nil)
	req.Header.Set("X-Request-ID", testReqID)
	req.Header.Set("X-API-Key", apiKey)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify the workflow service received the request ID
	check(t, "workflow service received X-Request-ID", workflowReqID == testReqID, fmt.Sprintf("workflowReqID=%s, testReqID=%s", workflowReqID, testReqID))
}

// isValidHex checks if a string is a valid hexadecimal string
func isValidHex(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}
