package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/fullsend-ai/orgpulse/internal/database"
	"github.com/fullsend-ai/orgpulse/internal/report"
)

func TestHealthEndpoint(t *testing.T) {
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	srv := New(0, false, fstest.MapFS{}, nil, nil, nil, FrontendConfig{}, nil, AIAssistConfig{}, nil, nil, "")
	srv.srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestHealthLiveEndpoint(t *testing.T) {
	req := httptest.NewRequest("GET", "/health/live", nil)
	w := httptest.NewRecorder()

	srv := New(0, false, fstest.MapFS{}, nil, nil, nil, FrontendConfig{}, nil, AIAssistConfig{}, nil, nil, "")
	srv.srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", body["status"])
	}
}

func TestHealthReadyEndpoint(t *testing.T) {
	req := httptest.NewRequest("GET", "/health/ready", nil)
	w := httptest.NewRecorder()

	srv := New(0, false, fstest.MapFS{}, nil, nil, nil, FrontendConfig{}, nil, AIAssistConfig{}, nil, nil, "")
	srv.srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", body["status"])
	}
}

// adminCfg authenticates every request as an administrator so endpoint tests
// exercise handler behaviour behind requireAuth/requireAdmin.
var adminCfg = FrontendConfig{DevAdminOverride: true}

func TestChartEndpoint(t *testing.T) {
	srv := New(0, false, fstest.MapFS{}, nil, nil, nil, adminCfg, nil, AIAssistConfig{}, nil, nil, "")
	body := `{"type":"bar","label":"Test","description":"t","lastupdate":1000,"data":{"labels":["A","B"],"datasets":[{"label":"S","data":[1,2]}]}}`
	req := httptest.NewRequest("POST", "/chart", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("expected Content-Type image/png, got %q", ct)
	}
	if w.Body.Len() == 0 {
		t.Error("expected non-empty response body")
	}
}

func TestChartEndpointBadJSON(t *testing.T) {
	srv := New(0, false, fstest.MapFS{}, nil, nil, nil, adminCfg, nil, AIAssistConfig{}, nil, nil, "")
	req := httptest.NewRequest("POST", "/chart", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestSendReportNotConfigured(t *testing.T) {
	srv := New(0, false, fstest.MapFS{}, nil, nil, nil, adminCfg, nil, AIAssistConfig{}, nil, nil, "")
	req := httptest.NewRequest("POST", "/sendreport", strings.NewReader(`{"subscription_id":1}`))
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestSendReportBadJSON(t *testing.T) {
	srv := New(0, false, fstest.MapFS{}, nil, nil, nil, adminCfg, nil, AIAssistConfig{}, nil, nil, "")
	req := httptest.NewRequest("POST", "/sendreport", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)

	// With nil service, the 503 fires before JSON decode
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestSendReportMissingID(t *testing.T) {
	srv := New(0, false, fstest.MapFS{}, nil, nil, nil, adminCfg, nil, AIAssistConfig{}, nil, nil, "")
	req := httptest.NewRequest("POST", "/sendreport", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)

	// With nil service, the 503 fires before validation
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestSendReport_Configured_BadJSON(t *testing.T) {
	store := database.NewMemStore()
	svc := report.NewService(store, &http.Client{Timeout: 5 * time.Second}, report.Config{
		AIAnalyticsURL: "http://localhost",
		AISummaryURL:   "http://localhost",
		SMTPHost:       "localhost",
		SMTPFrom:       "test@example.com",
	})
	srv := New(0, false, fstest.MapFS{}, nil, nil, svc, adminCfg, nil, AIAssistConfig{}, nil, nil, "")
	req := httptest.NewRequest("POST", "/sendreport", strings.NewReader("{invalid"))
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for bad JSON, got %d", w.Code)
	}
}

func TestSendReport_Configured_MissingID(t *testing.T) {
	store := database.NewMemStore()
	svc := report.NewService(store, &http.Client{Timeout: 5 * time.Second}, report.Config{
		AIAnalyticsURL: "http://localhost",
		AISummaryURL:   "http://localhost",
		SMTPHost:       "localhost",
		SMTPFrom:       "test@example.com",
	})
	srv := New(0, false, fstest.MapFS{}, nil, nil, svc, adminCfg, nil, AIAssistConfig{}, nil, nil, "")
	req := httptest.NewRequest("POST", "/sendreport", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for missing ID, got %d", w.Code)
	}
}

func TestSendReport_Configured_NotFound(t *testing.T) {
	store := database.NewMemStore()
	svc := report.NewService(store, &http.Client{Timeout: 5 * time.Second}, report.Config{
		AIAnalyticsURL: "http://localhost",
		AISummaryURL:   "http://localhost",
		SMTPHost:       "localhost",
		SMTPFrom:       "test@example.com",
	})
	srv := New(0, false, fstest.MapFS{}, nil, nil, svc, adminCfg, nil, AIAssistConfig{}, nil, nil, "")
	req := httptest.NewRequest("POST", "/sendreport", strings.NewReader(`{"subscription_id":999}`))
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for non-existent subscription, got %d", w.Code)
	}
}

func TestChartAndSendReport_RequireAuth(t *testing.T) {
	srv := New(0, false, fstest.MapFS{}, nil, nil, nil, FrontendConfig{}, nil, AIAssistConfig{}, nil, nil, "")
	for _, path := range []string{"/chart", "/sendreport"} {
		req := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		srv.srv.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s anonymous: expected 401, got %d", path, w.Code)
		}
	}
}

func TestSendReport_RequiresAdmin(t *testing.T) {
	srv := New(0, false, fstest.MapFS{}, nil, nil, nil, FrontendConfig{}, nil, AIAssistConfig{}, nil, nil, "")
	req := httptest.NewRequest("POST", "/sendreport", strings.NewReader(`{"subscription_id":1}`))
	req.Header.Set("X-Forwarded-User", "bob")
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("non-admin: expected 403, got %d", w.Code)
	}
}

func TestFrontendConfig_DoesNotExposeAdminSettings(t *testing.T) {
	srv := New(0, false, fstest.MapFS{}, nil, nil, nil, FrontendConfig{AdminUsers: []string{"alice"}, DevAdminOverride: true}, nil, AIAssistConfig{}, nil, nil, "")
	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)
	if strings.Contains(w.Body.String(), "admin_users") || strings.Contains(w.Body.String(), "dev_admin_override") || strings.Contains(w.Body.String(), "alice") {
		t.Errorf("/api/config exposes admin settings: %s", w.Body.String())
	}
}

