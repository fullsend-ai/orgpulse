package devlake

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fullsend-ai/orgpulse/internal/database"
)

func TestTestRegistryScopeName(t *testing.T) {
	tests := []struct {
		ciTool, org, fullName, want string
	}{
		{CIToolTektonCI, "konflux-test-storage", "konflux-test-storage/konflux-team/release-service", "konflux-team/release-service"},
		{CIToolTektonCI, "konflux-test-storage", "other-org/repo", "other-org/repo"},
		{CIToolOpenshiftCI, "redhat-appstudio", "e2e-tests", "e2e-tests"},
		{CIToolTektonCI, "", "a/b", "a/b"},
	}
	for _, tt := range tests {
		if got := TestRegistryScopeName(tt.ciTool, tt.org, tt.fullName); got != tt.want {
			t.Errorf("TestRegistryScopeName(%q, %q, %q) = %q, want %q", tt.ciTool, tt.org, tt.fullName, got, tt.want)
		}
	}
}

func TestCreateTeamConnection_TestRegistryTekton(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	connID, err := svc.CreateTeamConnection(ctx, "alpha", ConnectionEntry{
		Plugin:       PluginTestRegistry,
		CITool:       CIToolTektonCI,
		Organization: " konflux-test-storage ",
		ExpiryDate:   "never",
	})
	if err != nil {
		t.Fatalf("CreateTeamConnection: %v", err)
	}

	fake.mu.Lock()
	body := fake.connectionBodies[PluginTestRegistry][connID]
	fake.mu.Unlock()
	if body["ciTool"] != CIToolTektonCI {
		t.Errorf("ciTool = %v, want %q", body["ciTool"], CIToolTektonCI)
	}
	if body["quayOrganization"] != "konflux-test-storage" {
		t.Errorf("quayOrganization = %v", body["quayOrganization"])
	}
	if body["project"] != testRegistryPlaceholderProject {
		t.Errorf("project = %v, want placeholder %q", body["project"], testRegistryPlaceholderProject)
	}
	if _, ok := body["endpoint"]; ok {
		t.Errorf("testregistry connection must not send an endpoint: %v", body)
	}
	if _, ok := body["githubToken"]; ok {
		t.Errorf("Tekton CI connection must not send a GitHub token: %v", body)
	}

	kv, _ := store.GetEntityConfigKV(ctx, "team", "alpha")
	if kv["devlake_testregistry_connection_id"] != strconv.Itoa(connID) {
		t.Errorf("connection_id = %q", kv["devlake_testregistry_connection_id"])
	}
	if kv["devlake_testregistry_ci_tool"] != CIToolTektonCI {
		t.Errorf("ci_tool = %q", kv["devlake_testregistry_ci_tool"])
	}
	if kv["devlake_testregistry_organization"] != "konflux-test-storage" {
		t.Errorf("organization = %q", kv["devlake_testregistry_organization"])
	}
	if _, ok := kv["devlake_testregistry_token"]; ok {
		t.Error("Tekton CI connection must not store a token")
	}
}

func TestCreateTeamConnection_TestRegistryOpenshift(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	connID, err := svc.CreateTeamConnection(ctx, "alpha", ConnectionEntry{
		Plugin:       PluginTestRegistry,
		CITool:       CIToolOpenshiftCI,
		Organization: "redhat-appstudio",
		Token:        "ghp_secret",
		ExpiryDate:   "2027-01-01",
	})
	if err != nil {
		t.Fatalf("CreateTeamConnection: %v", err)
	}
	fake.mu.Lock()
	body := fake.connectionBodies[PluginTestRegistry][connID]
	fake.mu.Unlock()
	if body["githubOrganization"] != "redhat-appstudio" || body["githubToken"] != "ghp_secret" {
		t.Errorf("unexpected Openshift CI body: %v", body)
	}
	if _, ok := body["token"]; ok {
		t.Errorf("testregistry uses githubToken, not token: %v", body)
	}
	kv, _ := store.GetEntityConfigKV(ctx, "team", "alpha")
	if kv["devlake_testregistry_token"] != "ghp_secret" || kv["devlake_testregistry_token_expiry"] != "2027-01-01" {
		t.Errorf("token config not stored: %v", kv)
	}
}

