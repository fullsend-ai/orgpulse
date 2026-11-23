package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadCoverageTrendBundle(t *testing.T) WorkflowBundle {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "workflows", "code-coverage", "coveragetrend.json"))
	if err != nil {
		t.Fatalf("read workflow bundle: %v", err)
	}
	var bundle WorkflowBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatalf("decode workflow bundle: %v", err)
	}
	return bundle
}

type coverageTrendOutput struct {
	Type       string   `json:"type"`
	Error      string   `json:"error"`
	Flags      []string `json:"flags"`
	Warning    string   `json:"warning"`
	LastUpdate int64    `json:"lastupdate"`
	Data       struct {
		Labels   []string `json:"labels"`
		Datasets []struct {
			Label string `json:"label"`
			Data  []any  `json:"data"`
		} `json:"datasets"`
	} `json:"data"`
}

func runCoverageTrendOutput(t *testing.T, bundle WorkflowBundle, input, results map[string]any) (map[string]any, coverageTrendOutput) {
	t.Helper()
	value, err := runJS(bundle.Workflow.OutputScript, map[string]any{"input": input, "results": results})
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
	var decoded coverageTrendOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	return output, decoded
}

func trendDataset(t *testing.T, output coverageTrendOutput, label string) []any {
	t.Helper()
	for _, dataset := range output.Data.Datasets {
		if dataset.Label == label {
			return dataset.Data
		}
	}
	t.Fatalf("missing dataset %q", label)
	return nil
}

func TestCoverageTrendBundle(t *testing.T) {
	bundle := loadCoverageTrendBundle(t)
	if bundle.FormatVersion != 1 || bundle.Workflow.Name != "coveragetrend" {
		t.Fatalf("unexpected bundle: version=%d name=%q", bundle.FormatVersion, bundle.Workflow.Name)
	}
	if len(bundle.Workflow.Queries) != 5 || len(bundle.JsonSchemas) != 1 {
		t.Fatalf("queries=%d schemas=%d", len(bundle.Workflow.Queries), len(bundle.JsonSchemas))
	}
	for _, query := range bundle.Workflow.Queries {
		if query.Label != "last_pipeline" && !strings.Contains(query.SQLText, ":repo_ids") {
			t.Errorf("query %q is not repository-scoped", query.Label)
		}
		if query.Label == "daily_coverage" && !strings.Contains(query.SQLText, "cc.lines_total > 0") {
			t.Error("daily coverage must include repositories with 0% coverage and nonzero lines")
		}
	}
	value, err := runJS(bundle.Workflow.InputScript, map[string]any{"input": map[string]any{
		"owner": []any{"metadata/snitch", "konflux-ci"},
		"name":  []any{"snitch", "coverport"},
		"from":  1789948800, "to": 1792540800,
	}})
	if err != nil {
		t.Fatalf("input script: %v", err)
	}
	input := value.output.(map[string]any)
	if input["requested_count"] != int64(2) || input["repo_ids"].([]any)[0] != "metadata:snitch/snitch" {
		t.Fatalf("input scope = %#v", input)
	}
}