type mockStore struct {
	orgs     []database.Org
	products []database.Product
	teams    []database.Team
}

func (m *mockStore) GetOrgs(_ context.Context) ([]database.Org, error) {
	return m.orgs, nil
}

func (m *mockStore) GetProducts(_ context.Context) ([]database.Product, error) {
	return m.products, nil
}

func (m *mockStore) GetTeams(_ context.Context) ([]database.Team, error) {
	return m.teams, nil
}

func (m *mockStore) Close() error { return nil }

func TestOrgsEndpoint(t *testing.T) {
	store := &mockStore{
		orgs: []database.Org{
			{
				ID:       "myorg",
				Name:     "My Org",
				Products: []string{"prod-a", "prod-b"},
				Dashboards: []database.Dashboard{
					{ID: "dash1", Type: "baseorgmetrics", Title: "Org Metrics"},
				},
			},
		},
	}

	srv := New(0, false, fstest.MapFS{}, store, nil, nil, FrontendConfig{DevAdminOverride: true}, nil, AIAssistConfig{}, nil, nil, "")
	req := httptest.NewRequest("GET", "/api/orgs", nil)
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var orgs []database.Org
	if err := json.NewDecoder(w.Body).Decode(&orgs); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(orgs) != 1 {
		t.Fatalf("expected 1 org, got %d", len(orgs))
	}
	if orgs[0].ID != "myorg" {
		t.Errorf("expected id=myorg, got %q", orgs[0].ID)
	}
	if len(orgs[0].Products) != 2 {
		t.Errorf("expected 2 products, got %d", len(orgs[0].Products))
	}
}

func TestProductsEndpoint(t *testing.T) {
	store := &mockStore{
		products: []database.Product{
			{
				ID:           "prod-a",
				Name:         "Product A",
				Teams:        []string{"team-1"},
				ContactEmail: "test@example.com",
			},
		},
	}

	srv := New(0, false, fstest.MapFS{}, store, nil, nil, FrontendConfig{DevAdminOverride: true}, nil, AIAssistConfig{}, nil, nil, "")
	req := httptest.NewRequest("GET", "/api/products", nil)
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var products []database.Product
	if err := json.NewDecoder(w.Body).Decode(&products); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}
	if products[0].ContactEmail != "test@example.com" {
		t.Errorf("expected contactemail, got %q", products[0].ContactEmail)
	}
}

func TestTeamsEndpoint(t *testing.T) {
	store := &mockStore{
		teams: []database.Team{
			{
				ID:   "team-1",
				Name: "Team One",
				Sources: []database.Source{
					{
						ID:   "gitrepos",
						Type: "gitrepos",
						Repos: []database.Repo{
							{ID: "r1", Type: "github", Owner: "org", Name: "repo1"},
						},
					},
				},
			},
		},
	}

	srv := New(0, false, fstest.MapFS{}, store, nil, nil, FrontendConfig{DevAdminOverride: true}, nil, AIAssistConfig{}, nil, nil, "")
	req := httptest.NewRequest("GET", "/api/teams", nil)
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var raw []json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("expected 1 team, got %d", len(raw))
	}

	var m map[string]any
	if err := json.Unmarshal(raw[0], &m); err != nil {
		t.Errorf("failed to decode: %v", err)
	}
	sources, ok := m["sources"].([]any)
	if !ok || len(sources) != 1 {
		t.Fatalf("expected 1 source, got %v", m["sources"])
	}
	src := sources[0].(map[string]any)
	if src["type"] != "gitrepos" {
		t.Errorf("expected source type=gitrepos, got %v", src["type"])
	}
	if _, ok := src["repos"]; !ok {
		t.Error("gitrepos source must include repos field")
	}
	if _, ok := src["projects"]; ok {
		t.Error("gitrepos source must not include projects field")
	}
}