func TestCreateTeamConnection_TestRegistryValidation(t *testing.T) {
	svc, _, _ := setupTestService(t)
	ctx := context.Background()
	cases := []ConnectionEntry{
		{Plugin: PluginTestRegistry, CITool: "Jenkins", Organization: "x"},
		{Plugin: PluginTestRegistry, CITool: CIToolTektonCI},
		{Plugin: PluginTestRegistry, CITool: CIToolOpenshiftCI, Organization: "org"},
	}
	for _, entry := range cases {
		if _, err := svc.CreateTeamConnection(ctx, "alpha", entry); !errors.Is(err, ErrInvalidConnection) {
			t.Errorf("CreateTeamConnection(%+v) err = %v, want ErrInvalidConnection", entry, err)
		}
	}
}

func TestTestRegistryCIToolIsImmutable(t *testing.T) {
	svc, fake, _ := setupTestService(t)
	ctx := context.Background()

	connID, err := svc.CreateTeamConnection(ctx, "alpha", ConnectionEntry{
		Plugin: PluginTestRegistry, CITool: CIToolOpenshiftCI, Organization: "org", Token: "t1", ExpiryDate: "never",
	})
	if err != nil {
		t.Fatalf("CreateTeamConnection: %v", err)
	}

	_, err = svc.CreateTeamConnection(ctx, "alpha", ConnectionEntry{
		Plugin: PluginTestRegistry, CITool: CIToolTektonCI, Organization: "org", ExpiryDate: "never",
	})
	if !errors.Is(err, ErrInvalidConnection) {
		t.Errorf("re-create with another CI tool: err = %v, want ErrInvalidConnection", err)
	}

	err = svc.UpdateTeamConnection(ctx, "alpha", ConnectionEntry{
		Plugin: PluginTestRegistry, CITool: CIToolTektonCI, Token: "t2", ExpiryDate: "never",
	})
	if !errors.Is(err, ErrInvalidConnection) {
		t.Errorf("update with another CI tool: err = %v, want ErrInvalidConnection", err)
	}

	err = svc.UpdateTeamConnection(ctx, "alpha", ConnectionEntry{
		Plugin: PluginTestRegistry, Organization: "other-org", Token: "t2", ExpiryDate: "never",
	})
	if !errors.Is(err, ErrInvalidConnection) {
		t.Errorf("update with another organization: err = %v, want ErrInvalidConnection", err)
	}

	if err := svc.UpdateTeamConnection(ctx, "alpha", ConnectionEntry{
		Plugin: PluginTestRegistry, Token: "t2", ExpiryDate: "2027-01-01",
	}); err != nil {
		t.Fatalf("token update: %v", err)
	}
	fake.mu.Lock()
	body := fake.connectionBodies[PluginTestRegistry][connID]
	fake.mu.Unlock()
	if body["githubToken"] != "t2" || body["ciTool"] != CIToolOpenshiftCI {
		t.Errorf("token update body = %v", body)
	}
	if _, ok := body["endpoint"]; ok {
		t.Errorf("update must not send an endpoint: %v", body)
	}
}

func TestUpdateTeamConnection_TestRegistryTektonHasNoToken(t *testing.T) {
	svc, _, _ := setupTestService(t)
	ctx := context.Background()
	if _, err := svc.CreateTeamConnection(ctx, "alpha", ConnectionEntry{
		Plugin: PluginTestRegistry, CITool: CIToolTektonCI, Organization: "q", ExpiryDate: "never",
	}); err != nil {
		t.Fatalf("CreateTeamConnection: %v", err)
	}
	err := svc.UpdateTeamConnection(ctx, "alpha", ConnectionEntry{Plugin: PluginTestRegistry, Token: "x", ExpiryDate: "never"})
	if !errors.Is(err, ErrInvalidConnection) {
		t.Errorf("err = %v, want ErrInvalidConnection", err)
	}
}

