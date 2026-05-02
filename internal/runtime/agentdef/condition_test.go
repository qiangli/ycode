package agentdef

import (
	"context"
	"testing"
)

func TestContainsCondition(t *testing.T) {
	cfg := ConditionConfig{Type: "output_contains", Source: "analyze", Value: "issues found"}
	cond, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}

	vars := map[string]string{"analyze": "3 issues found in the code"}
	ok, err := cond.Evaluate(context.Background(), vars)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected match")
	}

	vars["analyze"] = "code is clean"
	ok, err = cond.Evaluate(context.Background(), vars)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected no match")
	}
}

func TestContainsCondition_MissingSource(t *testing.T) {
	cfg := ConditionConfig{Type: "output_contains", Source: "missing", Value: "x"}
	cond, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}

	ok, err := cond.Evaluate(context.Background(), map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected false for missing source")
	}
}

func TestMatchesCondition(t *testing.T) {
	cfg := ConditionConfig{Type: "output_matches", Source: "check", Value: `\d+ errors?`}
	cond, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}

	vars := map[string]string{"check": "found 5 errors"}
	ok, err := cond.Evaluate(context.Background(), vars)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected regex match")
	}

	vars["check"] = "all good"
	ok, err = cond.Evaluate(context.Background(), vars)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected no regex match")
	}
}

func TestMatchesCondition_InvalidRegex(t *testing.T) {
	cfg := ConditionConfig{Type: "output_matches", Source: "x", Value: `[invalid`}
	_, err := cfg.Build()
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
}

func TestScoreAboveCondition(t *testing.T) {
	cfg := ConditionConfig{Type: "score_above", Source: "eval", Value: "0.8"}
	cond, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		output string
		want   bool
	}{
		{"0.95", true},
		{"0.8", false}, // not above, equal
		{"0.5", false},
		{"not a number", false},
	}

	for _, tt := range tests {
		vars := map[string]string{"eval": tt.output}
		ok, err := cond.Evaluate(context.Background(), vars)
		if err != nil {
			t.Fatal(err)
		}
		if ok != tt.want {
			t.Errorf("score %q: got %v, want %v", tt.output, ok, tt.want)
		}
	}
}

func TestAllOfCondition(t *testing.T) {
	cfg := ConditionConfig{
		Type: "all_of",
		Children: []ConditionConfig{
			{Type: "output_contains", Source: "a", Value: "pass"},
			{Type: "output_contains", Source: "b", Value: "pass"},
		},
	}
	cond, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}

	// Both pass.
	vars := map[string]string{"a": "pass", "b": "pass"}
	ok, err := cond.Evaluate(context.Background(), vars)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected all_of to pass")
	}

	// One fails.
	vars["b"] = "fail"
	ok, err = cond.Evaluate(context.Background(), vars)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected all_of to fail")
	}
}

func TestAnyOfCondition(t *testing.T) {
	cfg := ConditionConfig{
		Type: "any_of",
		Children: []ConditionConfig{
			{Type: "output_contains", Source: "a", Value: "pass"},
			{Type: "output_contains", Source: "b", Value: "pass"},
		},
	}
	cond, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}

	// One passes.
	vars := map[string]string{"a": "fail", "b": "pass"}
	ok, err := cond.Evaluate(context.Background(), vars)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected any_of to pass")
	}

	// None pass.
	vars = map[string]string{"a": "fail", "b": "fail"}
	ok, err = cond.Evaluate(context.Background(), vars)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected any_of to fail")
	}
}

func TestUnknownConditionType(t *testing.T) {
	cfg := ConditionConfig{Type: "unknown_type"}
	_, err := cfg.Build()
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestRouteConfig(t *testing.T) {
	route := RouteConfig{
		When:   ConditionConfig{Type: "output_contains", Source: "a", Value: "fix"},
		Target: "fixer-agent",
	}
	cond, err := route.When.Build()
	if err != nil {
		t.Fatal(err)
	}

	vars := map[string]string{"a": "needs fix"}
	ok, err := cond.Evaluate(context.Background(), vars)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected route to match")
	}
	if route.Target != "fixer-agent" {
		t.Fatalf("expected fixer-agent, got %s", route.Target)
	}
}
