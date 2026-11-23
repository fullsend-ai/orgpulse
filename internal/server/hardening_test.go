package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/fullsend-ai/orgpulse/internal/authz"
	"github.com/fullsend-ai/orgpulse/internal/database"
	"github.com/fullsend-ai/orgpulse/internal/logging"
)

var testReportURLs = []string{"https://example.com/"}

func TestReportURLAllowed(t *testing.T) {
	trusted := trustedReportURLs(FrontendConfig{
		WorkflowBaseURL: "https://dash.example.com",
		AIAnalyticsURL:  "https://ai.example.com/analyze",
	}, "http://workflows:7070")
	tests := []struct {
		url  string
		want bool
	}{
		{"https://dash.example.com/workflows/ftprovertime", true},
		{"http://workflows:7070/workflows/issuethroughput?x=1", true},
		{"https://ai.example.com/analyze", true},
		{"https://dash.example.com/workflows/", false},
		{"https://dash.example.com/workflows/../api/v1/keys", false},
		{"https://dash.example.com/workflows/a/../../api", false},
		{"https://dash.example.com/api/v1/keys", false},
		{"https://ai.example.com/analyze/other", false},
		{"http://169.254.169.254/latest/meta-data", false},
		{"https://user:pw@dash.example.com/workflows/x", false},
		{"https://dash.example.com.evil.com/workflows/x", false},
		{"not a url", false},
	}
	for _, tt := range tests {
		if got := reportURLAllowed(tt.url, trusted); got != tt.want {
			t.Errorf("reportURLAllowed(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestReportURLs_NonAdminRestricted_AdminUnrestricted(t *testing.T) {
	trusted := []string{"https://dash.example.com/workflows/"}
	for _, tc := range []struct {
		name    string
		ident   Identity
		wantErr bool
	}{
		{"non-admin", Identity{Email: "alice@example.com"}, true},
		{"admin", Identity{Email: "admin@example.com", IsAdmin: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := database.NewMemStore()
			body := jsonBodyForTest(t, database.AlertRow{URL: "http://internal-service:8080/secret", Rule: "true", AIAnalyticsURL: "https://attacker.example/collect"})
			r := httptest.NewRequest("POST", "/api/v1/alerts", body)
			r = r.WithContext(withIdentity(context.Background(), tc.ident))
			w := httptest.NewRecorder()
			alertCreateHandler(store, trusted).ServeHTTP(w, r)
			if tc.wantErr && w.Code != http.StatusForbidden {
				t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
			}
			if !tc.wantErr && w.Code != http.StatusCreated {
				t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
			}

			sub := database.SubscriptionRow{Elements: []database.SubscriptionElementRow{{URL: "http://internal-service:8080/secret"}}}
			r = httptest.NewRequest("POST", "/api/v1/subscriptions", jsonBodyForTest(t, sub))
			r = r.WithContext(withIdentity(context.Background(), tc.ident))
			w = httptest.NewRecorder()
			subscriptionCreateHandler(store, trusted).ServeHTTP(w, r)
			if tc.wantErr && w.Code != http.StatusForbidden {
				t.Fatalf("subscription: expected 403, got %d: %s", w.Code, w.Body.String())
			}
			if !tc.wantErr && w.Code != http.StatusCreated {
				t.Fatalf("subscription: expected 201, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestConfigGet_RedactsIntegrationTokens(t *testing.T) {
	store := database.NewMemStore()
	ctx := context.Background()
	if err := store.ReplaceEntityConfig(ctx, "team", "t1", map[string]string{
		"devlake_github_token":        "ghp_secret",
		"devlake_github_token_expiry": "2027-01-01",
		"devlake_jira_token":          "",
		"devlake_jira_username":       "bob",
		"my_custom_token":             "kept",
	}); err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest("GET", "/api/v1/config/team/t1", nil)
	r.SetPathValue("type", "team")
	r.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	configGetHandler(store).ServeHTTP(w, r)

	var got map[string]string
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["devlake_github_token"] != redactedSecret {
		t.Errorf("token not redacted: %q", got["devlake_github_token"])
	}
	if got["devlake_github_token_expiry"] != "2027-01-01" || got["devlake_jira_username"] != "bob" || got["my_custom_token"] != "kept" {
		t.Errorf("non-secret keys changed: %v", got)
	}
	if got["devlake_jira_token"] != "" {
		t.Errorf("empty token must stay empty so the UI sees it as unset, got %q", got["devlake_jira_token"])
	}
}

func TestConfigPut_DoesNotStorePlaceholder(t *testing.T) {
	store := database.NewMemStore()
	ctx := context.Background()
	if err := store.ReplaceEntityConfig(ctx, "team", "t1", map[string]string{"devlake_github_token": "ghp_secret"}); err != nil {
		t.Fatal(err)
	}
	body := `{"devlake_github_token":"********","devlake_gitlab_token":"********","color":"blue"}`
	r := httptest.NewRequest("PUT", "/api/v1/config/team/t1", strings.NewReader(body))
	r.SetPathValue("type", "team")
	r.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	configPutHandler(store).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", w.Code, w.Body.String())
	}
	stored, _ := store.GetEntityConfigKV(ctx, "team", "t1")
	if stored["devlake_github_token"] != "ghp_secret" {
		t.Errorf("stored token changed: %q", stored["devlake_github_token"])
	}
	if _, ok := stored["devlake_gitlab_token"]; ok {
		t.Errorf("placeholder stored for a token that did not exist")
	}
	if stored["color"] != "blue" {
		t.Errorf("regular key not saved")
	}
	if strings.Contains(w.Body.String(), "ghp_secret") {
		t.Errorf("PUT response leaks token: %s", w.Body.String())
	}
}

func TestCheckScopeKeys(t *testing.T) {
	tests := []struct {
		body    string
		wantErr bool
	}{
		{`{"owner":"a","name":"b"}`, false},
		{`{"owner":["a"],"name":["b"],"extra":1,"extra":2}`, false},
		{`{"projects":[{"id":"P"},"Q"]}`, false},
		{`{"owner":"victim","name":"secret","Owner":"mine","Name":"mine"}`, true},
		{`{"owner":"a","owner":"b","name":"c"}`, true},
		{`{"projects":[],"projectſ":[{"id":"X"}]}`, true},
		{`{"projects":[{"id":"P","ID":"X"}]}`, true},
		{`{"JIRAPROJECT":"X"}`, true},
		{`[]`, false},
		{``, false},
	}
	for _, tt := range tests {
		err := checkScopeKeys([]byte(tt.body))
		if (err != nil) != tt.wantErr {
			t.Errorf("checkScopeKeys(%s) err = %v, wantErr %v", tt.body, err, tt.wantErr)
		}
	}
	if err := checkScopeInput(map[string]any{"owner": "a", "Owner": "b"}); err == nil {
		t.Error("checkScopeInput accepted case variant")
	}
	if err := checkScopeInput(map[string]any{"owner": "a", "projects": []any{map[string]any{"Id": "x"}}}); err == nil {
		t.Error("checkScopeInput accepted project key case variant")
	}
	if err := checkScopeInput(map[string]any{"owner": "a", "name": "b", "days": 30}); err != nil {
		t.Errorf("checkScopeInput rejected canonical input: %v", err)
	}
}

func TestMatchAdminUser_EmailDomains(t *testing.T) {
	admins := []string{"alice"}
	if !matchAdminUser("", "alice@redhat.com", admins, nil) {
		t.Error("legacy prefix match broken")
	}
	if !matchAdminUser("", "alice@gmail.com", admins, nil) {
		t.Error("legacy prefix match must stay unrestricted when no domains are configured")
	}
	if !matchAdminUser("", "alice@redhat.com", admins, []string{"redhat.com"}) {
		t.Error("prefix match within configured domain failed")
	}
	if matchAdminUser("", "alice@gmail.com", admins, []string{"redhat.com"}) {
		t.Error("prefix match outside configured domain must fail")
	}
	if !matchAdminUser("alice", "alice@gmail.com", admins, []string{"redhat.com"}) {
		t.Error("exact username match must still work")
	}
	if !matchAdminUser("", "alice@gmail.com", []string{"alice@gmail.com"}, []string{"redhat.com"}) {
		t.Error("exact email match must still work")
	}
	keys := identityKeys(Identity{Email: "bob@gmail.com"}, []string{"redhat.com"})
	for _, k := range keys {
		if k == "bob" {
			t.Error("directory lookup by local part outside configured domain")
		}
	}
}

func TestVSCodeToken_GetDoesNotMintKey(t *testing.T) {
	store := database.NewMemStore()
	cb := url.QueryEscape("http://127.0.0.1:5555/callback")
	r := httptest.NewRequest("GET", "/auth/vscode-token?callback="+cb+"&state=abcdefghijklmnop", nil)
	r = r.WithContext(withIdentity(r.Context(), Identity{Email: "admin@example.com", IsAdmin: true}))
	w := httptest.NewRecorder()
	vsCodeTokenConfirmHandler().ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `method="POST"`) {
		t.Fatalf("expected confirmation page, got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("confirmation page must not be frameable")
	}
	keys, _ := store.ListAPIKeys(r.Context())
	if len(keys) != 0 {
		t.Fatalf("GET minted %d keys", len(keys))
	}

	r = httptest.NewRequest("GET", "/auth/vscode-token?callback="+cb+"&state=bad", nil)
	w = httptest.NewRecorder()
	vsCodeTokenConfirmHandler().ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid state: expected 400, got %d", w.Code)
	}
}

func TestVSCodeToken_PostMintsKeyAndEchoesState(t *testing.T) {
	store := database.NewMemStore()
	form := url.Values{"callback": {"http://127.0.0.1:5555/callback"}, "state": {"abcdefghijklmnop"}}
	r := httptest.NewRequest("POST", "/auth/vscode-token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = r.WithContext(withIdentity(r.Context(), Identity{Email: "admin@example.com", IsAdmin: true}))
	w := httptest.NewRecorder()
	vsCodeTokenHandler(store).ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", w.Code, w.Body.String())
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil || loc.Host != "127.0.0.1:5555" || loc.Query().Get("token") == "" || loc.Query().Get("state") != "abcdefghijklmnop" {
		t.Fatalf("bad redirect %q", w.Header().Get("Location"))
	}

	form.Set("callback", "http://evil.example.com/callback")
	r = httptest.NewRequest("POST", "/auth/vscode-token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	vsCodeTokenHandler(store).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("non-loopback callback: expected 400, got %d", w.Code)
	}
}

func TestCrossOriginProtection(t *testing.T) {
	h := crossOriginProtection(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	tests := []struct {
		name   string
		method string
		header map[string]string
		want   int
	}{
		{"same-origin browser POST", "POST", map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusNoContent},
		{"cross-site browser POST", "POST", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"cross-site browser GET", "GET", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusNoContent},
		{"non-browser POST", "POST", nil, http.StatusNoContent},
		{"cross-site POST with API key", "POST", map[string]string{"Sec-Fetch-Site": "cross-site", "X-API-Key": "k"}, http.StatusNoContent},
		{"cross-site POST with bearer", "POST", map[string]string{"Sec-Fetch-Site": "cross-site", "Authorization": "Bearer k"}, http.StatusNoContent},
		{"cross-site POST with basic auth", "POST", map[string]string{"Sec-Fetch-Site": "cross-site", "Authorization": "Basic dTpw"}, http.StatusForbidden},
	}
	for _, tt := range tests {
		r := httptest.NewRequest(tt.method, "http://dash.example.com/api/v1/teams", nil)
		for k, v := range tt.header {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tt.want {
			t.Errorf("%s: got %d, want %d", tt.name, w.Code, tt.want)
		}
	}
}

func TestRunJSTimesOut(t *testing.T) {
	old := jsTimeout
	jsTimeout = 100 * time.Millisecond
	defer func() { jsTimeout = old }()
	if _, err := runJS("while(true){}", nil); err == nil {
		t.Fatal("expected infinite loop to be interrupted")
	}
}

func TestRelPutScoped_ChildMustBeUpdatable(t *testing.T) {
	links := map[string][]database.RelEntry{"o1": {{ID: "p1"}, {ID: "p-other"}}}
	put := func(_ context.Context, id string, e []database.RelEntry) error { links[id] = e; return nil }
	get := func(_ context.Context, id string) ([]database.RelEntry, error) { return links[id], nil }
	h := relPutScoped(put, get, authz.Grant.CanUpdateOrg, authz.Grant.CanUpdateProduct)
	grant := authz.Grant{
		OrgAggregate:     map[string]struct{}{"o1": {}},
		ProductAggregate: map[string]struct{}{"p1": {}, "p2": {}},
	}
	do := func(g authz.Grant, body string) int {
		r := httptest.NewRequest("PUT", "/api/v1/orgs/o1/products", strings.NewReader(body))
		r.SetPathValue("id", "o1")
		r = r.WithContext(withGrant(r.Context(), g))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if code := do(grant, `[{"id":"p1"},{"id":"p-other"},{"id":"victim"}]`); code != http.StatusForbidden {
		t.Errorf("linking a product the caller cannot update: got %d, want 403", code)
	}
	if len(links["o1"]) != 2 {
		t.Fatalf("links changed on rejected request: %v", links["o1"])
	}
	if code := do(grant, `[{"id":"p-other"},{"id":"p1"},{"id":"p2"}]`); code != http.StatusOK {
		t.Errorf("reorder kept children and added an updatable one: got %d, want 200", code)
	}
	if code := do(grant, `[{"id":"p2"}]`); code != http.StatusOK {
		t.Errorf("removing children: got %d, want 200", code)
	}
	if code := do(authz.Grant{Bypass: true}, `[{"id":"anything"}]`); code != http.StatusOK {
		t.Errorf("admin: got %d, want 200", code)
	}
	if code := do(grant, `not json`); code != http.StatusBadRequest {
		t.Errorf("bad JSON: got %d, want 400", code)
	}
}

func TestValidateSource_RequiresTeamAccess(t *testing.T) {
	store := database.NewMemStore()
	r := httptest.NewRequest("POST", "/api/v1/teams/t1/validate-source", strings.NewReader(`{"plugin":"github","owner":"o","name":"n"}`))
	r.SetPathValue("teamId", "t1")
	ctx := withIdentity(r.Context(), Identity{Username: "bob", Email: "bob@example.com"})
	r = r.WithContext(withGrant(ctx, authz.Grant{Teams: map[string]struct{}{"t2": {}}}))
	w := httptest.NewRecorder()
	validateSourceHandler(store, nil).ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403", w.Code)
	}
}

func TestRequestIDValidation(t *testing.T) {
	var seen string
	h := withRequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = logging.RequestIDFromContext(r.Context())
	}))
	for in, keep := range map[string]bool{
		"0af7651916cd43dd8448eb211c80319c":     true,
		"4bf92f35-77b3-4da7-a3ce-929bbba3a1b1": true,
		"trace:1.2_3":                          true,
		"evil\nlog line":                       false,
		strings.Repeat("a", 129):               false,
		"<script>":                             false,
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("X-Request-ID", in)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if (seen == in) != keep {
			t.Errorf("X-Request-ID %q: kept=%v, want %v", in, seen == in, keep)
		}
		if w.Header().Get("X-Request-ID") != seen {
			t.Errorf("response header %q != context %q", w.Header().Get("X-Request-ID"), seen)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	srv := New(0, false, fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}, nil, nil, nil, FrontendConfig{}, nil, AIAssistConfig{}, nil, nil, "")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "SAMEORIGIN",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	} {
		if got := w.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "object-src 'none'") {
		t.Errorf("CSP = %q", csp)
	}
}

func TestWriteDetailedError(t *testing.T) {
	const detail = "Error 1146: Table 'lake.secret' doesn't exist"
	cases := []struct {
		name       string
		identity   *Identity
		wantDetail bool
	}{
		{name: "admin sees detail", identity: &Identity{Username: "admin", IsAdmin: true}, wantDetail: true},
		{name: "non-admin sees generic message", identity: &Identity{Username: "user"}},
		{name: "no identity sees generic message"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/x", nil)
			ctx := logging.WithRequestID(r.Context(), "rid-1")
			if tc.identity != nil {
				ctx = withIdentity(ctx, *tc.identity)
			}
			r = r.WithContext(ctx)
			w := httptest.NewRecorder()
			writeDetailedError(w, r, "list scopes failed", errors.New(detail))

			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", w.Code)
			}
			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if got := strings.Contains(body["error"], detail); got != tc.wantDetail {
				t.Errorf("detail present = %v, want %v (body %q)", got, tc.wantDetail, body["error"])
			}
			if !tc.wantDetail && !strings.Contains(body["error"], "rid-1") {
				t.Errorf("generic body lacks request id: %q", body["error"])
			}
		})
	}
}

func TestAIGenerate_RequiresAdmin(t *testing.T) {
	store := database.NewMemStore()
	srv := New(0, false, fstest.MapFS{}, nil, store, nil, FrontendConfig{}, nil, AIAssistConfig{APIKey: "k", BaseURL: "http://127.0.0.1:1"}, nil, nil, "")
	r := httptest.NewRequest("POST", "/api/v1/ai/generate", strings.NewReader(`{"prompt":"x"}`))
	r.Header.Set("X-Forwarded-User", "bob")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("non-admin: got %d, want 403", w.Code)
	}
}
