package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadCoverageLineBreakdownBundle(t *testing.T) WorkflowBundle {
	t.Helper()

	path := filepath.Join("..", "..", "workflows", "code-coverage", "coveragelinebreakdown.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read workflow bundle: %v", err)
	}

	var bundle WorkflowBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatalf("decode workflow bundle: %v", err)
	}
	return bundle
}

func runCoverageLineBreakdownOutput(t *testing.T, bundle WorkflowBundle, input map[string]any, rows []map[string]any) map[string]any {
	t.Helper()

	result, err := runJS(bundle.Workflow.OutputScript, map[string]any{
		"input":   input,
		"results": map[string]any{"line_counts": rows},
	})
	if err != nil {
		t.Fatalf("run output script: %v", err)
	}
	output, ok := result.output.(map[string]any)
	if !ok {
		t.Fatalf("output type = %T, want map[string]any", result.output)
	}
	return output
}

func decodeCoverageLineBreakdownOutput(t *testing.T, output map[string]any) struct {
	Type  string `json:"type"`
	Label string `json:"label"`
	Data  struct {
		Datasets []struct {
			Data []float64 `json:"data"`
		} `json:"datasets"`
	} `json:"data"`
} {
	t.Helper()

	var decoded struct {
		Type  string `json:"type"`
		Label string `json:"label"`
		Data  struct {
			Datasets []struct {
				Data []float64 `json:"data"`
			} `json:"datasets"`
		} `json:"data"`
	}
	raw, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("encode output: %v", err)
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	return decoded
}

func TestCoverageLineBreakdownWorkflowBundle(t *testing.T) {
	bundle := loadCoverageLineBreakdownBundle(t)

	if bundle.FormatVersion != 1 {
		t.Fatalf("format version = %d, want 1", bundle.FormatVersion)
	}
	if bundle.Workflow.Name != "coveragelinebreakdown" {
		t.Fatalf("workflow name = %q", bundle.Workflow.Name)
	}
	if len(bundle.Workflow.Queries) != 1 {
		t.Fatalf("query count = %d, want 1", len(bundle.Workflow.Queries))
	}
	if len(bundle.Workflow.Tests) != 3 {
		t.Fatalf("workflow test count = %d, want 3", len(bundle.Workflow.Tests))
	}
	if len(bundle.JsonSchemas) != 1 || bundle.JsonSchemas[0].ID != 1 {
		t.Fatal("expected portable output schema id 1")
	}
	for _, tc := range bundle.Workflow.Tests[:2] {
		if tc.SchemaID != 1 {
			t.Fatalf("test %q schema id = %d, want 1", tc.Name, tc.SchemaID)
		}
	}
}