func useTestRegistryAPIs(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	origGH, origQuay := testRegistryGitHubAPI, testRegistryQuayAPI
	testRegistryGitHubAPI, testRegistryQuayAPI = srv.URL, srv.URL
	t.Cleanup(func() { testRegistryGitHubAPI, testRegistryQuayAPI = origGH, origQuay })
}

func TestTestConnection_TestRegistry(t *testing.T) {
	useTestRegistryAPIs(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/user":
			if r.Header.Get("Authorization") != "Bearer good" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("github-authentication-token-expiration", "2027-02-03 10:00:00 UTC")
		case r.URL.Path == "/orgs/my-org":
		case r.URL.Path == "/orgs/private-org":
			w.WriteHeader(http.StatusForbidden)
		case r.URL.Path == "/api/v1/repository" && r.URL.Query().Get("namespace") == "store":
			_, _ = w.Write([]byte(`{"repositories":[]}`))
		default:
			http.NotFound(w, r)
		}
	})
	svc, _, _ := setupTestService(t)
	ctx := context.Background()

	res, err := svc.TestConnection(ctx, ConnectionEntry{
		Plugin: PluginTestRegistry, CITool: CIToolOpenshiftCI, Organization: "my-org", Token: "good",
	})
	if err != nil {
		t.Fatalf("Openshift CI test: %v", err)
	}
	if res.ExpiryDate != "2027-02-03" {
		t.Errorf("expiry = %q, want 2027-02-03", res.ExpiryDate)
	}

	failures := []ConnectionEntry{
		{Plugin: PluginTestRegistry, CITool: CIToolOpenshiftCI, Organization: "my-org", Token: "bad"},
		{Plugin: PluginTestRegistry, CITool: CIToolOpenshiftCI, Organization: "missing-org", Token: "good"},
		{Plugin: PluginTestRegistry, CITool: CIToolOpenshiftCI, Organization: "private-org", Token: "good"},
		{Plugin: PluginTestRegistry, CITool: CIToolTektonCI, Organization: "unknown"},
	}
	for _, entry := range failures {
		if _, err := svc.TestConnection(ctx, entry); err == nil {
			t.Errorf("TestConnection(%+v): expected failure", entry)
		}
	}

	if _, err := svc.TestConnection(ctx, ConnectionEntry{
		Plugin: PluginTestRegistry, CITool: CIToolTektonCI, Organization: "store",
	}); err != nil {
		t.Errorf("Tekton CI test: %v", err)
	}
	if _, err := svc.TestConnection(ctx, ConnectionEntry{
		Plugin: PluginTestRegistry, CITool: CIToolTektonCI,
	}); !errors.Is(err, ErrInvalidConnection) {
		t.Errorf("missing org: err = %v, want ErrInvalidConnection", err)
	}
}

func testSuitesSource(names ...string) database.SourceRow {
	src := database.SourceRow{ID: SourceTypeTestSuites, Type: SourceTypeTestSuites}
	for i, n := range names {
		src.Projects = append(src.Projects, database.WorkitemProjRow{
			ID: "testregistry-" + strconv.Itoa(i), Type: PluginTestRegistry, ProjectName: n,
		})
	}
	return src
}

