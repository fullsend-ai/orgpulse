package report

import (
	"html/template"
	"strings"
	"testing"
)

func TestAssembleHTMLWithSections(t *testing.T) {
	sections := []reportSection{
		{
			ImageDataURI: template.URL("data:image/png;base64,AAAA"),
			AnalysisHTML: "<p>Analysis one</p>",
		},
		{
			AnalysisHTML: "<p>Analysis two</p>",
		},
	}

	html, err := assembleHTML("TestProduct — TestTeam", "<p>Executive Summary</p>", sections)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := string(html)
	if !strings.Contains(s, "<!DOCTYPE html>") {
		t.Error("missing DOCTYPE")
	}
	if !strings.Contains(s, "<title>TestProduct — TestTeam</title>") {
		t.Error("missing title")
	}
	if !strings.Contains(s, "<p>Executive Summary</p>") {
		t.Error("missing executive summary")
	}
	if !strings.Contains(s, "data:image/png;base64,AAAA") {
		t.Error("missing chart image")
	}
	if !strings.Contains(s, "<p>Analysis one</p>") {
		t.Error("missing analysis one")
	}
	if !strings.Contains(s, "<p>Analysis two</p>") {
		t.Error("missing analysis two")
	}
	if strings.Count(s, "<img class=\"chart-img\"") != 1 {
		t.Error("second section should not have a chart image")
	}
}

func TestAssembleHTMLEmpty(t *testing.T) {
	html, err := assembleHTML("P", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(html), "<!DOCTYPE html>") {
		t.Error("missing DOCTYPE for empty report")
	}
}

func TestBuildTitle(t *testing.T) {
	tests := []struct {
		product, team, want string
	}{
		{"prod", "team", "prod — team"},
		{"prod", "", "prod"},
		{"", "team", "team"},
		{"", "", "Metrics Report"},
	}
	for _, tt := range tests {
		got := buildTitle(tt.product, tt.team)
		if got != tt.want {
			t.Errorf("buildTitle(%q, %q) = %q, want %q", tt.product, tt.team, got, tt.want)
		}
	}
}

func TestSanitizeEmailHTML(t *testing.T) {
	keep := `<table border="0" cellpadding="4"><tr><td style="color:#c00;font-weight:600" align="right">5%</td></tr></table><p class="x"><b>Summary</b><br><a href="https://example.com/pr/1">PR</a></p>`
	got := sanitizeEmailHTML(keep)
	for _, want := range []string{`style="color:#c00;font-weight:600"`, `align="right"`, `cellpadding="4"`, `class="x"`, `<b>Summary</b>`, `href="https://example.com/pr/1"`} {
		if !strings.Contains(got, want) {
			t.Errorf("sanitized output lost %q: %s", want, got)
		}
	}
	bad := `<script>alert(1)</script><img src=x onerror="alert(1)"><a href="javascript:alert(1)">x</a><form action="https://evil"><input name=p></form><iframe src="https://evil"></iframe>`
	got = sanitizeEmailHTML(bad)
	for _, banned := range []string{"<script", "onerror", "javascript:", "<form", "<input", "<iframe"} {
		if strings.Contains(strings.ToLower(got), banned) {
			t.Errorf("sanitized output kept %q: %s", banned, got)
		}
	}
	if got := sanitizeEmailHTML("Set <4h first-review SLA"); !strings.Contains(got, "&lt;4h") {
		t.Errorf("plain text not preserved: %q", got)
	}
}

func TestHeaderValue(t *testing.T) {
	if got := headerValue("prod\r\nBcc: victim@example.com"); strings.ContainsAny(got, "\r\n") {
		t.Errorf("CRLF survived: %q", got)
	}
	if got := headerValue("Metrics Report"); got != "Metrics Report" {
		t.Errorf("ASCII subject changed: %q", got)
	}
	if got := headerValue("prod — team"); !strings.HasPrefix(got, "=?utf-8?q?") {
		t.Errorf("non-ASCII subject not encoded: %q", got)
	}
}
