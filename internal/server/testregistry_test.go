package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fullsend-ai/orgpulse/internal/database"
	"github.com/fullsend-ai/orgpulse/internal/devlake"
)

func TestValidateConnectionEntryFields(t *testing.T) {
	tests := []struct {
		name  string
		entry devlake.ConnectionEntry
		ok    bool
	}{
		{"github complete", devlake.ConnectionEntry{Plugin: "github", Token: "t", Endpoint: "https://api.github.com"}, true},
		{"github missing endpoint", devlake.ConnectionEntry{Plugin: "github", Token: "t"}, false},
		{"testregistry tekton without token or endpoint", devlake.ConnectionEntry{Plugin: "testregistry", CITool: devlake.CIToolTektonCI, Organization: "q"}, true},
		{"testregistry missing ci tool", devlake.ConnectionEntry{Plugin: "testregistry", Organization: "q"}, false},
		{"testregistry missing organization", devlake.ConnectionEntry{Plugin: "testregistry", CITool: devlake.CIToolOpenshiftCI, Token: "t"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := validateConnectionEntryFields(tt.entry)
			if (msg == "") != tt.ok {
				t.Errorf("validateConnectionEntryFields() = %q, want ok=%v", msg, tt.ok)
			}
		})
	}
}

func TestValidateAndAppendItems_TestSuites(t *testing.T) {
	ctx := context.Background()
	var req onboardSourceRequest
	if err := json.Unmarshal([]byte(`{"team_id":"team-a","source_id":"testsuites","items":[
		{"type":"testregistry","project_name":"q/repo-a"},
		{"type":"testregistry","project_name":"Q/REPO-A"},
		{"type":"testregistry","project_name":"q/repo-b"}]}`), &req); err != nil {
		t.Fatal(err)
	}
	source := &database.SourceRow{ID: "testsuites", Type: "testsuites"}
	updated := &database.SourceRow{ID: "testsuites", Type: "testsuites"}
	warnings, validationErr := validateAndAppendItems(ctx, req, source, updated, nil)
	if validationErr != "" {
		t.Fatalf("validateAndAppendItems: %s", validationErr)
	}
	if len(updated.Projects) != 2 {
		t.Fatalf("projects = %+v, want 2 (duplicate skipped)", updated.Projects)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want one duplicate warning", warnings)
	}
	for _, p := range updated.Projects {
		if p.Type != "testregistry" || p.JiraProject != "" || p.BoardID != "" {
			t.Errorf("unexpected stored project %+v", p)
		}
	}

	if err := json.Unmarshal([]byte(`{"team_id":"team-a","source_id":"testsuites","items":[{"type":"jira","board_id":"1"}]}`), &req); err != nil {
		t.Fatal(err)
	}
	if _, msg := validateAndAppendItems(ctx, req, source, &database.SourceRow{}, nil); msg == "" {
		t.Error("expected jira item to be rejected in testsuites source")
	}
}

func TestValidateTestRegistryItems_WrongSource(t *testing.T) {
	req := onboardSourceRequest{TeamID: "team-a", SourceID: "workitems"}
	if err := json.Unmarshal([]byte(`[{"type":"testregistry","project_name":"x"}]`), &req.Items); err != nil {
		t.Fatal(err)
	}
	if msg := validateTestRegistryItems(context.Background(), &req, nil); msg == "" {
		t.Error("expected testregistry items outside testsuites to be rejected")
	}
}

func TestValidateTestRegistryItems_CanonicalizesAgainstDevLake(t *testing.T) {
	ctx := context.Background()
	devlakeAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/plugins/testregistry/connections/7/remote-scopes") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"children":[{"type":"scope","data":{"fullName":"Q/Repo","name":"Repo"}}]}`))
	}))
	t.Cleanup(devlakeAPI.Close)

	store := database.NewMemStore()
	if err := store.SetEntityConfigKey(ctx, "team", "team-a", "devlake_testregistry_connection_id", "7"); err != nil {
		t.Fatal(err)
	}
	svc := devlake.NewService(devlake.Config{APIURL: devlakeAPI.URL, AllowPrivateEndpoints: true}, devlakeAPI.Client(), store)

	req := onboardSourceRequest{TeamID: "team-a", SourceID: "testsuites"}
	if err := json.Unmarshal([]byte(`[{"type":"testregistry","project_name":"q/repo"}]`), &req.Items); err != nil {
		t.Fatal(err)
	}
	if msg := validateTestRegistryItems(ctx, &req, svc); msg != "" {
		t.Fatalf("validateTestRegistryItems: %s", msg)
	}
	if req.Items[0].ProjectName != "Q/Repo" {
		t.Errorf("project_name = %q, want canonical Q/Repo", req.Items[0].ProjectName)
	}

	if err := json.Unmarshal([]byte(`[{"type":"testregistry","project_name":"unknown"}]`), &req.Items); err != nil {
		t.Fatal(err)
	}
	if msg := validateTestRegistryItems(ctx, &req, svc); msg == "" {
		t.Error("expected unknown scope to be rejected")
	}
}

func TestValidateOnboardSource_TestSuites(t *testing.T) {
	src := database.SourceRow{ID: "testsuites", Type: "testsuites", Projects: []database.WorkitemProjRow{
		{ID: "0", Type: "testregistry", ProjectName: "q/repo"},
	}}
	if msg := validateOnboardSource(src, map[string]bool{}, nil); msg == "" {
		t.Error("expected error without a testregistry connection")
	}
	if msg := validateOnboardSource(src, map[string]bool{"testregistry": true}, nil); msg != "" {
		t.Errorf("unexpected error: %s", msg)
	}
	src.Projects[0].ProjectName = " "
	if msg := validateOnboardSource(src, map[string]bool{"testregistry": true}, nil); msg == "" {
		t.Error("expected error for empty project_name")
	}
}

func TestValidateOnboardRequest_TestRegistryConnectionWithoutEndpoint(t *testing.T) {
	req := onboardRequest{
		Team: database.TeamRow{ID: "t", Name: "T", AuthGroup: "g"},
		Connections: []devlake.ConnectionEntry{{
			Plugin: "testregistry", CITool: devlake.CIToolTektonCI, Organization: "q", ExpiryDate: "never",
		}},
	}
	if msg := validateOnboardRequest(req); msg != "" {
		t.Errorf("validateOnboardRequest() = %q, want ok", msg)
	}
	req.Connections[0].CITool = ""
	if msg := validateOnboardRequest(req); msg == "" {
		t.Error("expected error without ci_tool")
	}
}
