package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/fullsend-ai/orgpulse/internal/logging"
)

// requireAdmin gates /sendreport but not /chart; chart errors use the
// public helper, so even an admin sees only the generic message.
func TestChartRenderErrorHidesInternalDetailFromAdmin(t *testing.T) {
	srv := New(0, false, fstest.MapFS{}, nil, nil, nil, FrontendConfig{
		DevAdminOverride: true,
	}, nil, AIAssistConfig{}, nil, nil, "")
	req := httptest.NewRequest(http.MethodPost, "/chart", strings.NewReader(`{"type":"not-a-chart"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(withIdentity(req.Context(), Identity{Username: "admin", IsAdmin: true}))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if strings.Contains(w.Body.String(), "not-a-chart") {
		t.Errorf("response leaked internal detail: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "render error") {
		t.Errorf("response lacks public message: %s", w.Body.String())
	}
}

func TestWritePublicErrorLogsRequestID(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	ctx := logging.WithRequestID(logging.WithLogger(context.Background(), logger), "rid-42")

	r := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	writePublicError(w, r, http.StatusInternalServerError, "report error", errors.New("dial tcp 10.0.0.5"))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "10.0.0.5") {
		t.Errorf("response leaked cause: %s", w.Body.String())
	}

	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("decode log: %v (log %q)", err, logs.String())
	}
	if entry["request_id"] != "rid-42" {
		t.Errorf("request_id = %v, want rid-42", entry["request_id"])
	}
	if entry["path"] != "/x" {
		t.Errorf("path = %v, want /x", entry["path"])
	}
}

func TestWritePublicErrorHidesCause(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/sendreport", nil)
	writePublicError(w, r, http.StatusInternalServerError, "report error", errors.New("dial tcp 10.0.0.5:25: connection refused"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "10.0.0.5") {
		t.Errorf("response leaked cause: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "report error") {
		t.Errorf("response lacks public message: %s", w.Body.String())
	}
}
