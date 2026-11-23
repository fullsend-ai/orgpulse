package database

import (
	"encoding/json"
	"testing"
)

func TestOrgMarshalRequiredProducts(t *testing.T) {
	o := Org{ID: "myorg", Name: "My Org", Products: []string{}}
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Errorf("unmarshal org: %v", err)
	}

	if _, ok := m["products"]; !ok {
		t.Error("products field must be present even when empty")
	}
	arr, ok := m["products"].([]any)
	if !ok || len(arr) != 0 {
		t.Errorf("products should be empty array, got %v", m["products"])
	}
}

func TestOrgMarshalOmitsEmptyOptionals(t *testing.T) {
	o := Org{ID: "myorg", Name: "My Org", Products: []string{}}
	b, err := json.Marshal(o)
	if err != nil {
		t.Errorf("marshal org: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Errorf("unmarshal org: %v", err)
	}

	for _, field := range []string{"config", "dashboards"} {
		if _, ok := m[field]; ok {
			t.Errorf("field %q should be omitted when empty", field)
		}
	}
}

func TestProductMarshalRequiredTeams(t *testing.T) {
	p := Product{ID: "prod", Name: "Prod", Teams: []string{}}
	b, err := json.Marshal(p)
	if err != nil {
		t.Error(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Errorf("unmarshal product: %v", err)
	}

	if _, ok := m["teams"]; !ok {
		t.Error("teams field must be present even when empty")
	}
}

func TestProductConfigMarshal(t *testing.T) {
	slow := 168.0
	critical := 336.0
	cfg := ProductConfig{
		CycleTimeSlowHours:     &slow,
		CycleTimeCriticalHours: &critical,
		AnalyticsPrompt:        "test prompt",
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Errorf("marshal product config: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Errorf("unmarshal product config: %v", err)
	}

	if m["cycleTimeSlowHours"] != 168.0 {
		t.Errorf("expected cycleTimeSlowHours=168, got %v", m["cycleTimeSlowHours"])
	}
	if m["cycleTimeCriticalHours"] != 336.0 {
		t.Errorf("expected cycleTimeCriticalHours=336, got %v", m["cycleTimeCriticalHours"])
	}
	if m["analyticsprompt"] != "test prompt" {
		t.Errorf("expected analyticsprompt='test prompt', got %v", m["analyticsprompt"])
	}
}

func TestSourceMarshalGitrepos(t *testing.T) {
	src := Source{
		ID:   "gitrepos",
		Type: "gitrepos",
		Repos: []Repo{
			{ID: "r1", Type: "github", Owner: "org", Name: "repo1"},
		},
	}
	b, err := json.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Errorf("unmarshal source: %v", err)
	}

	if m["type"] != "gitrepos" {
		t.Errorf("expected type=gitrepos, got %v", m["type"])
	}
	if _, ok := m["repos"]; !ok {
		t.Error("gitrepos source must have repos field")
	}
	if _, ok := m["projects"]; ok {
		t.Error("gitrepos source must not have projects field")
	}
}

func TestSourceMarshalGitreposEmpty(t *testing.T) {
	src := Source{ID: "gitrepos", Type: "gitrepos"}
	b, err := json.Marshal(src)
	if err != nil {
		t.Errorf("marshal source: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Errorf("unmarshal source: %v", err)
	}

	if _, ok := m["repos"]; !ok {
		t.Error("gitrepos source must have repos field even when empty")
	}
}

func TestSourceMarshalWorkitems(t *testing.T) {
	src := Source{
		ID:   "jira",
		Type: "workitems",
		Projects: []WorkitemProject{
			{ID: "p1", ProjectName: "Proj", Type: "jira"},
		},
	}
	b, err := json.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Errorf("unmarshal source: %v", err)
	}

	if m["type"] != "workitems" {
		t.Errorf("expected type=workitems, got %v", m["type"])
	}
	if _, ok := m["projects"]; !ok {
		t.Error("workitems source must have projects field")
	}
	if _, ok := m["repos"]; ok {
		t.Error("workitems source must not have repos field")
	}
}

func TestSourceMarshalWithConfig(t *testing.T) {
	src := Source{
		ID:     "gitrepos",
		Type:   "gitrepos",
		Config: json.RawMessage(`{"key":"value"}`),
	}
	b, err := json.Marshal(src)
	if err != nil {
		t.Error(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Errorf("unmarshal source: %v", err)
	}

	cfg, ok := m["config"].(map[string]any)
	if !ok {
		t.Fatal("config should be present")
	}
	if cfg["key"] != "value" {
		t.Errorf("expected config.key=value, got %v", cfg["key"])
	}
}

func TestSourceMarshalWithoutConfig(t *testing.T) {
	src := Source{ID: "gitrepos", Type: "gitrepos"}
	b, err := json.Marshal(src)
	if err != nil {
		t.Errorf("marshal source: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Errorf("unmarshal source: %v", err)
	}

	if _, ok := m["config"]; ok {
		t.Error("config should be omitted when nil")
	}
}

func TestDashboardMarshalOmitsEmptyOptionals(t *testing.T) {
	d := Dashboard{ID: "dash1", Type: "prcycletime", Title: "PR Cycle Time"}
	b, err := json.Marshal(d)
	if err != nil {
		t.Errorf("marshal dashboard: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Errorf("unmarshal dashboard: %v", err)
	}

	for _, field := range []string{"description", "defaultsource", "connectionid", "jiraproject", "chaturl", "aianalyticsurl", "blueprintid", "blueprintids", "diagrams"} {
		if _, ok := m[field]; ok {
			t.Errorf("field %q should be omitted when empty", field)
		}
	}
}

func TestDiagramMarshalPosition(t *testing.T) {
	pos := 0
	span := 2
	d := Diagram{Position: &pos, Type: "json", Endpoint: "http://example.com", Span: &span, AIAnalytics: true}
	b, err := json.Marshal(d)
	if err != nil {
		t.Errorf("marshal diagram: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Errorf("unmarshal diagram: %v", err)
	}

	if m["position"] != 0.0 {
		t.Errorf("expected position=0, got %v", m["position"])
	}
	if m["span"] != 2.0 {
		t.Errorf("expected span=2, got %v", m["span"])
	}
	if m["aianalytics"] != true {
		t.Errorf("expected aianalytics=true, got %v", m["aianalytics"])
	}
}

func TestOverrideRepoMarshal(t *testing.T) {
	r := OverrideRepo{
		ID:    "repo1",
		Type:  "github",
		Owner: "org",
		Name:  "repo1",
		Teams: []string{"team-a", "team-b"},
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Errorf("marshal override repo: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Errorf("unmarshal override repo: %v", err)
	}

	if m["id"] != "repo1" {
		t.Errorf("expected id=repo1, got %v", m["id"])
	}
	teams, ok := m["teams"].([]any)
	if !ok || len(teams) != 2 {
		t.Errorf("expected 2 teams, got %v", m["teams"])
	}
}

func TestBuildProductConfigNil(t *testing.T) {
	cfg := buildProductConfig(nil, nil)
	if cfg != nil {
		t.Error("expected nil config when no data")
	}
}

func TestBuildProductConfigWithValues(t *testing.T) {
	kv := map[string]string{
		"cycleTimeSlowHours":     "168",
		"cycleTimeCriticalHours": "336",
		"analyticsprompt":        "test",
	}
	overrides := []OverrideRepo{
		{ID: "r1", Type: "github", Owner: "o", Name: "n", Teams: []string{"t1"}},
	}
	cfg := buildProductConfig(kv, overrides)
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.CycleTimeSlowHours == nil || *cfg.CycleTimeSlowHours != 168 {
		t.Errorf("expected cycleTimeSlowHours=168, got %v", cfg.CycleTimeSlowHours)
	}
	if cfg.CycleTimeCriticalHours == nil || *cfg.CycleTimeCriticalHours != 336 {
		t.Errorf("expected cycleTimeCriticalHours=336, got %v", cfg.CycleTimeCriticalHours)
	}
	if cfg.AnalyticsPrompt != "test" {
		t.Errorf("expected analyticsprompt=test, got %q", cfg.AnalyticsPrompt)
	}
	if cfg.Overrides == nil || cfg.Overrides.Sources == nil || len(cfg.Overrides.Sources.Repos) != 1 {
		t.Error("expected overrides with 1 repo")
	}
}
