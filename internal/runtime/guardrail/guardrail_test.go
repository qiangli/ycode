package guardrail

import (
	"context"
	"testing"
)

func TestRegexGuardrail_Match(t *testing.T) {
	g, err := NewRegexGuardrail("pii", `(?i)(password|secret|api.key)`, ActionReject)
	if err != nil {
		t.Fatal(err)
	}

	result, err := g.Check(context.Background(), "", "The password is abc123")
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected guardrail to fail for PII match")
	}
	if result.Action != ActionReject {
		t.Fatalf("expected reject, got %s", result.Action)
	}
	if result.Feedback == "" {
		t.Fatal("expected feedback message")
	}
}

func TestRegexGuardrail_NoMatch(t *testing.T) {
	g, err := NewRegexGuardrail("pii", `(?i)(password|secret)`, ActionReject)
	if err != nil {
		t.Fatal(err)
	}

	result, err := g.Check(context.Background(), "", "This is a safe output")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("expected guardrail to pass for clean output")
	}
}

func TestChain_AllPass(t *testing.T) {
	g1, _ := NewRegexGuardrail("g1", `FORBIDDEN`, ActionReject)
	g2, _ := NewRegexGuardrail("g2", `BLOCKED`, ActionReject)

	chain := NewChain([]Guardrail{g1, g2}, 2)
	result, err := chain.Run(context.Background(), "", "clean output")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("expected chain to pass")
	}
}

func TestChain_FirstFails(t *testing.T) {
	g1, _ := NewRegexGuardrail("g1", `bad`, ActionReject)
	g2, _ := NewRegexGuardrail("g2", `NEVER_MATCH_THIS_UNIQUE_STRING`, ActionReject)

	chain := NewChain([]Guardrail{g1, g2}, 2)
	result, err := chain.Run(context.Background(), "", "this is bad")
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected chain to fail")
	}
	if result.Action != ActionReject {
		t.Fatalf("expected reject, got %s", result.Action)
	}
}

func TestChain_MaxRetry(t *testing.T) {
	chain := NewChain(nil, 0) // 0 should default to 2
	if chain.MaxRetry() != 2 {
		t.Fatalf("expected maxRetry 2, got %d", chain.MaxRetry())
	}
}

type fakeSchemaValidator struct {
	valid  bool
	errors []string
}

func (f *fakeSchemaValidator) ValidateOutput(_ string) (bool, []string) {
	return f.valid, f.errors
}

func TestSchemaGuardrail_Valid(t *testing.T) {
	g := NewSchemaGuardrail(&fakeSchemaValidator{valid: true})
	result, err := g.Check(context.Background(), "", `{"ok": true}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("expected pass")
	}
}

func TestSchemaGuardrail_Invalid(t *testing.T) {
	g := NewSchemaGuardrail(&fakeSchemaValidator{valid: false, errors: []string{"missing field"}})
	result, err := g.Check(context.Background(), "", `{"x": 1}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail")
	}
	if result.Action != ActionRetry {
		t.Fatalf("expected retry, got %s", result.Action)
	}
}

func TestBuildFromConfigs_Empty(t *testing.T) {
	chain, err := BuildFromConfigs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if chain != nil {
		t.Fatal("expected nil chain for empty configs")
	}
}

func TestBuildFromConfigs_Regex(t *testing.T) {
	configs := []Config{
		{Type: "regex", Pattern: `(?i)secret`, Action: ActionReject},
	}
	chain, err := BuildFromConfigs(configs)
	if err != nil {
		t.Fatal(err)
	}
	if chain == nil {
		t.Fatal("expected non-nil chain")
	}

	result, err := chain.Run(context.Background(), "", "the secret is out")
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected chain to fail")
	}
}

func TestBuildFromConfigs_InvalidPattern(t *testing.T) {
	configs := []Config{
		{Type: "regex", Pattern: `[invalid`},
	}
	_, err := BuildFromConfigs(configs)
	if err == nil {
		t.Fatal("expected error for invalid pattern")
	}
}

func TestBuildFromConfigs_UnknownType(t *testing.T) {
	configs := []Config{
		{Type: "unknown"},
	}
	_, err := BuildFromConfigs(configs)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestConfig_EffectiveAction(t *testing.T) {
	c := Config{}
	if c.EffectiveAction() != ActionRetry {
		t.Fatalf("expected retry default, got %s", c.EffectiveAction())
	}

	c.Action = ActionReject
	if c.EffectiveAction() != ActionReject {
		t.Fatalf("expected reject, got %s", c.EffectiveAction())
	}
}
