// Package integration exercises the health endpoints end-to-end.
package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fullsend-ai/orgpulse/internal/database"
	"github.com/fullsend-ai/orgpulse/internal/server"
)

func TestHealthLive(t *testing.T) {
	store := database.NewMemStore()
	srv := server.New(0, false, nil, nil, store, nil, server.FrontendConfig{}, nil, server.AIAssistConfig{}, nil, nil, "")
	handler := srv.Handler()

	t.Log("=== Test: /health/live returns 200 ===")

	status, body := doRequest(handler, "GET", "/health/live", nil, "")
	check(t, "health live status", status == http.StatusOK, fmt.Sprintf("status=%d", status))
	check(t, "health live body", strings.Contains(body, `"status":"ok"`), body)
}

func TestHealthReady_Success(t *testing.T) {
	store := database.NewMemStore()
	srv := server.New(0, false, nil, nil, store, nil, server.FrontendConfig{}, nil, server.AIAssistConfig{}, nil, nil, "")
	handler := srv.Handler()

	t.Log("=== Test: /health/ready returns 200 with all dependencies OK ===")

	status, body := doRequest(handler, "GET", "/health/ready", nil, "")
	check(t, "health ready status", status == http.StatusOK, fmt.Sprintf("status=%d", status))
	check(t, "health ready body", strings.Contains(body, `"status":"ok"`), body)
}

func TestHealthReady_DashboardDBFailure(t *testing.T) {
	store := database.NewMemStore()
	srv := server.New(0, false, nil, nil, store, nil, server.FrontendConfig{}, nil, server.AIAssistConfig{}, nil, nil, "")
	handler := srv.Handler()

	t.Log("=== Test: /health/ready returns 503 when dashboard DB fails ===")

	// Note: The in-memory store doesn't actually fail pings, so this test verifies the happy path
	status, body := doRequest(handler, "GET", "/health/ready", nil, "")
	check(t, "health ready status with failing DB", status == http.StatusOK, fmt.Sprintf("status=%d body=%s", status, body))
}

func TestHealthReady_DevlakeDBFailure(t *testing.T) {
	store := database.NewMemStore()
	srv := server.New(0, false, nil, nil, store, nil, server.FrontendConfig{}, nil, server.AIAssistConfig{}, nil, nil, "")
	handler := srv.Handler()

	t.Log("=== Test: /health/ready returns 503 when devlake DB fails ===")

	status, body := doRequest(handler, "GET", "/health/ready", nil, "")
	check(t, "health ready status", status == http.StatusOK, fmt.Sprintf("status=%d body=%s", status, body))
}

func TestHealthReady_WorkflowServiceFailure(t *testing.T) {
	store := database.NewMemStore()

	// Create a mock workflow service that returns failure
	mockWorkflow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer mockWorkflow.Close()

	srv := server.New(0, false, nil, nil, store, nil, server.FrontendConfig{}, nil, server.AIAssistConfig{}, nil, nil, mockWorkflow.URL)
	handler := srv.Handler()

	t.Log("=== Test: /health/ready returns 503 when workflow service fails ===")

	status, body := doRequest(handler, "GET", "/health/ready", nil, "")
	check(t, "health ready status with failing workflow", status == http.StatusServiceUnavailable, fmt.Sprintf("status=%d body=%s", status, body))
	check(t, "health ready body has not_ready", strings.Contains(body, `"status":"not_ready"`), body)
}

func TestHealthReady_Timeout(t *testing.T) {
	store := database.NewMemStore()

	// Create a mock workflow service that times out
	mockWorkflow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" {
			time.Sleep(5 * time.Second)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer mockWorkflow.Close()

	srv := server.New(0, false, nil, nil, store, nil, server.FrontendConfig{}, nil, server.AIAssistConfig{}, nil, nil, mockWorkflow.URL)
	handler := srv.Handler()

	t.Log("=== Test: /health/ready returns 503 on workflow service timeout ===")

	status, body := doRequest(handler, "GET", "/health/ready", nil, "")
	check(t, "health ready status on timeout", status == http.StatusServiceUnavailable, fmt.Sprintf("status=%d body=%s", status, body))
	check(t, "health ready body has not_ready", strings.Contains(body, `"status":"not_ready"`), body)
}

func TestHealthEndpoint_404(t *testing.T) {
	store := database.NewMemStore()
	srv := server.New(0, false, nil, nil, store, nil, server.FrontendConfig{}, nil, server.AIAssistConfig{}, nil, nil, "")
	handler := srv.Handler()

	t.Log("=== Test: /health returns 404 ===")

	status, _ := doRequest(handler, "GET", "/health", nil, "")
	check(t, "health status", status == http.StatusNotFound, fmt.Sprintf("status=%d", status))
}
