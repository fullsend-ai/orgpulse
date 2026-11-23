package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadCoverageKeyMetricsBundle(t *testing.T) WorkflowBundle {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "workflows", "code-coverage", "coveragekeymetrics.json"))
	if err != nil {
		t.Fatalf("read workflow bundle: %v", err)
	}
	var bundle WorkflowBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatalf("decode workflow bundle: %v", err)
	}
	return bundle
}

type coverageKeyMetric struct {
	Label         string `json:"label"`
	Value         any    `json:"value"`
	Goal          any    `json:"goal"`
	Informational *bool  `json:"informational"`
	Explain       string `json:"explain"`
}

type coverageKeyMetricsOutput struct {
	Type       string `json:"type"`
	Label      string `json:"label"`
	LastUpdate int64  `json:"lastupdate"`
	Error      string `json:"error"`
	Data       struct {
		Metrics []coverageKeyMetric `json:"metrics"`
		Extra   *struct {
			Data struct {
				Bypassed []struct {
					ID    int     `json:"id"`
					Patch float64 `json:"patch"`
				} `json:"bypassed"`
			} `json:"data"`
		} `json:"extra"`
	} `json:"data"`
}

func runCoverageKeyMetricsOutput(t *testing.T, bundle WorkflowBundle, results map[string]any) (map[string]any, coverageKeyMetricsOutput) {
	t.Helper()
	value, err := runJS(bundle.Workflow.OutputScript, map[string]any{"results": results})
	if err != nil {
		t.Fatalf("run output script: %v", err)
	}
	output, ok := value.output.(map[string]any)
	if !ok {
		t.Fatalf("output type = %T", value.output)
	}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("encode output: %v", err)
	}
	var decoded coverageKeyMetricsOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	return output, decoded
}

func metricByLabel(t *testing.T, metrics []coverageKeyMetric, label string) coverageKeyMetric {
	t.Helper()
	for _, metric := range metrics {
		if metric.Label == label {
			return metric
		}
	}
	t.Fatalf("metric %q missing", label)
	return coverageKeyMetric{}
}

func TestCoverageKeyMetricsBundle(t *testing.T) {
	bundle := loadCoverageKeyMetricsBundle(t)
	if bundle.FormatVersion != 1 || bundle.Workflow.Name != "coveragekeymetrics" {
		t.Fatalf("unexpected workflow bundle: version=%d name=%q", bundle.FormatVersion, bundle.Workflow.Name)
	}
	if len(bundle.Workflow.Queries) != 5 || len(bundle.JsonSchemas) != 1 {
		t.Fatalf("queries=%d schemas=%d, want 5 and 1", len(bundle.Workflow.Queries), len(bundle.JsonSchemas))
	}
	for _, query := range bundle.Workflow.Queries {
		if !strings.Contains(query.SQLText, ":repo_ids") && query.Label != "last_pipeline" {
			t.Errorf("query %q does not scope repositories", query.Label)
		}
		if query.Label == "pr_patch" {
			if strings.Contains(query.SQLText, "MAX(patch_target)") || !strings.Contains(query.SQLText, "ROW_NUMBER() OVER (PARTITION BY repo_id ORDER BY updated_at DESC, connection_id DESC)") {
				t.Error("PR bypass threshold must use the latest repository configuration")
			}
		}
		if query.Label == "coverage_summary" && !strings.Contains(query.SQLText, "cc.lines_total > 0") {
			t.Error("coverage summary must include repositories with 0% coverage and nonzero lines")
		}
	}
}

func TestCoverageKeyMetricsInput(t *testing.T) {
	bundle := loadCoverageKeyMetricsBundle(t)
	value, err := runJS(bundle.Workflow.InputScript, map[string]any{"input": map[string]any{
		"owner": []any{"metadata/snitch", "metadata/snitch"},
		"name":  []any{"snitch", "snitch"},
		"from":  1789948800, "to": 1792540800, "blueprintid": "17",
	}})
	if err != nil {
		t.Fatalf("run input script: %v", err)
	}
	input := value.output.(map[string]any)
	ids := input["repo_ids"].([]any)
	if len(ids) != 1 || ids[0] != "metadata:snitch/snitch" {
		t.Fatalf("repo IDs = %#v", ids)
	}
	if input["has_blueprint"] != int64(1) {
		t.Fatalf("has_blueprint = %#v", input["has_blueprint"])
	}
	for _, bad := range []map[string]any{
		{"owner": []any{"org"}, "name": []any{}, "from": 1, "to": 2},
		{"owner": []any{"org"}, "name": []any{"repo"}, "from": 2, "to": 1},
		{"owner": []any{"org"}, "name": []any{"repo"}, "from": 1, "to": 2, "blueprintid": "bad"},
	} {
		if _, err := runJS(bundle.Workflow.InputScript, map[string]any{"input": bad}); err == nil {
			t.Fatalf("expected input error for %#v", bad)
		}
	}
}

