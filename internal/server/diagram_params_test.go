package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fullsend-ai/orgpulse/internal/database"
	"github.com/fullsend-ai/orgpulse/schemas"
)

func TestValidateDiagramParams(t *testing.T) {
	cases := []struct {
		params string
		ok     bool
	}{
		{"", true},
		{"null", true},
		{`{"view":"criteria"}`, true},
		{` {"view":"flow"}`, true},
		{`["view"]`, false},
		{`"criteria"`, false},
	}
	for _, c := range cases {
		msg := validateDiagramParams([]database.DiagramRow{{Position: 1, Params: json.RawMessage(c.params)}})
		if (msg == "") != c.ok {
			t.Errorf("params %q: msg = %q, want ok=%v", c.params, msg, c.ok)
		}
	}
}

func TestDashboardTemplates_AIAnalyticsEnabled(t *testing.T) {
	var templates []database.DashboardRow
	if err := json.Unmarshal([]byte(schemas.DashboardTemplatesJSON), &templates); err != nil {
		t.Fatalf("parse templates: %v", err)
	}
	for _, tmpl := range templates {
		for _, d := range tmpl.Diagrams {
			if !d.AIAnalytics {
				t.Errorf("template %s diagram %d (%s): ai_analytics should be true", tmpl.DashboardID, d.Position, d.Endpoint)
			}
		}
	}
}

func TestDashboardTemplates_DocumentationURLs(t *testing.T) {
	var templates []database.DashboardRow
	if err := json.Unmarshal([]byte(schemas.DashboardTemplatesJSON), &templates); err != nil {
		t.Fatalf("parse templates: %v", err)
	}
	for _, tmpl := range templates {
		for _, d := range tmpl.Diagrams {
			if d.DocumentationURL == "" {
				continue
			}
			if !strings.HasPrefix(d.DocumentationURL, "https://") {
				t.Errorf("template %s diagram %d: documentation_url %q is not HTTPS", tmpl.DashboardID, d.Position, d.DocumentationURL)
			}
		}
	}
}

func TestDashboardTemplates_DiagramParams(t *testing.T) {
	var templates []database.DashboardRow
	if err := json.Unmarshal([]byte(schemas.DashboardTemplatesJSON), &templates); err != nil {
		t.Fatalf("parse templates: %v", err)
	}
	var found bool
	for _, tmpl := range templates {
		if msg := validateDiagramParams(tmpl.Diagrams); msg != "" {
			t.Errorf("template %s: %s", tmpl.DashboardID, msg)
		}
		if tmpl.Type != "aisdlc" {
			continue
		}
		found = true
		for _, d := range tmpl.Diagrams {
			var p map[string]any
			if err := json.Unmarshal(d.Params, &p); err != nil || p["view"] == nil {
				t.Errorf("aisdlc diagram %d: params %s must set view", d.Position, d.Params)
			}
		}
	}
	if !found {
		t.Fatal("no aisdlc dashboard template")
	}
}
