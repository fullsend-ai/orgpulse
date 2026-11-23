package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/fullsend-ai/orgpulse/internal/authz"
	"github.com/fullsend-ai/orgpulse/internal/database"
)

type assembledStore struct {
	*database.MemStore
}

func (s assembledStore) Close() error { return nil }

func (s assembledStore) GetOrgs(ctx context.Context) ([]database.Org, error) {
	rows, err := s.ListOrgRows(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]database.Org, 0, len(rows))
	for _, row := range rows {
		links, err := s.GetOrgProductLinks(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		org := database.Org{ID: row.ID, Name: row.Name, Products: []string{}}
		for _, link := range links {
			org.Products = append(org.Products, link.ID)
		}
		out = append(out, org)
	}
	return out, nil
}

func (s assembledStore) GetProducts(ctx context.Context) ([]database.Product, error) {
	rows, err := s.ListProductRows(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]database.Product, 0, len(rows))
	for _, row := range rows {
		links, err := s.GetProductTeamLinks(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		product := database.Product{ID: row.ID, Name: row.Name, Teams: []string{}}
		for _, link := range links {
			product.Teams = append(product.Teams, link.ID)
		}
		out = append(out, product)
	}
	return out, nil
}

func (s assembledStore) GetTeams(ctx context.Context) ([]database.Team, error) {
	rows, err := s.ListTeamRows(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]database.Team, 0, len(rows))
	for _, row := range rows {
		out = append(out, database.Team{ID: row.ID, Name: row.Name})
	}
	return out, nil
}

func TestTenantGrantReads(t *testing.T) {
	handler, _ := newTenantServer(t)

	status, body := doAs(handler, "", "GET", "/api/orgs", "")
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous orgs: status=%d body=%s", status, body)
	}

	status, body = doAs(handler, "cara", "GET", "/api/teams", "")
	if status != http.StatusOK || !strings.Contains(body, `"t1"`) || strings.Contains(body, `"t2"`) {
		t.Fatalf("cara teams: status=%d body=%s", status, body)
	}
	status, body = doAs(handler, "cara", "GET", "/api/orgs", "")
	if status != http.StatusOK || !strings.Contains(body, `"rhai"`) || strings.Contains(body, `"central"`) || strings.Contains(body, `"p2"`) {
		t.Fatalf("cara orgs: status=%d body=%s", status, body)
	}
	status, body = doAs(handler, "cara", "GET", "/api/products", "")
	if status != http.StatusOK || !strings.Contains(body, `"t1"`) || strings.Contains(body, `"t2"`) {
		t.Fatalf("cara products: status=%d body=%s", status, body)
	}

	status, _ = doAs(handler, "cara", "GET", "/api/v1/teams/t2", "")
	if status != http.StatusNotFound {
		t.Fatalf("cara foreign team: status=%d", status)
	}
	status, _ = doAs(handler, "cara", "GET", "/api/v1/products/p1/overrides", "")
	if status != http.StatusNotFound {
		t.Fatalf("cara product aggregate: status=%d", status)
	}
	status, body = doAs(handler, "bob", "GET", "/api/v1/products/p1/overrides", "")
	if status != http.StatusOK {
		t.Fatalf("bob overrides: status=%d body=%s", status, body)
	}
	status, _ = doAs(handler, "zed", "GET", "/api/v1/teams/t1", "")
	if status != http.StatusNotFound {
		t.Fatalf("unknown user team: status=%d", status)
	}

	status, body = doAs(handler, "op", "GET", "/api/v1/orgs", "")
	if status != http.StatusOK || !strings.Contains(body, `"rhai"`) || !strings.Contains(body, `"central"`) {
		t.Fatalf("operator orgs: status=%d body=%s", status, body)
	}
	status, body = doAs(handler, "op", "GET", "/api/v1/auth/me", "")
	if status != http.StatusOK || !strings.Contains(body, `"operator":true`) || strings.Contains(body, `"is_admin":true`) {
		t.Fatalf("operator me: status=%d body=%s", status, body)
	}
}

func TestTenantGrantWorkflow(t *testing.T) {
	handler, calls := newTenantServer(t)

	status, body := doAs(handler, "cara", "POST", "/workflows/production/cycle", `{"owner":"acme","name":"other"}`)
	if status != http.StatusForbidden || *calls != 0 {
		t.Fatalf("foreign repo: status=%d calls=%d body=%s", status, *calls, body)
	}
	status, body = doAs(handler, "cara", "POST", "/workflows/production/cycle", `{}`)
	if status != http.StatusForbidden || *calls != 0 {
		t.Fatalf("empty metric: status=%d calls=%d body=%s", status, *calls, body)
	}
	status, body = doAs(handler, "cara", "POST", "/workflows/production/cycle", `{"owner":"acme","name":"widget"}`)
	if status != http.StatusOK || *calls != 1 {
		t.Fatalf("granted repo: status=%d calls=%d body=%s", status, *calls, body)
	}
	status, body = doAs(handler, "root", "POST", "/workflows/production/cycle", `{}`)
	if status != http.StatusOK || *calls != 2 {
		t.Fatalf("admin metric: status=%d calls=%d body=%s", status, *calls, body)
	}
	status, body = doAs(handler, "op", "POST", "/workflows/production/cycle", `{}`)
	if status != http.StatusOK || *calls != 3 {
		t.Fatalf("operator metric: status=%d calls=%d body=%s", status, *calls, body)
	}
}