func TestCoverageTrendSingleRepoShapeAndStaleFlag(t *testing.T) {
	bundle := loadCoverageTrendBundle(t)
	output, got := runCoverageTrendOutput(t, bundle, map[string]any{"requested_count": 1}, map[string]any{
		"daily_coverage": []map[string]any{
			{"repo_id": "org/repo", "date_val": "2026-05-05", "daily_coverage_avg": 76.0},
			{"repo_id": "org/repo", "date_val": "2026-05-15", "daily_coverage_avg": 71.0},
			{"repo_id": "org/repo", "date_val": "2026-05-15", "daily_coverage_avg": 71.0},
		},
		"daily_patch": []map[string]any{{"repo_id": "org/repo", "date_val": "2026-05-15", "daily_patch_avg": 90.0}},
		"daily_flags": []map[string]any{
			{"repo_id": "org/repo", "date_val": "2026-05-05", "flag_name": "unit-tests", "daily_flag_avg": 70.0, "total_lines": 1000},
			{"repo_id": "org/repo", "date_val": "2026-05-05", "flag_name": "e2e-tests", "daily_flag_avg": 40.0, "total_lines": 1000},
			{"repo_id": "org/repo", "date_val": "2026-05-15", "flag_name": "unit-tests", "daily_flag_avg": 72.0, "total_lines": 1000},
			{"repo_id": "org/repo", "date_val": "2026-05-15", "flag_name": "e2e-tests", "daily_flag_avg": nil, "total_lines": 0},
		},
		"coverage_goals": []map[string]any{{"repo_id": "org/repo", "patch_target": 85.0}},
		"last_pipeline":  []map[string]any{{"lastupdate": 1792540372000}},
	})
	if got.Type != "line" || got.LastUpdate != 1792540372000 {
		t.Fatalf("type=%q lastupdate=%d", got.Type, got.LastUpdate)
	}
	if len(got.Flags) != 2 || got.Flags[0] != "e2e-tests" || !strings.Contains(got.Warning, "e2e-tests") {
		t.Fatalf("flags=%v warning=%q", got.Flags, got.Warning)
	}
	if v := trendDataset(t, got, "Overall Coverage (%)"); len(v) != 2 || v[0] != float64(76) || v[1] != float64(71) {
		t.Fatalf("overall data=%v", v)
	}
	if v := trendDataset(t, got, "e2e-tests (%)"); len(v) != 2 || v[0] != float64(40) || v[1] != nil {
		t.Fatalf("flag data=%v", v)
	}
	if v := trendDataset(t, got, "Patch Coverage (%)"); len(v) != 2 || v[0] != nil || v[1] != float64(90) {
		t.Fatalf("patch data=%v", v)
	}
	if v := trendDataset(t, got, "Patch Goal (85%)"); len(v) != 2 || v[0] != float64(85) {
		t.Fatalf("goal data=%v", v)
	}
	if err := validateJSONSchema(bundle.JsonSchemas[0].JsonSchema, output); err != nil {
		t.Fatalf("validate schema: %v", err)
	}
}

func TestCoverageTrendMultiRepoCarryForwardAndMissingData(t *testing.T) {
	bundle := loadCoverageTrendBundle(t)
	_, got := runCoverageTrendOutput(t, bundle, map[string]any{"requested_count": 2}, map[string]any{
		"daily_coverage": []map[string]any{
			{"repo_id": "org/a", "date_val": "2026-05-05", "daily_coverage_avg": 70.0},
			{"repo_id": "org/b", "date_val": "2026-05-06", "daily_coverage_avg": 50.0},
			{"repo_id": "org/a", "date_val": "2026-05-07", "daily_coverage_avg": 75.0},
		},
		"daily_flags": []map[string]any{{"repo_id": "org/a", "date_val": "2026-05-05", "flag_name": "unit-tests", "daily_flag_avg": 60.0, "total_lines": 100}},
	})
	if len(got.Flags) != 0 || got.Warning != "" || len(got.Data.Datasets) != 3 {
		t.Fatalf("multi repo output=%#v", got)
	}
	if v := trendDataset(t, got, "Overall Coverage (%)"); len(v) != 3 || v[0] != float64(70) || v[1] != float64(60) || v[2] != float64(62.5) {
		t.Fatalf("carry-forward data=%v", v)
	}
	if v := trendDataset(t, got, "Patch Goal (80% — default)"); len(v) != 3 {
		t.Fatalf("default goal data=%v", v)
	}
	_, missing := runCoverageTrendOutput(t, bundle, map[string]any{"requested_count": 1}, map[string]any{})
	if missing.Error != "No daily coverage data found for the selected period" {
		t.Fatalf("missing data=%#v", missing)
	}
}
