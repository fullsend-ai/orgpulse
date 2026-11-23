package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type HealthStatus struct {
	Status string `json:"status"`
}

type Pingable interface {
	PingContext(ctx context.Context) error
}

type HealthChecker struct {
	dashboardDB     Pingable
	devlakeDB       Pingable
	workflowExecURL string
	httpClient      *http.Client
}

func NewHealthChecker(dashboardDB, devlakeDB Pingable, workflowExecURL string) *HealthChecker {
	return &HealthChecker{
		dashboardDB:     dashboardDB,
		devlakeDB:       devlakeDB,
		workflowExecURL: workflowExecURL,
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

func (h *HealthChecker) LiveHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(HealthStatus{Status: "ok"}); err != nil {
		slog.Error("health response encode failed", "err", err)
	}
}

func (h *HealthChecker) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.pingDashboardDB(ctx); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(HealthStatus{Status: "not_ready"}); err != nil {
			slog.Error("health response encode failed", "err", err)
		}
		return
	}

	if err := h.pingDevlakeDB(ctx); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(HealthStatus{Status: "not_ready"}); err != nil {
			slog.Error("health response encode failed", "err", err)
		}
		return
	}

	if err := h.pingWorkflowService(ctx); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(HealthStatus{Status: "not_ready"}); err != nil {
			slog.Error("health response encode failed", "err", err)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(HealthStatus{Status: "ok"}); err != nil {
		slog.Error("health response encode failed", "err", err)
	}
}

func (h *HealthChecker) pingDashboardDB(ctx context.Context) error {
	if h.dashboardDB == nil {
		return nil
	}
	return h.dashboardDB.PingContext(ctx)
}

func (h *HealthChecker) pingDevlakeDB(ctx context.Context) error {
	if h.devlakeDB == nil {
		return nil
	}
	// Check if devlakeDB is a nil *sql.DB wrapped in the interface
	if db, ok := h.devlakeDB.(*sql.DB); ok && db == nil {
		return nil
	}
	return h.devlakeDB.PingContext(ctx)
}

func (h *HealthChecker) pingWorkflowService(ctx context.Context) error {
	if h.workflowExecURL == "" {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.workflowExecURL+"/health/live", nil)
	if err != nil {
		return err
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("workflow service returned status %d", resp.StatusCode)
	}

	_, err = io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBody))
	if err != nil {
		return err
	}

	return nil
}
