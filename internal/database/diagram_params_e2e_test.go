//go:build e2e

package database

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

// TestE2E_DiagramParams_RoundTrip proves the diagrams.params JSON column
// survives create, update and both read paths (dashboard API and config export).
func TestE2E_DiagramParams_RoundTrip(t *testing.T) {
	host, port, user, password, dbName := e2eConfig(t)
	caBundle := filepath.Join(testdataTLSDir, "ca.pem")
	ctx := context.Background()

	if err := Migrate(ctx, host, port, user, password, dbName, caBundle); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store, err := NewMySQLStore(host, port, user, password, dbName, caBundle)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer store.Close()

	if err := store.CreateTeam(ctx, TeamRow{ID: "params-team", Name: "Params"}); err != nil {
		t.Fatalf("create team: %v", err)
	}
	t.Cleanup(func() { _ = store.DeleteTeam(context.Background(), "params-team") })

	id, err := store.CreateDashboard(ctx, DashboardRow{
		OwnerType: "team", OwnerID: "params-team", DashboardID: "aisdlc", Type: "aisdlc", Title: "AISDLC",
		Diagrams: []DiagramRow{
			{Position: 0, Type: "json", Endpoint: "/workflows/production/a", Params: json.RawMessage(`{"view":"criteria"}`)},
			{Position: 1, Type: "json", Endpoint: "/workflows/production/b"},
		},
	})
	if err != nil {
		t.Fatalf("create dashboard: %v", err)
	}
	t.Cleanup(func() { _ = store.DeleteDashboard(context.Background(), id) })

	row, err := store.GetDashboardRow(ctx, id)
	if err != nil || row == nil {
		t.Fatalf("get dashboard: %v", err)
	}
	assertView(t, row.Diagrams[0].Params, "criteria")
	if row.Diagrams[1].Params != nil {
		t.Fatalf("diagram without params read back as %s, want nil", row.Diagrams[1].Params)
	}

	row.Diagrams[1].Params = json.RawMessage(`{"view":"trend"}`)
	if err := store.UpdateDashboard(ctx, id, *row); err != nil {
		t.Fatalf("update dashboard: %v", err)
	}
	teams, err := store.GetTeams(ctx)
	if err != nil {
		t.Fatalf("get teams: %v", err)
	}
	for _, team := range teams {
		if team.ID != "params-team" {
			continue
		}
		if len(team.Dashboards) != 1 || len(team.Dashboards[0].Diagrams) != 2 {
			t.Fatalf("export dashboards = %+v, want 1 dashboard with 2 diagrams", team.Dashboards)
		}
		assertView(t, team.Dashboards[0].Diagrams[0].Params, "criteria")
		assertView(t, team.Dashboards[0].Diagrams[1].Params, "trend")
		return
	}
	t.Fatal("params-team missing from GetTeams")
}

func assertView(t *testing.T, raw json.RawMessage, want string) {
	t.Helper()
	var p map[string]string
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("params %q: %v", raw, err)
	}
	if p["view"] != want {
		t.Fatalf("params view = %q, want %q", p["view"], want)
	}
}
