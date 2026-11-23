package report

import (
	"encoding/json"
	"testing"
	"time"
)

func TestIsDue(t *testing.T) {
	days := func(n int) *int { return &n }
	str := func(s string) *string { return &s }

	tests := []struct {
		name         string
		lastRunAt    *string
		intervalDays *int
		want         bool
	}{
		{"nil interval", nil, nil, false},
		{"zero interval", nil, days(0), false},
		{"never run", nil, days(7), true},
		{"not yet due", str("2999-01-01T00:00:00Z"), days(7), false},
		{"overdue", str("2000-01-01T00:00:00Z"), days(7), true},
		{"unparseable date", str("garbage"), days(7), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDue(tt.lastRunAt, tt.intervalDays); got != tt.want {
				t.Errorf("isDue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluateRule(t *testing.T) {
	tests := []struct {
		name    string
		rule    string
		data    string
		want    bool
		wantErr bool
	}{
		{"scalar gt true", "data.atomicMetricsValue > 100", `{"atomicMetricsValue": 200}`, true, false},
		{"scalar gt false", "data.atomicMetricsValue > 100", `{"atomicMetricsValue": 50}`, false, false},
		{"object field", "data.atomicMetricsValue.cycleTime > 168", `{"atomicMetricsValue": {"cycleTime": 200}}`, true, false},
		{"object field false", "data.atomicMetricsValue.cycleTime > 168", `{"atomicMetricsValue": {"cycleTime": 100}}`, false, false},
		{"boolean and", "data.a > 1 && data.b < 10", `{"a": 5, "b": 3}`, true, false},
		{"boolean and false", "data.a > 1 && data.b < 10", `{"a": 5, "b": 20}`, false, false},
		{"invalid js", "data.foo >>>", `{"foo": 1}`, false, true},
		{"invalid json", "data.foo > 1", `{bad`, false, true},
		{"missing field returns false", "data.missing > 0", `{"other": 1}`, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evaluateRule(tt.rule, json.RawMessage(tt.data))
			if (err != nil) != tt.wantErr {
				t.Errorf("evaluateRule() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("evaluateRule() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluateRuleTimesOut(t *testing.T) {
	old := ruleTimeout
	ruleTimeout = 100 * time.Millisecond
	defer func() { ruleTimeout = old }()
	if _, err := evaluateRule("while(true){}", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected infinite loop to be interrupted")
	}
}