func TestConfigEndpointsEmpty(t *testing.T) {
	store := &mockStore{}
	srv := New(0, false, fstest.MapFS{}, store, nil, nil, FrontendConfig{DevAdminOverride: true}, nil, AIAssistConfig{}, nil, nil, "")

	for _, path := range []string{"/api/orgs", "/api/products", "/api/teams"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		srv.srv.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", path, w.Code)
		}
		if body := strings.TrimSpace(w.Body.String()); body != "[]" {
			t.Errorf("%s: expected empty array, got %q", path, body)
		}
	}
}

func TestFrontendConfigEndpoint(t *testing.T) {
	cfg := FrontendConfig{
		AIAnalyticsURL: "https://ai.example.com/webhook",
		AdminUsers:     []string{"alice"},
	}
	srv := New(0, false, fstest.MapFS{}, nil, nil, nil, cfg, nil, AIAssistConfig{}, nil, nil, "")
	req := httptest.NewRequest("GET", "/api/config", nil)
	req.Header.Set("X-Forwarded-User", "alice")
	req.Header.Set("X-Forwarded-Email", "alice@example.com")
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var got struct {
		FrontendConfig
		UserEmail string   `json:"user_email"`
		UserName  string   `json:"user_name"`
		IsAdmin   bool     `json:"is_admin"`
		Roles     []string `json:"roles"`
	}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.AIAnalyticsURL != "https://ai.example.com/webhook" {
		t.Errorf("expected ai_analytics_url, got %q", got.AIAnalyticsURL)
	}
	if got.UserEmail != "alice@example.com" {
		t.Errorf("expected user_email=alice@example.com, got %q", got.UserEmail)
	}
	if got.UserName != "alice" {
		t.Errorf("expected user_name=alice, got %q", got.UserName)
	}
	if !got.IsAdmin {
		t.Errorf("expected is_admin=true for alice in AdminUsers")
	}
}

func TestFrontendConfigEndpoint_NoForwardedHeaders(t *testing.T) {
	srv := New(0, false, fstest.MapFS{}, nil, nil, nil, FrontendConfig{}, nil, AIAssistConfig{}, nil, nil, "")
	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)

	var got struct {
		UserEmail string   `json:"user_email"`
		UserName  string   `json:"user_name"`
		IsAdmin   bool     `json:"is_admin"`
		Roles     []string `json:"roles"`
	}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.UserEmail != "" || got.UserName != "" {
		t.Errorf("expected empty identity with no forwarded headers, got email=%q name=%q", got.UserEmail, got.UserName)
	}
	if got.IsAdmin {
		t.Errorf("expected is_admin=false with no headers")
	}
	if len(got.Roles) != 1 || got.Roles[0] != "anonymous" {
		t.Errorf("expected anonymous roles, got %v", got.Roles)
	}
}

func TestFrontendConfigEndpoint_TestMode(t *testing.T) {
	cfg := FrontendConfig{TestMode: true, TestEmail: "dev@example.com", TestIsAdmin: true}
	srv := New(0, false, fstest.MapFS{}, nil, nil, nil, cfg, nil, AIAssistConfig{}, nil, nil, "")
	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)

	var got struct {
		UserEmail string   `json:"user_email"`
		UserName  string   `json:"user_name"`
		IsAdmin   bool     `json:"is_admin"`
		Roles     []string `json:"roles"`
	}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.UserEmail != "dev@example.com" || got.UserName != "dev@example.com" {
		t.Errorf("expected test-mode identity dev@example.com, got email=%q name=%q", got.UserEmail, got.UserName)
	}
	if !got.IsAdmin {
		t.Errorf("expected is_admin=true when TestIsAdmin=true")
	}
}

func TestFrontendConfigEndpoint_DevAdminOverride(t *testing.T) {
	cfg := FrontendConfig{DevAdminOverride: true}
	srv := New(0, false, fstest.MapFS{}, nil, nil, nil, cfg, nil, AIAssistConfig{}, nil, nil, "")
	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(w, req)

	var got struct {
		UserEmail string   `json:"user_email"`
		UserName  string   `json:"user_name"`
		IsAdmin   bool     `json:"is_admin"`
		Roles     []string `json:"roles"`
	}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.IsAdmin {
		t.Errorf("expected is_admin=true with DevAdminOverride")
	}
	if got.UserName != "dev-admin" {
		t.Errorf("expected dev-admin username, got %q", got.UserName)
	}
}

func TestConfigEndpointsNotRegisteredWithoutStore(t *testing.T) {
	srv := New(0, false, fstest.MapFS{}, nil, nil, nil, FrontendConfig{}, nil, AIAssistConfig{}, nil, nil, "")

	for _, path := range []string{"/api/orgs", "/api/products", "/api/teams"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		srv.srv.Handler.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			t.Errorf("%s: expected non-200 without store, got 200", path)
		}
	}
}
