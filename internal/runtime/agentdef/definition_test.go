package agentdef

import (
	"testing"
	"time"
)

func TestAgentDefinition_Validate(t *testing.T) {
	tests := []struct {
		name    string
		def     AgentDefinition
		wantErr bool
	}{
		{
			name: "valid minimal",
			def: AgentDefinition{
				Name:        "test-agent",
				Instruction: "You are a helpful assistant.",
			},
		},
		{
			name: "valid with all fields",
			def: AgentDefinition{
				APIVersion:  "v1",
				Name:        "full-agent",
				Display:     "Full Agent",
				Description: "A fully configured agent",
				Instruction: "Be helpful.",
				Mode:        "plan",
				Model:       "default/any",
				Tools:       []string{"read_file", "grep_search"},
				MaxIter:     20,
				MaxTime:     300,
				Flow:        FlowSequence,
			},
		},
		{
			name:    "missing name",
			def:     AgentDefinition{Instruction: "test"},
			wantErr: true,
		},
		{
			name:    "invalid name chars",
			def:     AgentDefinition{Name: "Invalid Name!", Instruction: "test"},
			wantErr: true,
		},
		{
			name:    "missing instruction without embed",
			def:     AgentDefinition{Name: "no-instruction"},
			wantErr: true,
		},
		{
			name: "missing instruction with embed is ok",
			def: AgentDefinition{
				Name:  "embed-agent",
				Embed: []string{"parent"},
			},
		},
		{
			name: "invalid mode",
			def: AgentDefinition{
				Name:        "bad-mode",
				Instruction: "test",
				Mode:        "invalid",
			},
			wantErr: true,
		},
		{
			name: "invalid flow type",
			def: AgentDefinition{
				Name:        "bad-flow",
				Instruction: "test",
				Flow:        "nonexistent",
			},
			wantErr: true,
		},
		{
			name: "invalid trigger pattern",
			def: AgentDefinition{
				Name:        "bad-trigger",
				Instruction: "test",
				Triggers:    []TriggerPattern{{Pattern: "[invalid"}},
			},
			wantErr: true,
		},
		{
			name: "valid trigger pattern",
			def: AgentDefinition{
				Name:        "trigger-agent",
				Instruction: "test",
				Triggers:    []TriggerPattern{{Pattern: "(?i)deploy|release", MaxPerTurn: 1}},
			},
		},
		{
			name: "negative max_iterations",
			def: AgentDefinition{
				Name:        "neg-iter",
				Instruction: "test",
				MaxIter:     -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.def.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAgentDefinition_EffectiveDefaults(t *testing.T) {
	d := &AgentDefinition{Name: "test"}

	if got := d.EffectiveMode(); got != "build" {
		t.Errorf("EffectiveMode() = %q, want %q", got, "build")
	}
	if got := d.EffectiveMaxIter(); got != 15 {
		t.Errorf("EffectiveMaxIter() = %d, want 15", got)
	}
	if got := d.EffectiveTimeout(); got != 0 {
		t.Errorf("EffectiveTimeout() = %v, want 0", got)
	}

	d.Mode = "plan"
	d.MaxIter = 30
	d.MaxTime = 60

	if got := d.EffectiveMode(); got != "plan" {
		t.Errorf("EffectiveMode() = %q, want %q", got, "plan")
	}
	if got := d.EffectiveMaxIter(); got != 30 {
		t.Errorf("EffectiveMaxIter() = %d, want 30", got)
	}
	if got := d.EffectiveTimeout(); got != 60*time.Second {
		t.Errorf("EffectiveTimeout() = %v, want 60s", got)
	}
}

func TestAgentDefinition_MergeFrom(t *testing.T) {
	parent := &AgentDefinition{
		Name:        "parent",
		Instruction: "Parent instruction",
		Mode:        "plan",
		Model:       "parent-model",
		Tools:       []string{"tool_a", "tool_b"},
		Environment: map[string]string{"KEY": "parent-val"},
		MaxIter:     20,
	}

	child := &AgentDefinition{
		Name:        "child",
		Instruction: "Child instruction", // should NOT be overridden
		Tools:       []string{"tool_c"},
		Environment: map[string]string{"CHILD_KEY": "child-val"},
	}

	child.MergeFrom(parent)

	if child.Instruction != "Child instruction" {
		t.Error("MergeFrom should not override non-empty fields")
	}
	if child.Mode != "plan" {
		t.Error("MergeFrom should fill empty Mode from parent")
	}
	if child.Model != "parent-model" {
		t.Error("MergeFrom should fill empty Model from parent")
	}
	if child.MaxIter != 20 {
		t.Error("MergeFrom should fill zero MaxIter from parent")
	}

	// Tools should be unioned.
	if len(child.Tools) != 3 {
		t.Errorf("MergeFrom tools union: got %d, want 3", len(child.Tools))
	}

	// Environment should be unioned (child KEY not overridden).
	if child.Environment["KEY"] != "parent-val" {
		t.Error("MergeFrom should add parent env vars")
	}
	if child.Environment["CHILD_KEY"] != "child-val" {
		t.Error("MergeFrom should preserve child env vars")
	}
}

func TestTriggerPattern_Match(t *testing.T) {
	tp := TriggerPattern{Pattern: "(?i)deploy|release"}
	if err := tp.Compile(); err != nil {
		t.Fatal(err)
	}

	if !tp.Match("Please deploy this") {
		t.Error("should match 'deploy'")
	}
	if !tp.Match("RELEASE v1.0") {
		t.Error("should match 'RELEASE' case-insensitive")
	}
	if tp.Match("just a normal message") {
		t.Error("should not match unrelated text")
	}
}

func TestAgentDefinition_MatchesTrigger(t *testing.T) {
	d := &AgentDefinition{
		Name:        "deploy-agent",
		Instruction: "handle deployments",
		Triggers: []TriggerPattern{
			{Pattern: "(?i)deploy"},
			{Pattern: "(?i)release"},
		},
	}
	// Compile triggers (normally done by Validate).
	for i := range d.Triggers {
		if err := d.Triggers[i].Compile(); err != nil {
			t.Fatal(err)
		}
	}

	if !d.MatchesTrigger("Let's deploy this") {
		t.Error("should match 'deploy'")
	}
	if d.MatchesTrigger("hello world") {
		t.Error("should not match unrelated text")
	}
}

func TestAgentDefinition_DisplayName(t *testing.T) {
	d := &AgentDefinition{Name: "test-agent"}
	if got := d.DisplayName(); got != "test-agent" {
		t.Errorf("DisplayName() without display = %q, want %q", got, "test-agent")
	}
	d.Display = "Test Agent"
	if got := d.DisplayName(); got != "Test Agent" {
		t.Errorf("DisplayName() with display = %q, want %q", got, "Test Agent")
	}
}
