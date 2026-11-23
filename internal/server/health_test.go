package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type mockPingable struct {
	err error
}

func (m *mockPingable) PingContext(ctx context.Context) error {
	return m.err
}

func TestHealthLive(t *testing.T) {
	healthChecker := NewHealthChecker(nil, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	w := httptest.NewRecorder()

	healthChecker.LiveHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if status.Status != "ok" {
		t.Errorf("expected status 'ok', got '%s'", status.Status)
	}
}

func TestHealthReady_Success(t *testing.T) {
	healthChecker := NewHealthChecker(nil, nil, "")

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	healthChecker.ReadyHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if status.Status != "ok" {
		t.Errorf("expected status 'ok', got '%s'", status.Status)
	}
}

func TestHealthReady_DashboardDBFailure(t *testing.T) {
	mockErr := errors.New("dashboard db ping failed")
	healthChecker := NewHealthChecker(&mockPingable{err: mockErr}, nil, "")

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	healthChecker.ReadyHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", resp.StatusCode)
	}

	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if status.Status != "not_ready" {
		t.Errorf("expected status 'not_ready', got '%s'", status.Status)
	}
}

func TestHealthReady_DevlakeDBFailure(t *testing.T) {
	mockErr := errors.New("devlake db ping failed")
	healthChecker := NewHealthChecker(nil, &mockPingable{err: mockErr}, "")

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	healthChecker.ReadyHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", resp.StatusCode)
	}

	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if status.Status != "not_ready" {
		t.Errorf("expected status 'not_ready', got '%s'", status.Status)
	}
}

func TestHealthReady_WorkflowServiceSuccess(t *testing.T) {
	mockWorkflow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" {
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(HealthStatus{Status: "ok"})
			return
		}
		http.NotFound(w, r)
	}))
	defer mockWorkflow.Close()

	healthChecker := NewHealthChecker(nil, nil, mockWorkflow.URL)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	healthChecker.ReadyHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if status.Status != "ok" {
		t.Errorf("expected status 'ok', got '%s'", status.Status)
	}
}

func TestHealthReady_WorkflowServiceFailure(t *testing.T) {
	mockWorkflow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer mockWorkflow.Close()

	healthChecker := NewHealthChecker(nil, nil, mockWorkflow.URL)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	healthChecker.ReadyHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", resp.StatusCode)
	}

	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if status.Status != "not_ready" {
		t.Errorf("expected status 'not_ready', got '%s'", status.Status)
	}
}

func TestHealthReady_Timeout(t *testing.T) {
	mockWorkflow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" {
			time.Sleep(5 * time.Second)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer mockWorkflow.Close()

	healthChecker := NewHealthChecker(nil, nil, mockWorkflow.URL)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	healthChecker.ReadyHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 on timeout, got %d", resp.StatusCode)
	}

	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if status.Status != "not_ready" {
		t.Errorf("expected status 'not_ready', got '%s'", status.Status)
	}
}

func TestHealthReady_DatabaseTimeout(t *testing.T) {
	// Test with a fast failing pingable to simulate database timeout
	mockErr := context.DeadlineExceeded
	healthChecker := NewHealthChecker(&mockPingable{err: mockErr}, nil, "")

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	healthChecker.ReadyHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 on db timeout, got %d", resp.StatusCode)
	}

	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if status.Status != "not_ready" {
		t.Errorf("expected status 'not_ready', got '%s'", status.Status)
	}
}

func TestHealthReady_WorkflowServiceDown(t *testing.T) {
	// Start a server and immediately close it to get a guaranteed-unused URL.
	ts := httptest.NewServer(http.NotFoundHandler())
	ts.Close()
	healthChecker := NewHealthChecker(nil, nil, ts.URL)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	healthChecker.ReadyHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 when workflow service is down, got %d", resp.StatusCode)
	}

	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if status.Status != "not_ready" {
		t.Errorf("expected status 'not_ready', got '%s'", status.Status)
	}
}

func TestHealthReady_WithAllDependencies(t *testing.T) {
	mockWorkflow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" {
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(HealthStatus{Status: "ok"})
			return
		}
		http.NotFound(w, r)
	}))
	defer mockWorkflow.Close()

	healthChecker := NewHealthChecker(&mockPingable{}, &mockPingable{}, mockWorkflow.URL)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	healthChecker.ReadyHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if status.Status != "ok" {
		t.Errorf("expected status 'ok', got '%s'", status.Status)
	}
}

func TestHealthReady_WithDashboardDBOnly(t *testing.T) {
	healthChecker := NewHealthChecker(&mockPingable{}, nil, "")

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	healthChecker.ReadyHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if status.Status != "ok" {
		t.Errorf("expected status 'ok', got '%s'", status.Status)
	}
}

func TestHealthReady_WithDevlakeDBOnly(t *testing.T) {
	healthChecker := NewHealthChecker(nil, &mockPingable{}, "")

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	healthChecker.ReadyHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if status.Status != "ok" {
		t.Errorf("expected status 'ok', got '%s'", status.Status)
	}
}

func TestHealthReady_WithWorkflowServiceOnly(t *testing.T) {
	mockWorkflow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" {
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(HealthStatus{Status: "ok"})
			return
		}
		http.NotFound(w, r)
	}))
	defer mockWorkflow.Close()

	healthChecker := NewHealthChecker(nil, nil, mockWorkflow.URL)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	healthChecker.ReadyHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if status.Status != "ok" {
		t.Errorf("expected status 'ok', got '%s'", status.Status)
	}
}
