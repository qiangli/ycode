package agentdef

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse_YcodeNativeFormat(t *testing.T) {
	yaml := `
apiVersion: v1
name: my-agent
description: A test agent
instruction: You are a helpful assistant.
mode: explore
model: claude-sonnet-4-6
tools:
  - read_file
  - grep_search
max_iterations: 25
`
	defs, err := Parse([]byte(yaml), "test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(defs))
	}
	d := defs[0]
	if d.Name != "my-agent" {
		t.Errorf("name = %q, want %q", d.Name, "my-agent")
	}
	if d.Mode != "explore" {
		t.Errorf("mode = %q, want %q", d.Mode, "explore")
	}
	if d.MaxIter != 25 {
		t.Errorf("max_iterations = %d, want 25", d.MaxIter)
	}
	if len(d.Tools) != 2 {
		t.Errorf("tools count = %d, want 2", len(d.Tools))
	}
}

func TestParse_AISwarmFormat(t *testing.T) {
	yaml := `###
pack: "mypack"
log_level: "info"
agents:
  - name: "greeter"
    display: "Greeter Bot"
    description: "Greets people"
    model: "default/any"
    instruction: |
      You are a friendly greeter.
    functions:
      - "mypack:hello"
    max_turns: 10

###
kit: "mypack"
type: "func"
tools:
  - name: "hello"
    description: "Say hello"

###
set: "default"
models:
  any:
    model: "gpt-4o"
    provider: "openai"
`
	defs, err := Parse([]byte(yaml), "swarm.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 agent definition (tools/models skipped), got %d", len(defs))
	}
	d := defs[0]
	if d.Name != "greeter" {
		t.Errorf("name = %q, want %q", d.Name, "greeter")
	}
	if d.Display != "Greeter Bot" {
		t.Errorf("display = %q, want %q", d.Display, "Greeter Bot")
	}
	if d.MaxIter != 10 {
		t.Errorf("max_iterations = %d, want 10 (from max_turns)", d.MaxIter)
	}
	if len(d.Tools) != 1 || d.Tools[0] != "mypack:hello" {
		t.Errorf("tools = %v, want [mypack:hello]", d.Tools)
	}
	if d.Environment["pack"] != "mypack" {
		t.Errorf("environment[pack] = %q, want %q", d.Environment["pack"], "mypack")
	}
}

func TestParse_MultiDocStandard(t *testing.T) {
	yaml := `---
apiVersion: v1
name: agent-a
instruction: Do A
---
apiVersion: v1
name: agent-b
instruction: Do B
`
	defs, err := Parse([]byte(yaml), "multi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 2 {
		t.Fatalf("expected 2 definitions, got %d", len(defs))
	}
	if defs[0].Name != "agent-a" {
		t.Errorf("first agent name = %q, want %q", defs[0].Name, "agent-a")
	}
	if defs[1].Name != "agent-b" {
		t.Errorf("second agent name = %q, want %q", defs[1].Name, "agent-b")
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	_, err := Parse([]byte("{{invalid yaml"), "bad.yaml")
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestParse_ValidationFailure(t *testing.T) {
	yaml := `
name: "INVALID NAME!"
instruction: test
`
	_, err := Parse([]byte(yaml), "invalid.yaml")
	if err == nil {
		t.Error("expected validation error for invalid name")
	}
}

func TestLoadDir_Nonexistent(t *testing.T) {
	defs, err := LoadDir("/nonexistent/path/12345")
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 0 {
		t.Errorf("expected empty result for nonexistent dir, got %d", len(defs))
	}
}

func TestLoadDir_WithFiles(t *testing.T) {
	dir := t.TempDir()

	// Write a valid YAML file.
	content := `
apiVersion: v1
name: file-agent
instruction: Hello from file
`
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	// Write a non-YAML file (should be ignored).
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	defs, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(defs))
	}
	if defs[0].Name != "file-agent" {
		t.Errorf("name = %q, want %q", defs[0].Name, "file-agent")
	}
}

func TestLoadPaths_Override(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// dir1 has agent with instruction A.
	if err := os.WriteFile(filepath.Join(dir1, "shared.yaml"), []byte(`
name: shared
instruction: From dir1
`), 0644); err != nil {
		t.Fatal(err)
	}

	// dir2 has same agent name with instruction B.
	if err := os.WriteFile(filepath.Join(dir2, "shared.yaml"), []byte(`
name: shared
instruction: From dir2
`), 0644); err != nil {
		t.Fatal(err)
	}

	defs, err := LoadPaths(dir1, dir2)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 definition (overridden), got %d", len(defs))
	}
	if defs[0].Instruction != "From dir2" {
		t.Errorf("instruction = %q, want %q (from later dir)", defs[0].Instruction, "From dir2")
	}
}

func TestParse_AdvicesConfig(t *testing.T) {
	yaml := `
name: advised-agent
instruction: test
advices:
  before:
    - validate-input
  around:
    - timeout
  after:
    - format-output
`
	defs, err := Parse([]byte(yaml), "advised.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatal("expected 1 definition")
	}
	d := defs[0]
	if d.Advices == nil {
		t.Fatal("advices should not be nil")
	}
	if len(d.Advices.Before) != 1 || d.Advices.Before[0] != "validate-input" {
		t.Errorf("before = %v, want [validate-input]", d.Advices.Before)
	}
	if len(d.Advices.Around) != 1 || d.Advices.Around[0] != "timeout" {
		t.Errorf("around = %v, want [timeout]", d.Advices.Around)
	}
	if len(d.Advices.After) != 1 || d.Advices.After[0] != "format-output" {
		t.Errorf("after = %v, want [format-output]", d.Advices.After)
	}
}