func TestCoverageKeyMetricsSingleRepoLegacyShape(t *testing.T) {
	bundle := loadCoverageKeyMetricsBundle(t)
	output, got := runCoverageKeyMetricsOutput(t, bundle, map[string]any{
		"coverage_summary": []map[string]any{{"repo_id": "org/repo", "total_coverage": 88.0, "avg_coverage": 87.9, "earliest_coverage": 87.8, "latest_update": 1792540000000}},
		"patch_coverage":   []map[string]any{{"repo_id": "org/repo", "avg_patch_coverage": 93.1}},
		"coverage_goals":   []map[string]any{{"repo_id": "org/repo", "project_target_auto": true, "project_informational": false, "patch_target": 85, "patch_informational": false}},
		"pr_patch": []map[string]any{
			{"repo_id": "org/repo", "pr_number": 1, "avg_patch": 12.64, "effective_threshold": 85},
			{"repo_id": "org/repo", "pr_number": 2, "avg_patch": 90.0, "effective_threshold": 85},
		},
		"last_pipeline": []map[string]any{{"lastupdate": 1792540372000}},
	})
	if got.Type != "stats" || got.LastUpdate != 1792540372000 {
		t.Fatalf("type=%q lastupdate=%d", got.Type, got.LastUpdate)
	}
	if len(got.Data.Metrics) != 4 {
		t.Fatalf("metrics=%#v", got.Data.Metrics)
	}
	coverage := metricByLabel(t, got.Data.Metrics, "Avg Coverage (%)")
	if coverage.Value != float64(87.9) || coverage.Goal != "auto" {
		t.Fatalf("coverage metric=%#v", coverage)
	}
	patch := metricByLabel(t, got.Data.Metrics, "Avg Patch Coverage (%)")
	if patch.Value != float64(93.1) || patch.Goal != float64(85) {
		t.Fatalf("patch metric=%#v", patch)
	}
	bypass := metricByLabel(t, got.Data.Metrics, "Bypass Rate (%)")
	if bypass.Value != float64(50) || bypass.Explain != "1/2 PRs < 85%" {
		t.Fatalf("bypass metric=%#v", bypass)
	}
	if trend := metricByLabel(t, got.Data.Metrics, "Trend"); trend.Value != "Stable" {
		t.Fatalf("trend metric=%#v", trend)
	}
	if got.Data.Extra == nil || len(got.Data.Extra.Data.Bypassed) != 1 || got.Data.Extra.Data.Bypassed[0].Patch != 12.64 {
		t.Fatalf("bypassed PRs=%#v", got.Data.Extra)
	}
	if err := validateJSONSchema(bundle.JsonSchemas[0].JsonSchema, output); err != nil {
		t.Fatalf("validate response schema: %v", err)
	}
}

func TestCoverageKeyMetricsMultiRepoAndMissingData(t *testing.T) {
	bundle := loadCoverageKeyMetricsBundle(t)
	_, got := runCoverageKeyMetricsOutput(t, bundle, map[string]any{
		"coverage_summary": []map[string]any{
			{"repo_id": "org/a", "total_coverage": 80.5, "avg_coverage": 78.0, "earliest_coverage": 75.0},
			{"repo_id": "org/a", "total_coverage": 80.5, "avg_coverage": 78.0, "earliest_coverage": 75.0},
			{"repo_id": "org/b", "total_coverage": 45.2, "avg_coverage": 46.5, "earliest_coverage": 50.0},
		},
		"patch_coverage": []map[string]any{
			{"repo_id": "org/a", "avg_patch_coverage": 72.3},
			{"repo_id": "org/b", "avg_patch_coverage": 55.0},
		},
		"coverage_goals": []map[string]any{
			{"repo_id": "org/a", "project_target": 80, "patch_target": 80},
			{"repo_id": "org/b", "project_target": 90, "patch_target": 90},
		},
	})
	if metric := metricByLabel(t, got.Data.Metrics, "Avg Coverage (%)"); metric.Value != float64(62.3) || metric.Goal != nil {
		t.Fatalf("average coverage=%#v", metric)
	}
	if metric := metricByLabel(t, got.Data.Metrics, "Avg Patch Coverage (%)"); metric.Value != float64(63.7) || metric.Goal != nil {
		t.Fatalf("average patch=%#v", metric)
	}
	if metric := metricByLabel(t, got.Data.Metrics, "Trend"); metric.Value != "Stable" {
		t.Fatalf("trend=%#v", metric)
	}
	if len(got.Data.Metrics) != 3 || got.Data.Extra != nil {
		t.Fatalf("unexpected bypass metric or detail: %#v", got.Data)
	}

	_, noPatch := runCoverageKeyMetricsOutput(t, bundle, map[string]any{
		"coverage_summary": []map[string]any{{"repo_id": "org/a", "total_coverage": 80, "earliest_coverage": 60}},
	})
	if metric := metricByLabel(t, noPatch.Data.Metrics, "Avg Patch Coverage (%)"); metric.Value != "N/A" {
		t.Fatalf("missing patch=%#v", metric)
	}
	if metric := metricByLabel(t, noPatch.Data.Metrics, "Trend"); metric.Value != "+20%" {
		t.Fatalf("trend=%#v", metric)
	}

	_, noData := runCoverageKeyMetricsOutput(t, bundle, map[string]any{"coverage_summary": []map[string]any{}})
	if noData.Error != "No code coverage data found for the selected repositories" {
		t.Fatalf("no data response=%#v", noData)
	}
}