func TestCoverageLineBreakdownInputTransform(t *testing.T) {
	bundle := loadCoverageLineBreakdownBundle(t)
	result, err := runJS(bundle.Workflow.InputScript, map[string]any{
		"input": map[string]any{
			"owner": []any{"metadata/snitch", "metadata/snitch"},
			"name":  []any{"snitch", "snitch"},
			"flag":  " unit-tests ",
		},
	})
	if err != nil {
		t.Fatalf("run input script: %v", err)
	}

	raw, err := json.Marshal(result.output)
	if err != nil {
		t.Fatalf("encode transformed input: %v", err)
	}
	var input struct {
		RepoIDs []string `json:"repo_ids"`
		Flag    string   `json:"flag"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatalf("decode transformed input: %v", err)
	}
	if len(input.RepoIDs) != 1 || input.RepoIDs[0] != "metadata:snitch/snitch" {
		t.Fatalf("repo ids = %#v", input.RepoIDs)
	}
	if input.Flag != "unit-tests" {
		t.Fatalf("flag = %q", input.Flag)
	}
}

func TestCoverageLineBreakdownInputRejectsMismatchedRepositories(t *testing.T) {
	bundle := loadCoverageLineBreakdownBundle(t)
	_, err := runJS(bundle.Workflow.InputScript, map[string]any{
		"input": map[string]any{
			"owner": []any{"org", "other-org"},
			"name":  []any{"repo"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "same number of values") {
		t.Fatalf("error = %v, want mismatched repository error", err)
	}
}

func TestCoverageLineBreakdownOutputAggregatesAndDeduplicates(t *testing.T) {
	bundle := loadCoverageLineBreakdownBundle(t)
	output := runCoverageLineBreakdownOutput(t, bundle, map[string]any{"flag": ""}, []map[string]any{
		{"repo_id": "org/repo-a", "hits": 500, "partials": 50, "misses": 100},
		{"repo_id": "org/repo-a", "hits": 500, "partials": 50, "misses": 100},
		{"repo_id": "org/repo-b", "hits": 300, "partials": 25, "misses": 200},
	})
	decoded := decodeCoverageLineBreakdownOutput(t, output)

	if decoded.Type != "doughnut" || decoded.Label != "Line Coverage Breakdown" {
		t.Fatalf("unexpected response: type=%q label=%q", decoded.Type, decoded.Label)
	}
	if len(decoded.Data.Datasets) != 1 {
		t.Fatalf("dataset count = %d, want 1", len(decoded.Data.Datasets))
	}
	want := []float64{800, 75, 300}
	got := decoded.Data.Datasets[0].Data
	if len(got) != len(want) {
		t.Fatalf("values = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("values = %#v, want %#v", got, want)
		}
	}
	if err := validateJSONSchema(bundle.JsonSchemas[0].JsonSchema, output); err != nil {
		t.Fatalf("validate output schema: %v", err)
	}
}

func TestCoverageLineBreakdownOutputSupportsFlagsAndNoData(t *testing.T) {
	bundle := loadCoverageLineBreakdownBundle(t)
	flagOutput := runCoverageLineBreakdownOutput(t, bundle, map[string]any{"flag": "unit-tests"}, []map[string]any{
		{"repo_id": "org/repo", "hits": 7318, "partials": 765, "misses": 2534},
	})
	flagDecoded := decodeCoverageLineBreakdownOutput(t, flagOutput)
	if flagDecoded.Label != "Line Coverage Breakdown — unit-tests" {
		t.Fatalf("flag label = %q", flagDecoded.Label)
	}

	missedOutput := runCoverageLineBreakdownOutput(t, bundle, map[string]any{"flag": "unit-tests"}, []map[string]any{
		{"repo_id": "org/repo", "hits": 0, "partials": 0, "misses": 100},
	})
	missed := decodeCoverageLineBreakdownOutput(t, missedOutput).Data.Datasets[0].Data
	if len(missed) != 3 || missed[0] != 0 || missed[1] != 0 || missed[2] != 100 {
		t.Fatalf("0%%-covered flag reported incorrectly: %v", missed)
	}

	emptyOutput := runCoverageLineBreakdownOutput(t, bundle, map[string]any{"flag": "e2e-tests"}, nil)
	if emptyOutput["error"] != "No line coverage data for e2e-tests" {
		t.Fatalf("empty response = %#v", emptyOutput)
	}
}

func TestCoverageLineBreakdownSQLParameters(t *testing.T) {
	bundle := loadCoverageLineBreakdownBundle(t)
	query := bundle.Workflow.Queries[0].SQLText
	if strings.Contains(query, "cov.hits > 0") || strings.Contains(query, "cc.overall_coverage > 0") ||
		!strings.Contains(query, "COALESCE(cov.misses, 0)") || !strings.Contains(query, "COALESCE(cc.misses, 0)") {
		t.Fatal("line-breakdown SQL must retain zero-coverage flags and repositories with missed lines")
	}
	resolved, args := resolveNamedParams(query, map[string]any{
		"repo_ids": []any{"org/repo-a", "org/repo-b"},
		"flag":     "unit-tests",
	})

	if strings.Contains(resolved, ":repo_ids") || strings.Contains(resolved, ":flag") {
		t.Fatalf("unresolved named parameter in SQL: %s", resolved)
	}
	if len(args) != 7 {
		t.Fatalf("SQL argument count = %d, want 7", len(args))
	}
}
