package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fullsend-ai/orgpulse/internal/database"
	"github.com/fullsend-ai/orgpulse/internal/devlake"
	"github.com/fullsend-ai/orgpulse/schemas"
)

func TestCodeCoverageTemplateUsesProductionWorkflows(t *testing.T) {
	var templates []struct {
		DashboardID string `json:"dashboard_id"`
		Diagrams    []struct {
			Position int    `json:"position"`
			Endpoint string `json:"endpoint"`
		} `json:"diagrams"`
	}
	if err := json.Unmarshal([]byte(schemas.DashboardTemplatesJSON), &templates); err != nil {
		t.Fatalf("decode dashboard templates: %v", err)
	}

	want := []string{
		"/workflows/production/coveragekeymetrics",
		"/workflows/production/coveragetrend",
		"/workflows/production/coveragelinebreakdown",
	}
	for _, template := range templates {
		if template.DashboardID != "codecoverage" {
			continue
		}
		if len(template.Diagrams) != len(want) {
			t.Fatalf("Code Coverage diagram count = %d, want %d", len(template.Diagrams), len(want))
		}
		for i, diagram := range template.Diagrams {
			if diagram.Position != i || diagram.Endpoint != want[i] {
				t.Errorf("diagram %d = position %d, endpoint %q; want position %d, endpoint %q", i, diagram.Position, diagram.Endpoint, i, want[i])
			}
		}
		return
	}
	t.Fatal("Code Coverage template missing")
}

func TestExtractAPIKeyBearer(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer test-key-123")
	if got := extractAPIKey(r); got != "test-key-123" {
		t.Errorf("expected test-key-123, got %q", got)
	}
}

func TestExtractAPIKeyHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-API-Key", "my-key")
	if got := extractAPIKey(r); got != "my-key" {
		t.Errorf("expected my-key, got %q", got)
	}
}