func TestSyncTeam_TestRegistryScopes(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	connID, err := svc.CreateTeamConnection(ctx, "alpha", ConnectionEntry{
		Plugin: PluginTestRegistry, CITool: CIToolTektonCI, Organization: "konflux-test-storage", ExpiryDate: "never",
	})
	if err != nil {
		t.Fatalf("CreateTeamConnection: %v", err)
	}
	addTeamSources(store, "alpha", []database.SourceRow{
		testSuitesSource("konflux-test-storage/konflux-team/release-service", "konflux-test-storage/build"),
	})

	result, err := svc.SyncTeam(ctx, "alpha")
	if err != nil {
		t.Fatalf("SyncTeam: %v", err)
	}
	if len(result.Errors) > 0 {
		t.Fatalf("sync errors: %v", result.Errors)
	}
	if len(result.Resolved) != 2 {
		t.Errorf("resolved = %v, want 2 scopes", result.Resolved)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	scopes := fake.scopes[PluginTestRegistry][connID]
	if len(scopes) != 2 {
		t.Fatalf("registered %d scopes, want 2", len(scopes))
	}
	names := map[string]string{}
	for _, raw := range scopes {
		var data struct {
			FullName string `json:"fullName"`
			Name     string `json:"name"`
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatal(err)
		}
		names[data.FullName] = data.Name
	}
	if names["konflux-test-storage/konflux-team/release-service"] != "konflux-team/release-service" {
		t.Errorf("scope names = %v", names)
	}

	if len(fake.blueprints) != 1 {
		t.Fatalf("blueprints = %d, want 1", len(fake.blueprints))
	}
	bp := fake.blueprints[0]
	if bp.Name != "lfmbot-alpha-testregistry-konflux-test-storage" {
		t.Errorf("blueprint name = %q", bp.Name)
	}
	if len(bp.Connections) != 1 || bp.Connections[0].PluginName != PluginTestRegistry || bp.Connections[0].ConnectionID != connID {
		t.Fatalf("blueprint connections = %+v", bp.Connections)
	}
	got := map[string]bool{}
	for _, sc := range bp.Connections[0].Scopes {
		got[sc.ScopeID] = true
	}
	if !got["konflux-test-storage/konflux-team/release-service"] || !got["konflux-test-storage/build"] {
		t.Errorf("blueprint scopes = %+v", bp.Connections[0].Scopes)
	}
}

func TestSyncTeam_TestRegistryWithoutConnection(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()
	addTeamSources(store, "alpha", []database.SourceRow{testSuitesSource("repo")})

	result, err := svc.SyncTeam(ctx, "alpha")
	if err != nil {
		t.Fatalf("SyncTeam: %v", err)
	}
	if len(result.Errors) != 1 {
		t.Errorf("errors = %v, want missing connection error", result.Errors)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.blueprints) != 0 {
		t.Errorf("unexpected blueprints: %+v", fake.blueprints)
	}
}

func TestSyncTeam_TestRegistryDedupAcrossTeams(t *testing.T) {
	svc, _, store := setupTestService(t)
	ctx := context.Background()

	for _, team := range []string{"alpha", "beta"} {
		if _, err := svc.CreateTeamConnection(ctx, team, ConnectionEntry{
			Plugin: PluginTestRegistry, CITool: CIToolTektonCI, Organization: "shared-org", ExpiryDate: "never",
		}); err != nil {
			t.Fatalf("CreateTeamConnection(%s): %v", team, err)
		}
		addTeamSources(store, team, []database.SourceRow{testSuitesSource("shared-org/repo")})
	}

	if _, err := svc.SyncTeam(ctx, "alpha"); err != nil {
		t.Fatalf("SyncTeam(alpha): %v", err)
	}
	result, err := svc.SyncTeam(ctx, "beta")
	if err != nil {
		t.Fatalf("SyncTeam(beta): %v", err)
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("skipped = %v, want shared scope skipped", result.Skipped)
	}
	want := `shared-org/repo already collected by team alpha (blueprint "lfmbot-alpha-testregistry-shared-org"); existing data will be used for this team's metrics`
	if result.Skipped[0] != want {
		t.Errorf("skipped[0] = %q, want %q", result.Skipped[0], want)
	}
}

func TestValidateTestRegistryScopes(t *testing.T) {
	svc, fake, _ := setupTestService(t)
	ctx := context.Background()

	if _, err := svc.ValidateTestRegistryScopes(ctx, "alpha", []string{"x"}); err == nil {
		t.Error("expected error without a connection")
	}
	if _, err := svc.CreateTeamConnection(ctx, "alpha", ConnectionEntry{
		Plugin: PluginTestRegistry, CITool: CIToolOpenshiftCI, Organization: "org", Token: "t", ExpiryDate: "never",
	}); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.remoteScopeData = map[string][]json.RawMessage{PluginTestRegistry: {
		json.RawMessage(`{"name":"E2E-Tests","fullName":"E2E-Tests","id":"E2E-Tests"}`),
		json.RawMessage(`{"name":"infra","fullName":"infra","id":"infra"}`),
	}}
	fake.mu.Unlock()

	got, err := svc.ValidateTestRegistryScopes(ctx, "alpha", []string{"e2e-tests", " infra "})
	if err != nil {
		t.Fatalf("ValidateTestRegistryScopes: %v", err)
	}
	if len(got) != 2 || got[0] != "E2E-Tests" || got[1] != "infra" {
		t.Errorf("canonical names = %v", got)
	}
	if _, err := svc.ValidateTestRegistryScopes(ctx, "alpha", []string{"missing"}); err == nil {
		t.Error("expected error for unknown scope")
	}

	results, err := svc.BrowseRemoteScopes(ctx, "alpha", PluginTestRegistry, "")
	if err != nil {
		t.Fatalf("BrowseRemoteScopes: %v", err)
	}
	if len(results) != 2 || results[0].FullName != "E2E-Tests" || results[0].Plugin != PluginTestRegistry {
		t.Errorf("browse results = %+v", results)
	}
}

func TestCleanupOrphanedScopes_IncludesTestRegistry(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	connID, err := svc.CreateTeamConnection(ctx, "alpha", ConnectionEntry{
		Plugin: PluginTestRegistry, CITool: CIToolTektonCI, Organization: "q", ExpiryDate: "never",
	})
	if err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.scopes[PluginTestRegistry] = map[int][]json.RawMessage{connID: {json.RawMessage(`{"fullName":"q/orphan","name":"orphan"}`)}}
	fake.mu.Unlock()

	deleted, errs := svc.CleanupOrphanedScopes(ctx, "alpha")
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if len(deleted) == 0 || deleted[0] != "testregistry:q/orphan" {
		t.Errorf("deleted = %v, want orphaned testregistry scope", deleted)
	}
	kv, _ := store.GetEntityConfigKV(ctx, "team", "alpha")
	if kv["devlake_testregistry_connection_id"] != "" {
		t.Errorf("unused connection key not cleared: %v", kv)
	}
}

func TestProxyBrowse_TestRegistryOpenshiftCI(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ghp" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/repos/openshift/release/contents/ci-operator/config/my-org" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("ref") == "master" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"name":"e2e-tests","type":"dir"},{"name":"infra.yaml","type":"file"},{"name":"README.md","type":"file"}]`))
	}))
	t.Cleanup(gh.Close)
	orig := testRegistryGitHubAPI
	testRegistryGitHubAPI = gh.URL
	t.Cleanup(func() { testRegistryGitHubAPI = orig })

	svc, _, _ := setupTestService(t)
	results, err := svc.ProxyBrowse(context.Background(), ConnectionEntry{
		Plugin: PluginTestRegistry, CITool: CIToolOpenshiftCI, Organization: "my-org", Token: "ghp",
	}, "")
	if err != nil {
		t.Fatalf("ProxyBrowse: %v", err)
	}
	if len(results) != 2 || results[0].Key != "e2e-tests" || results[1].Key != "infra.yaml" {
		t.Errorf("results = %+v, want dir and yaml entries from the main branch", results)
	}
}

func TestProxyBrowse_TestRegistryTektonCIPaginates(t *testing.T) {
	quay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/v1/repository" || q.Get("namespace") != "store" || q.Get("public") != "true" {
			http.NotFound(w, r)
			return
		}
		if q.Get("next_page") == "" {
			_, _ = w.Write([]byte(`{"repositories":[{"namespace":"store","name":"team/a"}],"next_page":"p2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"repositories":[{"namespace":"store","name":"b"}]}`))
	}))
	t.Cleanup(quay.Close)
	orig := testRegistryQuayAPI
	testRegistryQuayAPI = quay.URL
	t.Cleanup(func() { testRegistryQuayAPI = orig })

	svc, _, _ := setupTestService(t)
	results, err := svc.ProxyBrowse(context.Background(), ConnectionEntry{
		Plugin: PluginTestRegistry, CITool: CIToolTektonCI, Organization: "store",
	}, "")
	if err != nil {
		t.Fatalf("ProxyBrowse: %v", err)
	}
	if len(results) != 2 || results[0].Key != "store/team/a" || results[1].Key != "store/b" {
		t.Errorf("results = %+v", results)
	}
}

