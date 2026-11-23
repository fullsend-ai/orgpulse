package devlake

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestSpreadCron_Deterministic(t *testing.T) {
	a := spreadCron("0 0 * * *", "lfmbot-team1-github-myorg")
	b := spreadCron("0 0 * * *", "lfmbot-team1-github-myorg")
	if a != b {
		t.Errorf("expected deterministic output, got %q and %q", a, b)
	}
}

func TestSpreadCron_DifferentSeeds(t *testing.T) {
	a := spreadCron("0 0 * * *", "lfmbot-team1-github-orgA")
	b := spreadCron("0 0 * * *", "lfmbot-team2-github-orgB")
	if a == b {
		t.Errorf("different seeds produced the same cron: %q", a)
	}
}

func TestSpreadCron_PreservesTrailingFields(t *testing.T) {
	result := spreadCron("0 0 * * 1-5", "test-seed")
	fields := strings.Fields(result)
	if len(fields) != 5 {
		t.Fatalf("expected 5 fields, got %d: %q", len(fields), result)
	}
	if fields[2] != "*" || fields[3] != "*" || fields[4] != "1-5" {
		t.Errorf("trailing fields changed: %q", result)
	}
}

func TestSpreadCron_ValidRange(t *testing.T) {
	for i := range 100 {
		result := spreadCron("0 0 * * *", "seed-"+strconv.Itoa(i))
		fields := strings.Fields(result)
		minute, _ := strconv.Atoi(fields[0])
		hour, _ := strconv.Atoi(fields[1])
		if minute < 0 || minute > 59 {
			t.Errorf("seed %d: minute %d out of range", i, minute)
		}
		if hour < 0 || hour > 23 {
			t.Errorf("seed %d: hour %d out of range", i, hour)
		}
	}
}

func TestSpreadCron_Distribution(t *testing.T) {
	hours := make(map[int]int)
	for i := range 240 {
		result := spreadCron("0 0 * * *", "blueprint-"+strconv.Itoa(i))
		fields := strings.Fields(result)
		hour, _ := strconv.Atoi(fields[1])
		hours[hour]++
	}
	if len(hours) < 12 {
		t.Errorf("expected at least 12 distinct hours across 240 seeds, got %d", len(hours))
	}
}

func TestMatchScope_GitHub_ExactMatch(t *testing.T) {
	data := []json.RawMessage{
		json.RawMessage(`{"githubId":100,"fullName":"myorg/repo-a"}`),
		json.RawMessage(`{"githubId":200,"fullName":"myorg/repo-b"}`),
	}
	got := matchScope("github", data, "myorg/repo-b")
	if got == nil {
		t.Fatal("expected match for myorg/repo-b")
	}
	var m map[string]any
	_ = json.Unmarshal(got, &m)
	if m["fullName"] != "myorg/repo-b" {
		t.Errorf("got fullName=%v, want myorg/repo-b", m["fullName"])
	}
}

func TestMatchScope_GitHub_NoMatch(t *testing.T) {
	data := []json.RawMessage{
		json.RawMessage(`{"githubId":100,"fullName":"myorg/repo-a"}`),
	}
	got := matchScope("github", data, "myorg/repo-MISSING")
	if got != nil {
		t.Errorf("expected nil for missing repo, got %s", got)
	}
}

func TestMatchScope_GitLab_UsesPathWithNamespace(t *testing.T) {
	data := []json.RawMessage{
		json.RawMessage(`{"gitlabId":10,"pathWithNamespace":"group/subgroup/project"}`),
	}
	got := matchScope("gitlab", data, "group/subgroup/project")
	if got == nil {
		t.Fatal("expected match for gitlab nested path")
	}
}

func TestMatchScope_CodecovUsesRequiredID(t *testing.T) {
	data := []json.RawMessage{
		json.RawMessage(`{"codecovId":"konflux-ci/other","fullName":"konflux-ci/konflux-ui"}`),
		json.RawMessage(`{"codecovId":"konflux-ci/konflux-ui"}`),
	}
	got := matchScope("codecov", data, "konflux-ci/konflux-ui")
	if got == nil || string(got) != string(data[1]) {
		t.Fatalf("matched scope = %s, want Codecov ID without fullName", got)
	}
}

func TestMatchScope_EmptyResults(t *testing.T) {
	got := matchScope("github", nil, "myorg/repo-a")
	if got != nil {
		t.Errorf("expected nil for empty results")
	}
}

func TestMatchScope_CaseSensitive(t *testing.T) {
	data := []json.RawMessage{
		json.RawMessage(`{"githubId":100,"fullName":"MyOrg/Repo-A"}`),
	}
	got := matchScope("github", data, "myorg/repo-a")
	if got != nil {
		t.Errorf("matchScope should be case-sensitive, but matched %s", got)
	}
}

func TestScopeIDFromRaw_GitHub(t *testing.T) {
	raw := json.RawMessage(`{"githubId":42,"fullName":"myorg/repo"}`)
	id := scopeIDFromRaw("github", raw)
	if id != "42" {
		t.Errorf("got %q, want 42", id)
	}
}

func TestScopeIDFromRaw_GitLab(t *testing.T) {
	raw := json.RawMessage(`{"gitlabId":99,"pathWithNamespace":"group/project"}`)
	id := scopeIDFromRaw("gitlab", raw)
	if id != "99" {
		t.Errorf("got %q, want 99", id)
	}
}

func TestScopeIDFromRaw_Jira(t *testing.T) {
	raw := json.RawMessage(`{"boardId":7,"name":"My Board"}`)
	id := scopeIDFromRaw("jira", raw)
	if id != "7" {
		t.Errorf("got %q, want 7", id)
	}
}

func TestScopeIDFromRaw_InvalidJSON(t *testing.T) {
	raw := json.RawMessage(`{broken`)
	id := scopeIDFromRaw("github", raw)
	if id != "" {
		t.Errorf("expected empty string for invalid JSON, got %q", id)
	}
}

func TestScopeIDFromRaw_MissingField(t *testing.T) {
	raw := json.RawMessage(`{"fullName":"myorg/repo"}`)
	id := scopeIDFromRaw("github", raw)
	if id != "" {
		t.Errorf("expected empty string for missing githubId, got %q", id)
	}
}
