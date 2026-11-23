package chart

import (
	"encoding/json"
	"testing"
)

var pngHeader = []byte{0x89, 0x50, 0x4e, 0x47}

func TestRenderBarChart(t *testing.T) {
	req := makeRequest(t, `{
		"type": "bar",
		"label": "PR Count",
		"description": "test",
		"lastupdate": 1000,
		"data": {
			"labels": ["Fast", "Average", "Slow"],
			"datasets": [{"label": "PR count", "data": [12, 45, 8]}]
		}
	}`)
	buf, err := Render(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertPNG(t, buf)
}

func TestRenderLineChart(t *testing.T) {
	req := makeRequest(t, `{
		"type": "line",
		"label": "Cycle Time",
		"description": "test",
		"lastupdate": 1000,
		"data": {
			"labels": ["2024-01", "2024-02", "2024-03"],
			"datasets": [
				{"label": "Cycle Time", "data": [3.5, 4.2, 2.8]},
				{"label": "Review Time", "data": [1.2, 1.8, 0.9]}
			]
		}
	}`)
	buf, err := Render(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertPNG(t, buf)
}

func TestRenderDoughnutChart(t *testing.T) {
	req := makeRequest(t, `{
		"type": "doughnut",
		"label": "Merge Rate",
		"description": "test",
		"lastupdate": 1000,
		"data": {
			"labels": ["Merged", "Closed"],
			"datasets": [{"data": [85, 15], "backgroundColor": ["#3E8635", "#A30000"]}]
		}
	}`)
	buf, err := Render(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertPNG(t, buf)
}

func TestRenderScatterChart(t *testing.T) {
	req := makeRequest(t, `{
		"type": "scatter",
		"label": "PR Size vs Time",
		"description": "test",
		"lastupdate": 1000,
		"data": {
			"datasets": [{
				"label": "PRs",
				"data": [{"x": 150, "y": 3.2}, {"x": 20, "y": 0.5}, {"x": 80, "y": 1.7}]
			}]
		}
	}`)
	buf, err := Render(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertPNG(t, buf)
}

func TestRenderKnotsChart(t *testing.T) {
	req := makeRequest(t, `{
		"type": "knots",
		"label": "Cycle Time Breakdown",
		"description": "test",
		"lastupdate": 1000,
		"data": {
			"stages": [
				{"label": "Pickup", "value": 12.5, "icon": "hourglass"},
				{"label": "Review", "value": 8.3},
				{"label": "Integration", "value": 2.1}
			]
		}
	}`)
	buf, err := Render(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertPNG(t, buf)
}

func TestStripHTML(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"plain text", "plain text"},
		{`PR Count <a href="http://help"><i class="icon">?</i></a>`, "PR Count ?"},
		{"<b>bold</b> and <em>emphasis</em>", "bold and emphasis"},
		{"no tags", "no tags"},
		{"", ""},
	}
	for _, tt := range tests {
		got := stripHTML(tt.in)
		if got != tt.want {
			t.Errorf("stripHTML(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRenderHTMLInLabels(t *testing.T) {
	req := makeRequest(t, `{
		"type": "bar",
		"label": "PR Count <a href=\"http://help\"><i>?</i></a>",
		"description": "test",
		"lastupdate": 1000,
		"data": {
			"labels": ["<b>Fast</b>", "Average"],
			"datasets": [{"label": "<em>PR count</em>", "data": [12, 45]}]
		}
	}`)
	buf, err := Render(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertPNG(t, buf)
}

func TestRenderSankeyChart(t *testing.T) {
	req := makeRequest(t, `{
		"type": "sankey",
		"label": "PR Lifecycle Flow",
		"description": "test",
		"lastupdate": 1000,
		"data": {
			"datasets": [{
				"label": "Flow",
				"data": [
					{"from": "opened", "to": "reviewed", "flow": 65.2, "count": 196},
					{"from": "opened", "to": "merged", "flow": 4.0, "count": 12},
					{"from": "reviewed", "to": "approved", "flow": 55.0, "count": 165},
					{"from": "approved", "to": "merged", "flow": 50.0, "count": 150}
				],
				"color": {"opened": "#0066CC", "reviewed": "#6753AC", "approved": "#228B22", "merged": "#2E8B57"},
				"labels": {"opened": "Opened", "reviewed": "Reviewed", "approved": "Approved", "merged": "Merged"},
				"priority": {"opened": 0, "reviewed": 1, "approved": 2, "merged": 3},
				"column": {"opened": 0, "reviewed": 1, "approved": 2, "merged": 3}
			}]
		}
	}`)
	buf, err := Render(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertPNG(t, buf)
}

func TestRenderUnsupportedType(t *testing.T) {
	req := makeRequest(t, `{
		"type": "radar",
		"label": "Flow",
		"description": "test",
		"lastupdate": 1000,
		"data": {}
	}`)
	_, err := Render(req)
	if err == nil {
		t.Fatal("expected error for unsupported chart type")
	}
}

func makeRequest(t *testing.T, payloadJSON string) Request {
	t.Helper()
	var req Request
	if err := json.Unmarshal([]byte(payloadJSON), &req); err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	return req
}

func assertPNG(t *testing.T, buf []byte) {
	t.Helper()
	if len(buf) == 0 {
		t.Fatal("expected non-empty output")
	}
	if len(buf) < 4 {
		t.Fatal("output too short for PNG")
	}
	for i, b := range pngHeader {
		if buf[i] != b {
			t.Fatalf("expected PNG header, got %x", buf[:4])
		}
	}
}