func TestExtractAPIKeyEmpty(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	if got := extractAPIKey(r); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestCrudListHandler(t *testing.T) {
	items := []database.OrgRow{
		{ID: "org1", Name: "Org One"},
		{ID: "org2", Name: "Org Two"},
	}
	handler := crudList(func(_ context.Context) ([]database.OrgRow, error) {
		return items, nil
	})

	req := httptest.NewRequest("GET", "/api/v1/orgs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result []database.OrgRow
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Errorf("unmarshal orgs: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 orgs, got %d", len(result))
	}
	if result[0].ID != "org1" {
		t.Errorf("expected org1, got %q", result[0].ID)
	}
}

func TestCrudListHandlerEmpty(t *testing.T) {
	handler := crudList(func(_ context.Context) ([]database.OrgRow, error) {
		return nil, nil
	})

	req := httptest.NewRequest("GET", "/api/v1/orgs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if body := strings.TrimSpace(w.Body.String()); body != "[]" {
		t.Errorf("expected empty array, got %q", body)
	}
}

func TestCrudGetHandler(t *testing.T) {
	org := &database.OrgRow{ID: "org1", Name: "Org One"}
	handler := crudGet(func(_ context.Context, id string) (*database.OrgRow, error) {
		if id == "org1" {
			return org, nil
		}
		return nil, nil
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orgs/{id}", handler)

	req := httptest.NewRequest("GET", "/orgs/org1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result database.OrgRow
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Errorf("unmarshal org: %v", err)
	}
	if result.ID != "org1" {
		t.Errorf("expected org1, got %q", result.ID)
	}
}

func TestCrudGetHandlerNotFound(t *testing.T) {
	handler := crudGet(func(_ context.Context, _ string) (*database.OrgRow, error) {
		return nil, nil
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orgs/{id}", handler)

	req := httptest.NewRequest("GET", "/orgs/nope", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestCrudCreateHandler(t *testing.T) {
	var created database.OrgRow
	handler := crudCreate(
		func(_ context.Context, input database.OrgRow) error {
			created = input
			return nil
		},
		func(r database.OrgRow) string { return r.ID },
	)

	body := `{"id":"new-org","name":"New Org"}`
	req := httptest.NewRequest("POST", "/api/v1/orgs", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if created.ID != "new-org" {
		t.Errorf("expected new-org, got %q", created.ID)
	}
}

func TestCrudCreateHandlerBadJSON(t *testing.T) {
	handler := crudCreate(
		func(_ context.Context, _ database.OrgRow) error { return nil },
		func(r database.OrgRow) string { return r.ID },
	)

	req := httptest.NewRequest("POST", "/api/v1/orgs", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestEntityCreateHandlersRejectDuplicates(t *testing.T) {
	tests := []struct {
		name    string
		seed    func(*database.MemStore) error
		handler func(*database.MemStore) http.Handler
		body    string
	}{
		{
			name: "organization",
			seed: func(store *database.MemStore) error {
				return store.CreateOrg(context.Background(), database.OrgRow{ID: "existing-id", Name: "Existing"})
			},
			handler: func(store *database.MemStore) http.Handler {
				return crudCreate(store.CreateOrg, func(row database.OrgRow) string { return row.ID })
			},
			body: `{"id":"existing-id","name":"Duplicate"}`,
		},
		{
			name: "product",
			seed: func(store *database.MemStore) error {
				return store.CreateProduct(context.Background(), database.ProductRow{ID: "existing-id", Name: "Existing"})
			},
			handler: func(store *database.MemStore) http.Handler {
				return productCreateHandler(store)
			},
			body: `{"id":"existing-id","name":"Duplicate"}`,
		},
		{
			name: "team",
			seed: func(store *database.MemStore) error {
				return store.CreateTeam(context.Background(), database.TeamRow{ID: "existing-id", Name: "Existing"})
			},
			handler: func(store *database.MemStore) http.Handler {
				return teamCreateHandler(store)
			},
			body: `{"id":"existing-id","name":"Duplicate","auth_group":"g"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := database.NewMemStore()
			if err := tt.seed(store); err != nil {
				t.Fatalf("seed entity: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			w := httptest.NewRecorder()
			tt.handler(store).ServeHTTP(w, req)
			if w.Code != http.StatusConflict {
				t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestCrudDeleteHandler(t *testing.T) {
	handler := crudDelete(func(_ context.Context, id string) error {
		if id == "org1" {
			return nil
		}
		return database.ErrNotFound
	})

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /orgs/{id}", handler)

	req := httptest.NewRequest("DELETE", "/orgs/org1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
}

func TestCrudDeleteHandlerNotFound(t *testing.T) {
	handler := crudDelete(func(_ context.Context, _ string) error {
		return database.ErrNotFound
	})

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /orgs/{id}", handler)

	req := httptest.NewRequest("DELETE", "/orgs/nope", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestWriteErrorJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadRequest, "test error")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Errorf("unmarshal error: %v", err)
	}
	if result["error"] != "test error" {
		t.Errorf("expected 'test error', got %q", result["error"])
	}
}

func TestGenerateAPIKey(t *testing.T) {
	key1, err := generateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(key1) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(key1))
	}

	key2, _ := generateAPIKey()
	if key1 == key2 {
		t.Error("expected unique keys")
	}
}

func TestHashAPIKey(t *testing.T) {
	hash := database.HashAPIKey("test-key")
	if len(hash) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(hash))
	}
	if database.HashAPIKey("test-key") != hash {
		t.Error("hash should be deterministic")
	}
	if database.HashAPIKey("other-key") == hash {
		t.Error("different keys should produce different hashes")
	}
}

func TestRelGetHandler(t *testing.T) {
	handler := relGet(func(_ context.Context, id string) ([]database.RelEntry, error) {
		return []database.RelEntry{{ID: "prod-a", SortOrder: 0}}, nil
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orgs/{id}/products", handler)

	req := httptest.NewRequest("GET", "/orgs/myorg/products", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result []database.RelEntry
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Errorf("unmarshal result: %v", err)
	}
	if len(result) != 1 || result[0].ID != "prod-a" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestRelPutHandler(t *testing.T) {
	var saved []database.RelEntry
	handler := relPut(func(_ context.Context, _ string, entries []database.RelEntry) error {
		saved = entries
		return nil
	}, func(_ context.Context, _ string) ([]database.RelEntry, error) {
		return saved, nil
	})

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /orgs/{id}/products", handler)

	body := `[{"id":"prod-a","sort_order":0},{"id":"prod-b","sort_order":1}]`
	req := httptest.NewRequest("PUT", "/orgs/myorg/products", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(saved) != 2 {
		t.Errorf("expected 2 entries, got %d", len(saved))
	}
}

func TestConfigGetHandler(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/config/{type}/{id}", configGetHandler(nil))

	req := httptest.NewRequest("GET", "/api/v1/config/invalid/x", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid type, got %d", w.Code)
	}
}

func TestConfigPutHandler_PreservesDevlakeKeys(t *testing.T) {
	store := database.NewMemStore()
	if err := store.SetEntityConfigKey(context.Background(), "team", "t1", "devlake_github_connection_id", "4"); err != nil {
		t.Errorf("set entity config key: %v", err)
	}
	if err := store.SetEntityConfigKey(context.Background(), "team", "t1", "devlake_github_token_expiry", "2026-09-06"); err != nil {
		t.Errorf("set entity config key: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/config/{type}/{id}", configPutHandler(store))

	body := jsonBodyForTest(t, map[string]string{"cycle_time_slow": "48"})
	r := httptest.NewRequest("PUT", "/api/v1/config/team/t1", body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	all, _ := store.GetEntityConfigKV(context.Background(), "team", "t1")
	if all["devlake_github_connection_id"] != "4" {
		t.Error("devlake_github_connection_id should be preserved")
	}
	if all["devlake_github_token_expiry"] != "2026-09-06" {
		t.Error("devlake_github_token_expiry should be preserved")
	}
	if all["cycle_time_slow"] != "48" {
		t.Errorf("expected cycle_time_slow=48, got %q", all["cycle_time_slow"])
	}
}

func TestRequireAuth_ForwardedIdentity_Authenticates(t *testing.T) {
	store := database.NewMemStore()
	var gotIdentity Identity
	var called bool

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		gotIdentity, _ = identityFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := requireAuth(store, AuthConfig{}, nil)(next)

	r := httptest.NewRequest("GET", "/api/v1/orgs", nil)
	r.Header.Set("X-Forwarded-User", "alice")
	r.Header.Set("X-Forwarded-Email", "alice@example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if !called {
		t.Fatal("expected next handler to be called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	want := Identity{Username: "alice", Email: "alice@example.com"}
	if gotIdentity.Username != want.Username || gotIdentity.Email != want.Email {
		t.Errorf("expected identity %+v in context, got %+v", want, gotIdentity)
	}
}

func TestRequireAuth_NoIdentityNoKey_Unauthorized(t *testing.T) {
	store := database.NewMemStore()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	})

	handler := requireAuth(store, AuthConfig{}, nil)(next)

	r := httptest.NewRequest("GET", "/api/v1/orgs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRequireAuth_ValidAPIKey_FallsThroughWhenNoIdentity(t *testing.T) {
	ctx := context.Background()
	store := database.NewMemStore()
	_, err := store.CreateAPIKey(ctx, "test-key", database.HashAPIKey("a-valid-key"), false, "")
	if err != nil {
		t.Errorf("create API key: %v", err)
	}

	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := requireAuth(store, AuthConfig{}, nil)(next)

	r := httptest.NewRequest("GET", "/api/v1/orgs", nil)
	r.Header.Set("X-API-Key", "a-valid-key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if !called {
		t.Fatal("expected next handler to be called for a valid API key")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRequireAuth_TestMode_InjectsTestIdentity(t *testing.T) {
	store := database.NewMemStore()
	var gotIdentity Identity

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIdentity, _ = identityFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	cfg := AuthConfig{TestMode: true, TestEmail: "dev@example.com"}
	handler := requireAuth(store, cfg, nil)(next)

	r := httptest.NewRequest("GET", "/api/v1/orgs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if gotIdentity.Username != "dev@example.com" || gotIdentity.Email != "dev@example.com" {
		t.Errorf("expected test identity in context, got %+v", gotIdentity)
	}
}

func TestRequireAdmin_AdminUserAllowed(t *testing.T) {
	store := database.NewMemStore()
	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	cfg := AuthConfig{AdminUsers: []string{"alice"}}
	handler := requireAdmin(store, cfg, nil)(next)

	r := httptest.NewRequest("POST", "/api/v1/orgs", nil)
	r.Header.Set("X-Forwarded-User", "alice")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if !called {
		t.Fatal("expected next handler to be called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRequireAdmin_NonAdminForbidden(t *testing.T) {
	store := database.NewMemStore()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	})

	cfg := AuthConfig{AdminUsers: []string{"alice"}}
	handler := requireAdmin(store, cfg, nil)(next)

	r := httptest.NewRequest("POST", "/api/v1/orgs", nil)
	r.Header.Set("X-Forwarded-User", "bob")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestRequireAdmin_DevAdminOverride_Allowed(t *testing.T) {
	store := database.NewMemStore()
	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	cfg := AuthConfig{DevAdminOverride: true}
	handler := requireAdmin(store, cfg, nil)(next)

	r := httptest.NewRequest("POST", "/api/v1/orgs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if !called {
		t.Fatal("expected next handler to be called with dev override")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAuthMeEndpoint(t *testing.T) {
	store := database.NewMemStore()
	cfg := AuthConfig{AdminUsers: []string{"alice"}}

	handler := authMeHandler(store, cfg, nil)

	// Case 1: Forwarded admin user
	r := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	r.Header.Set("X-Forwarded-User", "alice")
	r.Header.Set("X-Forwarded-Email", "alice@example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Errorf("unmarshal resp: %v", err)
	}
	if resp["authenticated"] != true || resp["username"] != "alice" || resp["is_admin"] != true {
		t.Errorf("unexpected resp for alice: %+v", resp)
	}

	// Case 2: Anonymous
	r = httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Errorf("unmarshal resp: %v", err)
	}
	if resp["authenticated"] != false || resp["is_admin"] != false || resp["auth_type"] != "anonymous" {
		t.Errorf("unexpected resp for anonymous: %+v", resp)
	}
}

func TestSubscriptionCreateHandler_AutoFillsEmailFromIdentity(t *testing.T) {
	ctx := context.Background()
	store := database.NewMemStore()

	handler := subscriptionCreateHandler(store, testReportURLs)

	body := jsonBodyForTest(t, database.SubscriptionRow{ProductID: "prod-1"})
	r := httptest.NewRequest("POST", "/api/v1/subscriptions", body)
	r = r.WithContext(withIdentity(ctx, Identity{Username: "alice", Email: "alice@example.com", Roles: []string{"contributor"}}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var got database.SubscriptionRow
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Email != "alice@example.com" {
		t.Errorf("expected email auto-filled from identity, got %q", got.Email)
	}
}

func TestSubscriptionCreateHandler_NonAdmin_CannotCreateForOtherUser(t *testing.T) {
	ctx := context.Background()
	store := database.NewMemStore()

	handler := subscriptionCreateHandler(store, testReportURLs)

	body := jsonBodyForTest(t, database.SubscriptionRow{ProductID: "prod-1", Email: "other@example.com"})
	r := httptest.NewRequest("POST", "/api/v1/subscriptions", body)
	r = r.WithContext(withIdentity(ctx, Identity{Username: "alice", Email: "alice@example.com", IsAdmin: false}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 when non-admin creates sub for other user, got %d", w.Code)
	}
}

func TestSubscriptionCreateHandler_Admin_CanCreateForOtherUser(t *testing.T) {
	ctx := context.Background()
	store := database.NewMemStore()

	handler := subscriptionCreateHandler(store, testReportURLs)

	body := jsonBodyForTest(t, database.SubscriptionRow{ProductID: "prod-1", Email: "other@example.com"})
	r := httptest.NewRequest("POST", "/api/v1/subscriptions", body)
	r = r.WithContext(withIdentity(ctx, Identity{Username: "admin", Email: "admin@example.com", IsAdmin: true}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201 when admin creates sub for other user, got %d", w.Code)
	}
}

func TestSubscriptionCreateHandler_NoIdentityNoEmail_StillRequiresEmail(t *testing.T) {
	store := database.NewMemStore()
	handler := subscriptionCreateHandler(store, testReportURLs)

	body := jsonBodyForTest(t, database.SubscriptionRow{ProductID: "prod-1"})
	r := httptest.NewRequest("POST", "/api/v1/subscriptions", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when no email is available from any source, got %d", w.Code)
	}
}

func TestSubscriptionCreateHandler_TolerantOfKnownIdentityWithNoEmail(t *testing.T) {
	store := database.NewMemStore()
	handler := subscriptionCreateHandler(store, testReportURLs)

	body := jsonBodyForTest(t, database.SubscriptionRow{ProductID: "prod-1"})
	r := httptest.NewRequest("POST", "/api/v1/subscriptions", body)
	r = r.WithContext(withIdentity(r.Context(), Identity{Username: "alice"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when identity has no email, got %d", w.Code)
	}
}

func TestAlertCreateHandler_AutoFillsEmailFromIdentity(t *testing.T) {
	store := database.NewMemStore()
	handler := alertCreateHandler(store, testReportURLs)

	body := jsonBodyForTest(t, database.AlertRow{URL: "https://example.com/metrics", Rule: "data.value > 1"})
	r := httptest.NewRequest("POST", "/api/v1/alerts", body)
	r = r.WithContext(withIdentity(r.Context(), Identity{Username: "alice", Email: "alice@example.com"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var got database.AlertRow
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Email != "alice@example.com" {
		t.Errorf("expected email auto-filled from identity, got %q", got.Email)
	}
}

func TestAlertCreateHandler_NonAdmin_CannotCreateForOtherUser(t *testing.T) {
	ctx := context.Background()
	store := database.NewMemStore()

	handler := alertCreateHandler(store, testReportURLs)

	body := jsonBodyForTest(t, database.AlertRow{URL: "https://example.com/metrics", Rule: "data.value > 1", Email: "other@example.com"})
	r := httptest.NewRequest("POST", "/api/v1/alerts", body)
	r = r.WithContext(withIdentity(ctx, Identity{Username: "alice", Email: "alice@example.com", IsAdmin: false}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 when non-admin creates alert for other user, got %d", w.Code)
	}
}

func TestSubscriptionList_Scoping(t *testing.T) {
	ctx := context.Background()
	store := database.NewMemStore()
	_, err := store.CreateSubscription(ctx, database.SubscriptionRow{Email: "alice@example.com", ProductID: "p1"})
	if err != nil {
		t.Errorf("create subscription: %v", err)
	}
	_, err = store.CreateSubscription(ctx, database.SubscriptionRow{Email: "bob@example.com", ProductID: "p2"})
	if err != nil {
		t.Errorf("create subscription: %v", err)
	}

	handler := subscriptionListHandler(store)

	// Non-admin alice only sees alice's subscription
	r := httptest.NewRequest("GET", "/api/v1/subscriptions", nil)
	r = r.WithContext(withIdentity(ctx, Identity{Username: "alice", Email: "alice@example.com", IsAdmin: false}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var rows []database.SubscriptionRow
	if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
		t.Errorf("unmarshal subscriptions: %v", err)
	}
	if len(rows) != 1 || rows[0].Email != "alice@example.com" {
		t.Errorf("expected 1 sub for alice, got %+v", rows)
	}

	// Admin sees all subscriptions
	r = httptest.NewRequest("GET", "/api/v1/subscriptions", nil)
	r = r.WithContext(withIdentity(ctx, Identity{Username: "admin", Email: "admin@example.com", IsAdmin: true}))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
		t.Errorf("unmarshal subscriptions: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 subs for admin, got %d", len(rows))
	}
}

func TestSubscriptionGetUpdateDelete_Scoping(t *testing.T) {
	ctx := context.Background()
	store := database.NewMemStore()
	id, _ := store.CreateSubscription(ctx, database.SubscriptionRow{Email: "alice@example.com", ProductID: "p1"})

	getHandler := subscriptionGetHandler(store)
	updateHandler := subscriptionUpdateHandler(store, testReportURLs)
	deleteHandler := subscriptionDeleteHandler(store)

	// bob trying to GET alice's subscription -> 403
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/subscriptions/{id}", getHandler)
	mux.HandleFunc("PUT /api/v1/subscriptions/{id}", updateHandler)
	mux.HandleFunc("DELETE /api/v1/subscriptions/{id}", deleteHandler)

	r := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/subscriptions/%d", id), nil)
	r = r.WithContext(withIdentity(ctx, Identity{Username: "bob", Email: "bob@example.com", IsAdmin: false}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for bob accessing alice's sub, got %d", w.Code)
	}

	// bob trying to PUT alice's subscription -> 403
	body := jsonBodyForTest(t, database.SubscriptionRow{Email: "bob@example.com", ProductID: "p2"})
	r = httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/subscriptions/%d", id), body)
	r = r.WithContext(withIdentity(ctx, Identity{Username: "bob", Email: "bob@example.com", IsAdmin: false}))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for bob updating alice's sub, got %d", w.Code)
	}

	// bob trying to DELETE alice's subscription -> 403
	r = httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/subscriptions/%d", id), nil)
	r = r.WithContext(withIdentity(ctx, Identity{Username: "bob", Email: "bob@example.com", IsAdmin: false}))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for bob deleting alice's sub, got %d", w.Code)
	}

	// admin can delete alice's subscription -> 204
	r = httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/subscriptions/%d", id), nil)
	r = r.WithContext(withIdentity(ctx, Identity{Username: "admin", Email: "admin@example.com", IsAdmin: true}))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 for admin deleting sub, got %d", w.Code)
	}
}

func TestSubscriptionCreateHandler_InvalidEmail(t *testing.T) {
	ctx := context.Background()
	store := database.NewMemStore()
	handler := subscriptionCreateHandler(store, testReportURLs)

	body := jsonBodyForTest(t, database.SubscriptionRow{ProductID: "prod-1", Email: "not-an-email"})
	r := httptest.NewRequest("POST", "/api/v1/subscriptions", body)
	r = r.WithContext(withIdentity(ctx, Identity{Username: "admin", Email: "admin@example.com", IsAdmin: true}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid email, got %d", w.Code)
	}
}

func TestAlertCreateHandler_InvalidEmail(t *testing.T) {
	ctx := context.Background()
	store := database.NewMemStore()
	handler := alertCreateHandler(store, testReportURLs)

	body := jsonBodyForTest(t, database.AlertRow{URL: "https://example.com", Rule: "true", Email: "not-an-email"})
	r := httptest.NewRequest("POST", "/api/v1/alerts", body)
	r = r.WithContext(withIdentity(ctx, Identity{Username: "admin", Email: "admin@example.com", IsAdmin: true}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid email, got %d", w.Code)
	}
}

func TestAlertList_Scoping(t *testing.T) {
	ctx := context.Background()
	store := database.NewMemStore()
	_, err := store.CreateAlert(ctx, database.AlertRow{Email: "alice@example.com", URL: "https://example.com/1", Rule: "true"})
	if err != nil {
		t.Errorf("create alert: %v", err)
	}
	_, err = store.CreateAlert(ctx, database.AlertRow{Email: "bob@example.com", URL: "https://example.com/2", Rule: "true"})
	if err != nil {
		t.Errorf("create alert: %v", err)
	}

	handler := alertListHandler(store)

	// Non-admin alice only sees alice's alert
	r := httptest.NewRequest("GET", "/api/v1/alerts", nil)
	r = r.WithContext(withIdentity(ctx, Identity{Username: "alice", Email: "alice@example.com", IsAdmin: false}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var rows []database.AlertRow
	if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
		t.Errorf("unmarshal alerts: %v", err)
	}
	if len(rows) != 1 || rows[0].Email != "alice@example.com" {
		t.Errorf("expected 1 alert for alice, got %+v", rows)
	}

	// Admin sees all alerts
	r = httptest.NewRequest("GET", "/api/v1/alerts", nil)
	r = r.WithContext(withIdentity(ctx, Identity{Username: "admin", Email: "admin@example.com", IsAdmin: true}))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
		t.Errorf("unmarshal alerts: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 alerts for admin, got %d", len(rows))
	}
}

func TestAlertGetUpdateDelete_Scoping(t *testing.T) {
	ctx := context.Background()
	store := database.NewMemStore()
	id, _ := store.CreateAlert(ctx, database.AlertRow{Email: "alice@example.com", URL: "https://example.com/1", Rule: "true"})

	getHandler := alertGetHandler(store)
	updateHandler := alertUpdateHandler(store, testReportURLs)
	deleteHandler := alertDeleteHandler(store)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/alerts/{id}", getHandler)
	mux.HandleFunc("PUT /api/v1/alerts/{id}", updateHandler)
	mux.HandleFunc("DELETE /api/v1/alerts/{id}", deleteHandler)

	// bob trying to GET alice's alert -> 403
	r := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/alerts/%d", id), nil)
	r = r.WithContext(withIdentity(ctx, Identity{Username: "bob", Email: "bob@example.com", IsAdmin: false}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for bob accessing alice's alert, got %d", w.Code)
	}

	// bob trying to PUT alice's alert -> 403
	body := jsonBodyForTest(t, database.AlertRow{Email: "bob@example.com", URL: "https://example.com/updated", Rule: "false"})
	r = httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/alerts/%d", id), body)
	r = r.WithContext(withIdentity(ctx, Identity{Username: "bob", Email: "bob@example.com", IsAdmin: false}))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for bob updating alice's alert, got %d", w.Code)
	}

	// bob trying to DELETE alice's alert -> 403
	r = httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/alerts/%d", id), nil)
	r = r.WithContext(withIdentity(ctx, Identity{Username: "bob", Email: "bob@example.com", IsAdmin: false}))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for bob deleting alice's alert, got %d", w.Code)
	}

	// admin can delete alice's alert -> 204
	r = httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/alerts/%d", id), nil)
	r = r.WithContext(withIdentity(ctx, Identity{Username: "admin", Email: "admin@example.com", IsAdmin: true}))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 for admin deleting alert, got %d", w.Code)
	}
}

func jsonBodyForTest(t *testing.T, v any) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bytes.NewReader(b)
}

func TestSourceListJiraConnectionScope(t *testing.T) {
	store := database.NewMemStore()
	ctx := context.Background()
	for _, tc := range []struct {
		team       string
		connection string
	}{
		{team: "team-a", connection: "1"},
		{team: "team-b", connection: "2"},
		{team: "unconfigured"},
	} {
		t.Run(tc.team, func(t *testing.T) {
			if err := store.CreateSource(ctx, tc.team, database.SourceRow{
				ID: "issues", Type: "workitems",
				Projects: []database.WorkitemProjRow{{ID: "project", Type: "jira", JiraProject: "PROJECT", BoardID: "10"}},
			}); err != nil {
				t.Fatal(err)
			}
			if err := store.SetEntityConfigKey(ctx, "team", tc.team, "devlake_jira_connection_id", tc.connection); err != nil {
				t.Fatal(err)
			}
			if err := store.SetEntityConfigKey(ctx, "team", tc.team, "devlake_jira_token", "private-token"); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("GET", "/", nil)
			r.SetPathValue("teamId", tc.team)
			w := httptest.NewRecorder()
			sourceListHandler(store).ServeHTTP(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			var rows []sourceResponse
			if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].JiraConnectionID != tc.connection || rows[0].Projects[0].BoardID != "10" {
				t.Fatalf("unexpected source scope: %+v", rows)
			}
			if strings.Contains(w.Body.String(), "private-token") {
				t.Fatal("team credentials included in source response")
			}
		})
	}
	request := httptest.NewRequest("GET", "/", nil)
	request.SetPathValue("teamId", "empty")
	w := httptest.NewRecorder()
	sourceListHandler(store).ServeHTTP(w, request)
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("empty team response: %d %s", w.Code, w.Body.String())
	}
}

// ---- Onboard handler tests ----

func TestOnboardHandler_Success(t *testing.T) {
	store := database.NewMemStore()
	handler := onboardHandler(store, nil)

	payload := onboardRequest{
		Team: database.TeamRow{ID: "my-team", Name: "My Team", AuthGroup: "my-group"},
		Connections: []devlake.ConnectionEntry{
			{Plugin: "github", Token: "ghp_test", Endpoint: "https://api.github.com/", ExpiryDate: "2027-01-01"},
		},
		Sources: []database.SourceRow{
			{ID: "repos", Type: "gitrepos", Repos: []database.RepoRow{
				{ID: "r1", Type: "github", Owner: "myorg", Name: "myrepo"},
			}},
		},
		Dashboards: []database.DashboardRow{
			{DashboardID: "d1", Type: "grafana", Title: "Main"},
		},
		Config: map[string]string{"key1": "val1"},
	}

	r := httptest.NewRequest("POST", "/api/v1/onboard", jsonBodyForTest(t, payload))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Errorf("unmarshal response: %v", err)
	}
	if resp["team_id"] != "my-team" || resp["status"] != "created" {
		t.Errorf("unexpected response: %+v", resp)
	}

	ctx := context.Background()
	team, _ := store.GetTeamRow(ctx, "my-team")
	if team == nil || team.Name != "My Team" {
		t.Error("team was not created in store")
	}

	sources, _ := store.ListSourceRows(ctx, "my-team")
	if len(sources) != 1 || sources[0].ID != "repos" {
		t.Errorf("expected 1 source, got %+v", sources)
	}

	dashboards, _ := store.ListDashboardRows(ctx, "team", "my-team")
	if len(dashboards) != 1 || dashboards[0].DashboardID != "d1" {
		t.Errorf("expected 1 dashboard, got %+v", dashboards)
	}

	cfg, _ := store.GetEntityConfigKV(ctx, "team", "my-team")
	if cfg["key1"] != "val1" {
		t.Errorf("expected config key1=val1, got %+v", cfg)
	}
}

func TestOnboardHandler_DuplicateTeamID(t *testing.T) {
	store := database.NewMemStore()
	if err := store.CreateTeam(context.Background(), database.TeamRow{ID: "Existing-Team", Name: "Existing"}); err != nil {
		t.Fatalf("seed team: %v", err)
	}
	handler := onboardHandler(store, nil)
	payload := onboardRequest{Team: database.TeamRow{ID: "existing-team", Name: "Duplicate", AuthGroup: "g"}}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/onboard", jsonBodyForTest(t, payload))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `team id \"existing-team\" already exists`) {
		t.Fatalf("unexpected error response: %s", w.Body.String())
	}
}

func TestOnboardConnectionFailureHidesDetail(t *testing.T) {
	devlakeAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "driver detail 10.0.0.5", http.StatusInternalServerError)
	}))
	t.Cleanup(devlakeAPI.Close)

	cases := []struct {
		name       string
		identity   Identity
		wantDetail bool
	}{
		{name: "non-admin", identity: Identity{Username: "user", Email: "user@example.com"}},
		{name: "admin", identity: Identity{Username: "admin", IsAdmin: true}, wantDetail: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := database.NewMemStore()
			svc := devlake.NewService(devlake.Config{APIURL: devlakeAPI.URL, AllowPrivateEndpoints: true}, devlakeAPI.Client(), store)
			handler := onboardHandler(store, svc)
			payload := onboardRequest{
				Team: database.TeamRow{ID: "fail-team", Name: "Fail", AuthGroup: "g", ContactEmail: "user@example.com"},
				Connections: []devlake.ConnectionEntry{
					{Plugin: "github", Token: "x", Endpoint: "https://api.github.com/", ExpiryDate: "2027-01-01"},
				},
			}
			r := httptest.NewRequest(http.MethodPost, "/api/v1/onboard", jsonBodyForTest(t, payload))
			r = r.WithContext(withIdentity(context.Background(), tc.identity))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			body := w.Body.String()
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500: %s", w.Code, body)
			}
			if !strings.Contains(body, "create connections failed") {
				t.Errorf("body lacks public message: %s", body)
			}
			if !tc.wantDetail && strings.Contains(body, "driver detail 10.0.0.5") {
				t.Errorf("body leaked endpoint detail: %s", body)
			}
			if got := strings.Contains(body, "create github connection"); got != tc.wantDetail {
				t.Errorf("operation detail present = %v, want %v (body %s)", got, tc.wantDetail, body)
			}
		})
	}
}

func TestMyIssueTransitionFailureReturns502WithoutDetail(t *testing.T) {
	jira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "jira upstream secret", http.StatusInternalServerError)
	}))
	t.Cleanup(jira.Close)

	store := database.NewMemStore()
	ctx := context.Background()
	for key, value := range map[string]string{
		"personal_jira_token":    "local-only",
		"personal_jira_endpoint": jira.URL,
		"personal_jira_username": "user@example.com",
	} {
		if err := store.SetEntityConfigKey(ctx, "user", "user@example.com", key, value); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}
	handler := myIssueTransitionHandler(store, outboundConfig{AllowPrivate: true})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/my-issues/transition",
		jsonBodyForTest(t, map[string]string{"source": "jira", "source_id": "ABC-1", "transition_id": "31"}))
	r = r.WithContext(withIdentity(ctx, Identity{Username: "user", Email: "user@example.com"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	body := w.Body.String()
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", w.Code, body)
	}
	if !strings.Contains(body, "transition failed") {
		t.Errorf("body lacks public message: %s", body)
	}
	if strings.Contains(body, "jira upstream secret") {
		t.Errorf("body leaked upstream detail: %s", body)
	}
}

func TestOnboardHandler_NonAdminUsesOAuthContactEmail(t *testing.T) {
	store := database.NewMemStore()
	handler := onboardHandler(store, nil)
	payload := onboardRequest{Team: database.TeamRow{
		ID:           "oauth-team",
		Name:         "OAuth Team",
		AuthGroup:    "oauth-group",
		ContactEmail: "entered@redhat.com",
	}}
	r := httptest.NewRequest("POST", "/api/v1/onboard", jsonBodyForTest(t, payload))
	r = r.WithContext(withIdentity(r.Context(), Identity{
		Username: "test-user",
		Email:    "test-user@cluster.local",
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	team, err := store.GetTeamRow(context.Background(), "oauth-team")
	if err != nil || team == nil {
		t.Fatalf("get team: %v", err)
	}
	if team.ContactEmail != "test-user@cluster.local" {
		t.Fatalf("expected OAuth contact email, got %q", team.ContactEmail)
	}
}

func TestOnboardHandler_AdminPreservesAssignedContactEmail(t *testing.T) {
	store := database.NewMemStore()
	handler := onboardHandler(store, nil)
	payload := onboardRequest{Team: database.TeamRow{
		ID:           "assigned-team",
		Name:         "Assigned Team",
		AuthGroup:    "assigned-group",
		ContactEmail: "owner@redhat.com",
	}}
	r := httptest.NewRequest("POST", "/api/v1/onboard", jsonBodyForTest(t, payload))
	r = r.WithContext(withIdentity(r.Context(), Identity{
		Username: "admin",
		Email:    "admin@cluster.local",
		IsAdmin:  true,
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	team, err := store.GetTeamRow(context.Background(), "assigned-team")
	if err != nil || team == nil {
		t.Fatalf("get team: %v", err)
	}
	if team.ContactEmail != "owner@redhat.com" {
		t.Fatalf("expected assigned contact email, got %q", team.ContactEmail)
	}
}

func TestOnboardHandler_AdminRejectsInvalidContactEmail(t *testing.T) {
	store := database.NewMemStore()
	handler := onboardHandler(store, nil)
	payload := onboardRequest{Team: database.TeamRow{
		ID:           "invalid-contact-team",
		Name:         "Invalid Contact Team",
		ContactEmail: "not-an-email",
	}}
	r := httptest.NewRequest("POST", "/api/v1/onboard", jsonBodyForTest(t, payload))
	r = r.WithContext(withIdentity(r.Context(), Identity{
		Username: "admin",
		Email:    "admin@cluster.local",
		IsAdmin:  true,
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	team, err := store.GetTeamRow(context.Background(), "invalid-contact-team")
	if err != nil {
		t.Fatalf("get team: %v", err)
	}
	if team != nil {
		t.Fatal("invalid team must not be created")
	}
}

func TestTeamUpdateHandler_NonAdminUsesOAuthContactEmail(t *testing.T) {
	store := database.NewMemStore()
	if err := store.CreateTeam(context.Background(), database.TeamRow{
		ID: "editable-team", Name: "Editable Team", ContactEmail: "test-user@cluster.local",
	}); err != nil {
		t.Fatalf("create team: %v", err)
	}
	handler := teamUpdateHandler(store)
	payload := database.TeamRow{Name: "Updated Team", ContactEmail: "entered@redhat.com", AuthGroup: "g"}
	r := httptest.NewRequest("PUT", "/api/v1/teams/editable-team", jsonBodyForTest(t, payload))
	r.SetPathValue("id", "editable-team")
	r = r.WithContext(withIdentity(r.Context(), Identity{
		Username: "test-user",
		Email:    "test-user@cluster.local",
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	team, err := store.GetTeamRow(context.Background(), "editable-team")
	if err != nil || team == nil {
		t.Fatalf("get team: %v", err)
	}
	if team.ContactEmail != "test-user@cluster.local" {
		t.Fatalf("expected OAuth contact email, got %q", team.ContactEmail)
	}
}

func TestTeamUpdateHandler_AdminPreservesAssignedContactEmail(t *testing.T) {
	store := database.NewMemStore()
	if err := store.CreateTeam(context.Background(), database.TeamRow{
		ID: "admin-team", Name: "Admin Team", ContactEmail: "old@redhat.com",
	}); err != nil {
		t.Fatalf("create team: %v", err)
	}
	handler := teamUpdateHandler(store)
	payload := database.TeamRow{Name: "Admin Team", ContactEmail: "owner@redhat.com", AuthGroup: "g"}
	r := httptest.NewRequest("PUT", "/api/v1/teams/admin-team", jsonBodyForTest(t, payload))
	r.SetPathValue("id", "admin-team")
	r = r.WithContext(withIdentity(r.Context(), Identity{
		Username: "admin",
		Email:    "admin@cluster.local",
		IsAdmin:  true,
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	team, err := store.GetTeamRow(context.Background(), "admin-team")
	if err != nil || team == nil {
		t.Fatalf("get team: %v", err)
	}
	if team.ContactEmail != "owner@redhat.com" {
		t.Fatalf("expected assigned contact email, got %q", team.ContactEmail)
	}
}

func TestAdminContactEmailValidation(t *testing.T) {
	store := database.NewMemStore()
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		body    string
	}{
		{
			name:    "team create",
			handler: teamCreateHandler(store),
			method:  http.MethodPost,
			path:    "/api/v1/teams",
			body:    `{"id":"invalid-team","name":"Invalid Team","contact_email":"not-an-email"}`,
		},
		{
			name:    "product create",
			handler: productCreateHandler(store),
			method:  http.MethodPost,
			path:    "/api/v1/products",
			body:    `{"id":"invalid-product","name":"Invalid Product","contact_email":"not-an-email"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			w := httptest.NewRecorder()
			tt.handler.ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestIsValidEmail(t *testing.T) {
	tests := map[string]bool{
		"user@redhat.com":        true,
		"user@cluster.local":     true,
		"user@example":           false,
		"user@redhat.com.":       false,
		" user@redhat.com":       false,
		"user@redhat.com ":       false,
		"user name@redhat.com":   false,
		"User <user@redhat.com>": false,
		"user@@redhat.com":       false,
		"`user@redhat.com":       false,
		"user`@redhat.com":       false,
		"not-an-email":           false,
	}
	for email, want := range tests {
		t.Run(email, func(t *testing.T) {
			if got := isValidEmail(email); got != want {
				t.Errorf("isValidEmail(%q) = %v, want %v", email, got, want)
			}
		})
	}
}

func TestOnboardHandler_MissingTeamID(t *testing.T) {
	handler := onboardHandler(database.NewMemStore(), nil)

	payload := onboardRequest{Team: database.TeamRow{Name: "My Team"}}
	r := httptest.NewRequest("POST", "/api/v1/onboard", jsonBodyForTest(t, payload))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOnboardHandler_MissingTeamName(t *testing.T) {
	handler := onboardHandler(database.NewMemStore(), nil)

	payload := onboardRequest{Team: database.TeamRow{ID: "my-team"}}
	r := httptest.NewRequest("POST", "/api/v1/onboard", jsonBodyForTest(t, payload))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOnboardHandler_InvalidJSON(t *testing.T) {
	handler := onboardHandler(database.NewMemStore(), nil)

	r := httptest.NewRequest("POST", "/api/v1/onboard", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestOnboardHandler_ConnectionMissingPlugin(t *testing.T) {
	handler := onboardHandler(database.NewMemStore(), nil)

	payload := onboardRequest{
		Team: database.TeamRow{ID: "t1", Name: "T1"},
		Connections: []devlake.ConnectionEntry{
			{Token: "tok", Endpoint: "https://api.github.com/", ExpiryDate: "2027-01-01"},
		},
	}
	r := httptest.NewRequest("POST", "/api/v1/onboard", jsonBodyForTest(t, payload))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing plugin, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOnboardHandler_ConnectionMissingExpiryDate(t *testing.T) {
	handler := onboardHandler(database.NewMemStore(), nil)

	payload := onboardRequest{
		Team: database.TeamRow{ID: "t1", Name: "T1"},
		Connections: []devlake.ConnectionEntry{
			{Plugin: "github", Token: "tok", Endpoint: "https://api.github.com/"},
		},
	}
	r := httptest.NewRequest("POST", "/api/v1/onboard", jsonBodyForTest(t, payload))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing expiry, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOnboardHandler_JiraMissingUsername(t *testing.T) {
	handler := onboardHandler(database.NewMemStore(), nil)

	payload := onboardRequest{
		Team: database.TeamRow{ID: "t1", Name: "T1"},
		Connections: []devlake.ConnectionEntry{
			{Plugin: "jira", Token: "tok", Endpoint: "https://jira.example.com/", ExpiryDate: "2027-01-01"},
		},
	}
	r := httptest.NewRequest("POST", "/api/v1/onboard", jsonBodyForTest(t, payload))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for jira missing username, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOnboardHandler_SourceReferencesUnprovidedPlugin(t *testing.T) {
	handler := onboardHandler(database.NewMemStore(), nil)

	payload := onboardRequest{
		Team: database.TeamRow{ID: "t1", Name: "T1"},
		Connections: []devlake.ConnectionEntry{
			{Plugin: "github", Token: "tok", Endpoint: "https://api.github.com/", ExpiryDate: "2027-01-01"},
		},
		Sources: []database.SourceRow{
			{ID: "repos", Type: "gitrepos", Repos: []database.RepoRow{
				{ID: "r1", Type: "gitlab", Owner: "org", Name: "repo"},
			}},
		},
	}
	r := httptest.NewRequest("POST", "/api/v1/onboard", jsonBodyForTest(t, payload))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for gitlab source without gitlab connection, got %d: %s", w.Code, w.Body.String())
	}
}

func TestValidateOnboardRequest_CodecovOnlyRepository(t *testing.T) {
	req := onboardRequest{
		Team: database.TeamRow{ID: "coverage-only", Name: "Coverage Only", AuthGroup: "g"},
		Connections: []devlake.ConnectionEntry{{
			Plugin: "codecov", Token: "tok", Endpoint: "https://api.codecov.io",
			Organization: "konflux-ci", Service: "github", ExpiryDate: "never",
		}},
		Sources: []database.SourceRow{{
			ID: "gitrepos", Type: "gitrepos", Repos: []database.RepoRow{{
				ID: "konflux-ui", Type: "github", Owner: "konflux-ci", Name: "konflux-ui",
			}},
		}},
	}
	if msg := validateOnboardRequest(req); msg != "" {
		t.Fatalf("Codecov-only repository was rejected: %s", msg)
	}
	req.Sources[0].Repos[0].Owner = "other-org"
	if msg := validateOnboardRequest(req); msg == "" {
		t.Fatal("unmatched GitHub repository was accepted without a GitHub connection")
	}
}

func TestOnboardHandler_WorkItemsWithoutJiraConnection(t *testing.T) {
	handler := onboardHandler(database.NewMemStore(), nil)

	payload := onboardRequest{
		Team: database.TeamRow{ID: "t1", Name: "T1"},
		Sources: []database.SourceRow{
			{ID: "work", Type: "workitems", Projects: []database.WorkitemProjRow{
				{ID: "p1", ProjectName: "Proj", Type: "jira", JiraProject: "PROJ"},
			}},
		},
	}
	r := httptest.NewRequest("POST", "/api/v1/onboard", jsonBodyForTest(t, payload))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for jira workitems without jira connection, got %d: %s", w.Code, w.Body.String())
	}
}

func jiraBoardMetadataServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var response any
		switch r.URL.Path {
		case "/rest/agile/1.0/board/6499":
			response = map[string]any{"id": 6499, "location": map[string]string{"projectKey": "SANDBOX"}}
		case "/rest/api/2/project/SANDBOX":
			response = map[string]any{"key": "SANDBOX", "name": "Sandbox", "issueTypes": []map[string]string{{"name": "Task"}}}
		case "/rest/api/2/field":
			response = []any{}
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
}

func TestValidateAndAppendItems_ResolvesJiraProjectFromBoardID(t *testing.T) {
	ctx := context.Background()
	store := database.NewMemStore()
	jiraServer := jiraBoardMetadataServer(t)
	t.Cleanup(jiraServer.Close)
	for key, value := range map[string]string{
		"devlake_jira_endpoint": jiraServer.URL,
		"devlake_jira_token":    "local-only",
		"devlake_jira_username": "local@example.test",
	} {
		if err := store.SetEntityConfigKey(ctx, "team", "team-a", key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}
	service := devlake.NewService(devlake.Config{APIURL: "http://devlake.invalid", AllowPrivateEndpoints: true}, jiraServer.Client(), store)

	var req onboardSourceRequest
	if err := json.Unmarshal([]byte(`{"team_id":"team-a","source_id":"workitems","items":[{"type":"jira","board_id":"6499"}]}`), &req); err != nil {
		t.Fatal(err)
	}
	source := &database.SourceRow{ID: "workitems", Type: "workitems"}
	updated := &database.SourceRow{ID: "workitems", Type: "workitems"}
	warnings, validationErr := validateAndAppendItems(ctx, req, source, updated, service)
	if validationErr != "" {
		t.Fatalf("validateAndAppendItems: %s", validationErr)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(updated.Projects) != 1 {
		t.Fatalf("projects = %+v, want one Jira project", updated.Projects)
	}
	project := updated.Projects[0]
	if project.JiraProject != "SANDBOX" || project.BoardID != "6499" {
		t.Fatalf("saved Jira project = %+v, want key SANDBOX and board 6499", project)
	}
}

func TestValidateImportJiraProjects_ResolvesJiraProjectFromBoardID(t *testing.T) {
	ctx := context.Background()
	store := database.NewMemStore()
	jiraServer := jiraBoardMetadataServer(t)
	t.Cleanup(jiraServer.Close)
	service := devlake.NewService(devlake.Config{APIURL: "http://devlake.invalid", AllowPrivateEndpoints: true}, jiraServer.Client(), store)
	team := &database.Team{Sources: []database.Source{{
		ID:   "workitems",
		Type: "workitems",
		Projects: []database.WorkitemProject{{
			Type: "jira", BoardID: "6499",
		}},
	}}}
	connections := []devlake.ConnectionEntry{{
		Plugin: "jira", Endpoint: jiraServer.URL, Token: "local-only", Username: "local@example.test",
	}}

	if err := validateImportJiraProjects(ctx, service, team, connections); err != nil {
		t.Fatalf("validateImportJiraProjects: %v", err)
	}
	project := team.Sources[0].Projects[0]
	if project.JiraProject != "SANDBOX" || project.BoardID != "6499" {
		t.Fatalf("imported Jira project = %+v, want key SANDBOX and board 6499", project)
	}
}

// ---- Offboard handler tests ----

func seedTeamForOffboard(t *testing.T, store database.ConfigStore) {
	t.Helper()
	ctx := context.Background()
	if err := store.CreateTeam(ctx, database.TeamRow{ID: "del-team", Name: "Delete Me", ContactEmail: "alice@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSource(ctx, "del-team", database.SourceRow{ID: "s1", Type: "gitrepos"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateDashboard(ctx, database.DashboardRow{OwnerType: "team", OwnerID: "del-team", DashboardID: "d1", Type: "grafana", Title: "Dash"}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceEntityConfig(ctx, "team", "del-team", map[string]string{"k": "v"}); err != nil {
		t.Fatal(err)
	}
}

func TestOffboardHandler_Success(t *testing.T) {
	store := database.NewMemStore()
	seedTeamForOffboard(t, store)
	handler := offboardHandler(store, nil)

	payload := offboardRequest{TeamID: "del-team"}
	r := httptest.NewRequest("POST", "/api/v1/offboard", jsonBodyForTest(t, payload))
	r = r.WithContext(withIdentity(r.Context(), Identity{Username: "alice", Email: "alice@example.com", IsAdmin: true}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	ctx := context.Background()
	team, _ := store.GetTeamRow(ctx, "del-team")
	if team != nil {
		t.Error("team should have been deleted")
	}
	sources, _ := store.ListSourceRows(ctx, "del-team")
	if len(sources) != 0 {
		t.Errorf("expected 0 sources, got %d", len(sources))
	}
	dashboards, _ := store.ListDashboardRows(ctx, "team", "del-team")
	if len(dashboards) != 0 {
		t.Errorf("expected 0 dashboards, got %d", len(dashboards))
	}
	cfg, _ := store.GetEntityConfigKV(ctx, "team", "del-team")
	if len(cfg) != 0 {
		t.Errorf("expected empty config, got %+v", cfg)
	}
}

func TestOffboardHandler_AdminCanDeleteAnyTeam(t *testing.T) {
	store := database.NewMemStore()
	seedTeamForOffboard(t, store)
	handler := offboardHandler(store, nil)

	payload := offboardRequest{TeamID: "del-team"}
	r := httptest.NewRequest("POST", "/api/v1/offboard", jsonBodyForTest(t, payload))
	r = r.WithContext(withIdentity(r.Context(), Identity{Username: "admin", Email: "admin@example.com", IsAdmin: true}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOffboardHandler_NonManagerForbidden(t *testing.T) {
	store := database.NewMemStore()
	seedTeamForOffboard(t, store)
	handler := offboardHandler(store, nil)

	payload := offboardRequest{TeamID: "del-team"}
	r := httptest.NewRequest("POST", "/api/v1/offboard", jsonBodyForTest(t, payload))
	r = r.WithContext(withIdentity(r.Context(), Identity{Username: "bob", Email: "bob@example.com"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	team, _ := store.GetTeamRow(context.Background(), "del-team")
	if team == nil {
		t.Error("team should NOT have been deleted")
	}
}

func TestOffboardHandler_MissingTeamID(t *testing.T) {
	handler := offboardHandler(database.NewMemStore(), nil)

	payload := offboardRequest{}
	r := httptest.NewRequest("POST", "/api/v1/offboard", jsonBodyForTest(t, payload))
	r = r.WithContext(withIdentity(r.Context(), Identity{Username: "alice", Email: "alice@example.com"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOffboardHandler_TeamNotFound(t *testing.T) {
	handler := offboardHandler(database.NewMemStore(), nil)

	payload := offboardRequest{TeamID: "nonexistent"}
	r := httptest.NewRequest("POST", "/api/v1/offboard", jsonBodyForTest(t, payload))
	r = r.WithContext(withIdentity(r.Context(), Identity{Username: "alice", Email: "alice@example.com"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// ---- Offboard-source handler tests ----

func seedTeamForSourceOffboard(t *testing.T, store database.ConfigStore) {
	t.Helper()
	ctx := context.Background()
	if err := store.CreateTeam(ctx, database.TeamRow{ID: "src-team", Name: "Source Team", ContactEmail: "alice@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSource(ctx, "src-team", database.SourceRow{
		ID:   "s1",
		Type: "gitrepos",
		Repos: []database.RepoRow{
			{ID: "repo-a", Type: "github", Owner: "org", Name: "repo-a"},
			{ID: "repo-b", Type: "github", Owner: "org", Name: "repo-b"},
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestOffboardSourceHandler_RemoveItem(t *testing.T) {
	store := database.NewMemStore()
	seedTeamForSourceOffboard(t, store)
	handler := offboardSourceHandler(store, nil)

	payload := offboardSourceRequest{TeamID: "src-team", SourceID: "s1", ItemID: "repo-a"}
	r := httptest.NewRequest("POST", "/api/v1/offboard-source", jsonBodyForTest(t, payload))
	r = r.WithContext(withIdentity(r.Context(), Identity{Username: "alice", Email: "alice@example.com"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	ctx := context.Background()
	src, _ := store.GetSourceRow(ctx, "src-team", "s1")
	if src == nil {
		t.Fatal("source should still exist (one repo remaining)")
	}
	if len(src.Repos) != 1 || src.Repos[0].ID != "repo-b" {
		t.Errorf("expected only repo-b remaining, got %+v", src.Repos)
	}
}

func TestOffboardSourceHandler_DeletesEmptySource(t *testing.T) {
	store := database.NewMemStore()
	ctx := context.Background()
	if err := store.CreateTeam(ctx, database.TeamRow{ID: "src-team", Name: "Source Team", ContactEmail: "alice@example.com"}); err != nil {
		t.Errorf("create team: %v", err)
	}
	if err := store.CreateSource(ctx, "src-team", database.SourceRow{
		ID:   "s1",
		Type: "gitrepos",
		Repos: []database.RepoRow{
			{ID: "repo-only", Type: "github", Owner: "org", Name: "repo-only"},
		},
	}); err != nil {
		t.Errorf("create source: %v", err)
	}
	handler := offboardSourceHandler(store, nil)

	payload := offboardSourceRequest{TeamID: "src-team", SourceID: "s1", ItemID: "repo-only"}
	r := httptest.NewRequest("POST", "/api/v1/offboard-source", jsonBodyForTest(t, payload))
	r = r.WithContext(withIdentity(r.Context(), Identity{Username: "alice", Email: "alice@example.com"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	src, _ := store.GetSourceRow(ctx, "src-team", "s1")
	if src != nil {
		t.Error("source should have been deleted when last item was removed")
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("unmarshal response: %v", err)
	}
	if resp["source_deleted"] != true {
		t.Error("expected source_deleted=true in response")
	}
}

func TestOffboardSourceHandler_AdminCanRemove(t *testing.T) {
	store := database.NewMemStore()
	seedTeamForSourceOffboard(t, store)
	handler := offboardSourceHandler(store, nil)

	payload := offboardSourceRequest{TeamID: "src-team", SourceID: "s1", ItemID: "repo-a"}
	r := httptest.NewRequest("POST", "/api/v1/offboard-source", jsonBodyForTest(t, payload))
	r = r.WithContext(withIdentity(r.Context(), Identity{Username: "admin", Email: "admin@example.com", IsAdmin: true}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOffboardSourceHandler_NonManagerForbidden(t *testing.T) {
	store := database.NewMemStore()
	seedTeamForSourceOffboard(t, store)
	handler := offboardSourceHandler(store, nil)

	payload := offboardSourceRequest{TeamID: "src-team", SourceID: "s1", ItemID: "repo-a"}
	r := httptest.NewRequest("POST", "/api/v1/offboard-source", jsonBodyForTest(t, payload))
	r = r.WithContext(withIdentity(r.Context(), Identity{Username: "bob", Email: "bob@example.com"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	ctx := context.Background()
	src, _ := store.GetSourceRow(ctx, "src-team", "s1")
	if src == nil || len(src.Repos) != 2 {
		t.Error("source should be unchanged after forbidden request")
	}
}

func TestOffboardSourceHandler_ItemNotFound(t *testing.T) {
	store := database.NewMemStore()
	seedTeamForSourceOffboard(t, store)
	handler := offboardSourceHandler(store, nil)

	payload := offboardSourceRequest{TeamID: "src-team", SourceID: "s1", ItemID: "nonexistent"}
	r := httptest.NewRequest("POST", "/api/v1/offboard-source", jsonBodyForTest(t, payload))
	r = r.WithContext(withIdentity(r.Context(), Identity{Username: "alice", Email: "alice@example.com"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImportPreview_OverridesContactEmail(t *testing.T) {
	store := database.NewMemStore()
	handler := importTeamPreviewHandler(store)

	team := database.Team{
		ID:           "import-test",
		Name:         "Import Test",
		ContactEmail: "json-email@redhat.com",
	}
	r := httptest.NewRequest("POST", "/api/v1/admin/import-team/preview", jsonBodyForTest(t, team))
	r = r.WithContext(withIdentity(r.Context(), Identity{
		Username: "importer",
		Email:    "importer@cluster.local",
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp importPreviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Team.ContactEmail != "importer@cluster.local" {
		t.Errorf("expected contact email to be overridden to importer identity, got %q", resp.Team.ContactEmail)
	}
}

func TestTeamHandlers_RequireAuthGroup(t *testing.T) {
	store := database.NewMemStore()
	if err := store.CreateTeam(context.Background(), database.TeamRow{
		ID: "grouped-team", Name: "Grouped Team", AuthGroup: "existing-group",
	}); err != nil {
		t.Fatalf("create team: %v", err)
	}
	admin := Identity{Username: "admin", Email: "admin@cluster.local", IsAdmin: true}

	for _, group := range []string{"", "   "} {
		payload := database.TeamRow{Name: "Grouped Team", AuthGroup: group}
		r := httptest.NewRequest("PUT", "/api/v1/teams/grouped-team", jsonBodyForTest(t, payload))
		r.SetPathValue("id", "grouped-team")
		r = r.WithContext(withIdentity(r.Context(), admin))
		w := httptest.NewRecorder()
		teamUpdateHandler(store).ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("update with auth_group %q: expected 400, got %d: %s", group, w.Code, w.Body.String())
		}

		create := database.TeamRow{ID: "new-team", Name: "New Team", AuthGroup: group}
		r = httptest.NewRequest("POST", "/api/v1/teams", jsonBodyForTest(t, create))
		w = httptest.NewRecorder()
		teamCreateHandler(store).ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("create with auth_group %q: expected 400, got %d: %s", group, w.Code, w.Body.String())
		}

		if msg := validateOnboardRequest(onboardRequest{Team: create}); msg != errAuthGroupRequired {
			t.Fatalf("onboard with auth_group %q: expected %q, got %q", group, errAuthGroupRequired, msg)
		}
	}

	team, err := store.GetTeamRow(context.Background(), "grouped-team")
	if err != nil || team == nil {
		t.Fatalf("get team: %v", err)
	}
	if team.AuthGroup != "existing-group" {
		t.Fatalf("auth_group was cleared: got %q", team.AuthGroup)
	}
}
