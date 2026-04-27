package swarm

import (
	"testing"

	"github.com/qiangli/ycode/internal/runtime/agentdef"
)

func newTestRegistry(t *testing.T, defs ...*agentdef.AgentDefinition) *agentdef.Registry {
	t.Helper()
	reg := agentdef.NewRegistry()
	for _, d := range defs {
		// Compile trigger patterns.
		for i := range d.Triggers {
			if err := d.Triggers[i].Compile(); err != nil {
				t.Fatalf("compile trigger: %v", err)
			}
		}
		if err := reg.Register(d); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	return reg
}

func TestCheckMatchingTrigger(t *testing.T) {
	def := &agentdef.AgentDefinition{
		Name: "security-agent",
		Triggers: []agentdef.TriggerPattern{
			{Pattern: `(?i)security`},
		},
	}

	reg := newTestRegistry(t, def)
	tr := NewTriggerRegistry(reg)

	matches := tr.Check("we need a security review")
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	if matches[0].AgentDef.Name != "security-agent" {
		t.Fatalf("matched agent = %q", matches[0].AgentDef.Name)
	}
}

func TestCheckNonMatchingTrigger(t *testing.T) {
	def := &agentdef.AgentDefinition{
		Name: "security-agent",
		Triggers: []agentdef.TriggerPattern{
			{Pattern: `(?i)security`},
		},
	}

	reg := newTestRegistry(t, def)
	tr := NewTriggerRegistry(reg)

	matches := tr.Check("hello world")
	if len(matches) != 0 {
		t.Fatalf("matches = %d, want 0", len(matches))
	}
}

func TestCheckMultipleAgents(t *testing.T) {
	def1 := &agentdef.AgentDefinition{
		Name: "agent-a",
		Triggers: []agentdef.TriggerPattern{
			{Pattern: `error`},
		},
	}
	def2 := &agentdef.AgentDefinition{
		Name: "agent-b",
		Triggers: []agentdef.TriggerPattern{
			{Pattern: `warning`},
		},
	}

	reg := newTestRegistry(t, def1, def2)
	tr := NewTriggerRegistry(reg)

	// Only agent-a matches.
	matches := tr.Check("there is an error in the code")
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	if matches[0].AgentDef.Name != "agent-a" {
		t.Fatalf("matched = %q, want agent-a", matches[0].AgentDef.Name)
	}
}

func TestMaxPerTurnLimiting(t *testing.T) {
	def := &agentdef.AgentDefinition{
		Name: "limited-agent",
		Triggers: []agentdef.TriggerPattern{
			{Pattern: `trigger`, MaxPerTurn: 2},
		},
	}

	reg := newTestRegistry(t, def)
	tr := NewTriggerRegistry(reg)

	// First two triggers should match.
	matches := tr.Check("trigger text")
	if len(matches) != 1 {
		t.Fatalf("first check: matches = %d, want 1", len(matches))
	}
	tr.RecordTrigger("limited-agent")

	matches = tr.Check("trigger again")
	if len(matches) != 1 {
		t.Fatalf("second check: matches = %d, want 1", len(matches))
	}
	tr.RecordTrigger("limited-agent")

	// Third should be blocked.
	matches = tr.Check("trigger once more")
	if len(matches) != 0 {
		t.Fatalf("third check: matches = %d, want 0 (max per turn)", len(matches))
	}
}

func TestResetTurn(t *testing.T) {
	def := &agentdef.AgentDefinition{
		Name: "agent",
		Triggers: []agentdef.TriggerPattern{
			{Pattern: `keyword`, MaxPerTurn: 1},
		},
	}

	reg := newTestRegistry(t, def)
	tr := NewTriggerRegistry(reg)

	matches := tr.Check("keyword")
	if len(matches) != 1 {
		t.Fatalf("first: matches = %d, want 1", len(matches))
	}
	tr.RecordTrigger("agent")

	// Should be blocked.
	matches = tr.Check("keyword")
	if len(matches) != 0 {
		t.Fatalf("after limit: matches = %d, want 0", len(matches))
	}

	// Reset and check again.
	tr.ResetTurn()
	matches = tr.Check("keyword")
	if len(matches) != 1 {
		t.Fatalf("after reset: matches = %d, want 1", len(matches))
	}
}

func TestCheckNilRegistry(t *testing.T) {
	tr := NewTriggerRegistry(nil)
	matches := tr.Check("anything")
	if len(matches) != 0 {
		t.Fatalf("nil registry should return no matches, got %d", len(matches))
	}
}