func TestCleanupTeam_MigratesSharedTestRegistryScope(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()
	addTeam(store, "owner")
	addTeam(store, "waiting")
	addTeam(store, "other-org")

	connIDs := map[string]int{}
	for team, org := range map[string]string{"owner": "shared-org", "waiting": "shared-org", "other-org": "different-org"} {
		id, err := svc.CreateTeamConnection(ctx, team, ConnectionEntry{
			Plugin: PluginTestRegistry, CITool: CIToolTektonCI, Organization: org, ExpiryDate: "never",
		})
		if err != nil {
			t.Fatalf("CreateTeamConnection(%s): %v", team, err)
		}
		connIDs[team] = id
	}
	addTeamSources(store, "owner", []database.SourceRow{testSuitesSource("shared-org/e2e", "shared-org/owner-only")})
	addTeamSources(store, "waiting", []database.SourceRow{testSuitesSource("shared-org/e2e")})
	// Same scope name under another Quay org is a different scope and must not be deduped.
	addTeamSources(store, "other-org", []database.SourceRow{testSuitesSource("shared-org/e2e")})

	if _, err := svc.SyncTeam(ctx, "owner"); err != nil {
		t.Fatalf("SyncTeam(owner): %v", err)
	}
	waiting, err := svc.SyncTeam(ctx, "waiting")
	if err != nil {
		t.Fatalf("SyncTeam(waiting): %v", err)
	}
	if len(waiting.Skipped) != 1 || len(waiting.Created) != 0 {
		t.Fatalf("waiting sync: skipped=%v created=%v, want shared scope skipped", waiting.Skipped, waiting.Created)
	}
	other, err := svc.SyncTeam(ctx, "other-org")
	if err != nil {
		t.Fatalf("SyncTeam(other-org): %v", err)
	}
	if len(other.Skipped) != 0 || len(other.Created) != 1 {
		t.Fatalf("other-org sync: skipped=%v created=%v, want no dedup across organizations", other.Skipped, other.Created)
	}

	cleanup, err := svc.CleanupTeam(ctx, "owner")
	if err != nil {
		t.Fatalf("CleanupTeam: %v", err)
	}
	if len(cleanup.Migrated) != 1 || !strings.Contains(cleanup.Migrated[0], "waiting") {
		t.Fatalf("migrated = %v, want shared scope migrated to waiting", cleanup.Migrated)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	var waitingBP *Blueprint
	for i := range fake.blueprints {
		bp := &fake.blueprints[i]
		if hasLabel(bp.Labels, "team:owner") {
			t.Errorf("owner blueprint %q still exists", bp.Name)
		}
		if hasLabel(bp.Labels, "team:waiting") {
			waitingBP = bp
		}
	}
	if waitingBP == nil {
		t.Fatal("waiting team has no blueprint after migration")
	}
	if waitingBP.Name != "lfmbot-waiting-testregistry-shared-org" {
		t.Errorf("waiting blueprint name = %q", waitingBP.Name)
	}
	conn := waitingBP.Connections[0]
	if conn.PluginName != PluginTestRegistry || conn.ConnectionID != connIDs["waiting"] ||
		len(conn.Scopes) != 1 || conn.Scopes[0].ScopeID != "shared-org/e2e" {
		t.Errorf("waiting blueprint connection = %+v, want shared scope on its own connection", conn)
	}
	if len(fake.scopes[PluginTestRegistry][connIDs["waiting"]]) != 1 {
		t.Errorf("scope not registered on waiting's connection: %v", fake.scopes[PluginTestRegistry][connIDs["waiting"]])
	}

	for _, c := range fake.connections[PluginTestRegistry] {
		if c.ID == connIDs["owner"] {
			t.Errorf("owner's exclusive testregistry connection %d was not deleted", c.ID)
		}
	}
	if len(fake.scopes[PluginTestRegistry][connIDs["owner"]]) != 0 {
		t.Errorf("owner's scopes were not deleted: %v", fake.scopes[PluginTestRegistry][connIDs["owner"]])
	}
}
