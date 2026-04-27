package agentdef

import (
	"testing"
)

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry()

	def := &AgentDefinition{Name: "test", Instruction: "hello"}
	if err := r.Register(def); err != nil {
		t.Fatal(err)
	}

	// Lookup existing.
	got, ok := r.Lookup("test")
	if !ok || got.Instruction != "hello" {
		t.Error("expected to find registered definition")
	}

	// Lookup missing.
	_, ok = r.Lookup("nonexistent")
	if ok {
		t.Error("expected not found for missing name")
	}

	// Duplicate registration.
	if err := r.Register(def); err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestRegistry_RegisterOrReplace(t *testing.T) {
	r := NewRegistry()

	def1 := &AgentDefinition{Name: "agent", Instruction: "v1"}
	r.RegisterOrReplace(def1)

	def2 := &AgentDefinition{Name: "agent", Instruction: "v2"}
	r.RegisterOrReplace(def2)

	got, ok := r.Lookup("agent")
	if !ok || got.Instruction != "v2" {
		t.Error("RegisterOrReplace should update existing definition")
	}
}

func TestRegistry_ListAndNames(t *testing.T) {
	r := NewRegistry()
	r.RegisterOrReplace(&AgentDefinition{Name: "charlie", Instruction: "c"})
	r.RegisterOrReplace(&AgentDefinition{Name: "alpha", Instruction: "a"})
	r.RegisterOrReplace(&AgentDefinition{Name: "bravo", Instruction: "b"})

	names := r.Names()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "alpha" || names[1] != "bravo" || names[2] != "charlie" {
		t.Errorf("names not sorted: %v", names)
	}

	list := r.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 defs, got %d", len(list))
	}
	if list[0].Name != "alpha" {
		t.Errorf("list not sorted: first = %q", list[0].Name)
	}
}

func TestRegistry_ResolveEmbeds(t *testing.T) {
	r := NewRegistry()

	parent := &AgentDefinition{
		Name:        "parent",
		Instruction: "parent instruction",
		Mode:        "plan",
		Tools:       []string{"tool_a"},
	}
	child := &AgentDefinition{
		Name:  "child",
		Embed: []string{"parent"},
		Tools: []string{"tool_b"},
	}

	r.RegisterOrReplace(parent)
	r.RegisterOrReplace(child)

	if err := r.ResolveEmbeds(); err != nil {
		t.Fatal(err)
	}

	resolved, _ := r.Lookup("child")
	if resolved.Instruction != "parent instruction" {
		t.Error("child should inherit parent instruction")
	}
	if resolved.Mode != "plan" {
		t.Error("child should inherit parent mode")
	}
	if len(resolved.Tools) != 2 {
		t.Errorf("child tools should be union: got %v", resolved.Tools)
	}
}

func TestRegistry_ResolveEmbeds_CycleDetection(t *testing.T) {
	r := NewRegistry()

	r.RegisterOrReplace(&AgentDefinition{
		Name:        "a",
		Instruction: "a",
		Embed:       []string{"b"},
	})
	r.RegisterOrReplace(&AgentDefinition{
		Name:        "b",
		Instruction: "b",
		Embed:       []string{"a"},
	})

	err := r.ResolveEmbeds()
	if err == nil {
		t.Error("expected cycle detection error")
	}
}

func TestRegistry_ResolveEmbeds_MissingParent(t *testing.T) {
	r := NewRegistry()
	r.RegisterOrReplace(&AgentDefinition{
		Name:  "orphan",
		Embed: []string{"nonexistent"},
	})

	err := r.ResolveEmbeds()
	if err == nil {
		t.Error("expected error for missing parent")
	}
}

func TestRegistry_FindTriggered(t *testing.T) {
	r := NewRegistry()

	deploy := &AgentDefinition{
		Name:        "deploy-agent",
		Instruction: "handle deploys",
		Triggers:    []TriggerPattern{{Pattern: "(?i)deploy"}},
	}
	for i := range deploy.Triggers {
		deploy.Triggers[i].Compile()
	}
	r.RegisterOrReplace(deploy)

	r.RegisterOrReplace(&AgentDefinition{
		Name:        "other-agent",
		Instruction: "something else",
	})

	matched := r.FindTriggered("let's deploy this")
	if len(matched) != 1 || matched[0].Name != "deploy-agent" {
		t.Errorf("FindTriggered: expected [deploy-agent], got %v", matched)
	}

	matched = r.FindTriggered("hello world")
	if len(matched) != 0 {
		t.Errorf("FindTriggered: expected empty for unrelated text, got %v", matched)
	}
}

func TestLoad_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Load from nonexistent dirs should return empty registry.
	reg, err := Load("/tmp/nonexistent-agentdef-test-1234")
	if err != nil {
		t.Fatal(err)
	}
	if reg.Len() != 0 {
		t.Errorf("expected empty registry, got %d", reg.Len())
	}
}