func TestTenantGrantWrites(t *testing.T) {
	handler, _ := newTenantServer(t)

	status, body := doAs(handler, "cara", "POST", "/api/v1/onboard", `{"team":{"id":"new-team","name":"New","auth_group":"new-group"},"product_id":"p1"}`)
	if status != http.StatusForbidden {
		t.Fatalf("cara onboard: status=%d body=%s", status, body)
	}
	status, body = doAs(handler, "bob", "POST", "/api/v1/onboard", `{"team":{"id":"new-team","name":"New","auth_group":"new-group"},"product_id":"p1"}`)
	if status != http.StatusCreated {
		t.Fatalf("bob onboard: status=%d body=%s", status, body)
	}

	status, body = doAs(handler, "cara", "PUT", "/api/v1/teams/t2", `{"name":"Nope","auth_group":"t2-group"}`)
	if status != http.StatusForbidden {
		t.Fatalf("cara update t2: status=%d body=%s", status, body)
	}
	status, body = doAs(handler, "cara", "PUT", "/api/v1/teams/t1", `{"name":"Team One","auth_group":"t1-renamed"}`)
	if status != http.StatusOK || !strings.Contains(body, `"t1-renamed"`) {
		t.Fatalf("cara update t1: status=%d body=%s", status, body)
	}
	status, body = doAs(handler, "bob", "PUT", "/api/v1/products/p1", `{"name":"Product One","auth_group":"other"}`)
	if status != http.StatusForbidden {
		t.Fatalf("bob change product group: status=%d body=%s", status, body)
	}
	status, body = doAs(handler, "alice", "PUT", "/api/v1/products/p1", `{"name":"Product One","auth_group":"p1-new"}`)
	if status != http.StatusOK {
		t.Fatalf("alice change product group: status=%d body=%s", status, body)
	}
	status, body = doAs(handler, "cara", "DELETE", "/api/v1/teams/t1", "")
	if status != http.StatusForbidden {
		t.Fatalf("cara delete: status=%d body=%s", status, body)
	}
}

func newTenantServer(t *testing.T) (http.Handler, *int) {
	t.Helper()
	ctx := context.Background()
	store := assembledStore{MemStore: database.NewMemStore()}
	must(t, store.CreateOrg(ctx, database.OrgRow{ID: "rhai", Name: "RHAI", AuthGroup: "rhai-org"}))
	must(t, store.CreateOrg(ctx, database.OrgRow{ID: "central", Name: "Central", AuthGroup: "central-org"}))
	must(t, store.CreateProduct(ctx, database.ProductRow{ID: "p1", Name: "Product One", AuthGroup: "p1-group"}))
	must(t, store.CreateProduct(ctx, database.ProductRow{ID: "p2", Name: "Product Two", AuthGroup: "p2-group"}))
	must(t, store.CreateTeam(ctx, database.TeamRow{ID: "t1", Name: "Team One", AuthGroup: "t1-group"}))
	must(t, store.CreateTeam(ctx, database.TeamRow{ID: "t2", Name: "Team Two", AuthGroup: "t2-group"}))
	must(t, store.ReplaceOrgProductLinks(ctx, "rhai", []database.RelEntry{{ID: "p1"}}))
	must(t, store.ReplaceOrgProductLinks(ctx, "central", []database.RelEntry{{ID: "p2"}}))
	must(t, store.ReplaceProductTeamLinks(ctx, "p1", []database.RelEntry{{ID: "t1"}, {ID: "t2"}}))
	must(t, store.CreateSource(ctx, "t1", database.SourceRow{
		ID: "git", Type: "gitrepos",
		Repos: []database.RepoRow{{ID: "widget", Owner: "acme", Name: "widget"}},
	}))
	must(t, store.CreateSource(ctx, "t2", database.SourceRow{
		ID: "git", Type: "gitrepos",
		Repos: []database.RepoRow{{ID: "other", Owner: "acme", Name: "other"}},
	}))

	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(upstream.Close)

	srv := New(0, false, fstest.MapFS{}, store, store, nil, FrontendConfig{
		AdminUsers:     []string{"root"},
		OperatorGroups: []string{"ops-group"},
		Membership: authz.MapDirectory{
			"cara":  {"t1-group"},
			"bob":   {"p1-group"},
			"alice": {"rhai-org"},
			"op":    {"ops-group"},
		},
	}, nil, AIAssistConfig{}, nil, nil, upstream.URL)
	return srv.Handler(), &calls
}

func doAs(handler http.Handler, user, method, path, body string) (int, string) {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if user != "" {
		req.Header.Set("X-Forwarded-User", user)
		req.Header.Set("X-Forwarded-Email", user+"@example.com")
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
