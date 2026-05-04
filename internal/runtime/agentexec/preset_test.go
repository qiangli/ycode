package agentexec

import (
	"testing"
)

func TestBuiltinPresets(t *testing.T) {
	r := NewPresetRegistry()

	expected := []string{"claude", "codex", "gemini", "aider", "cursor", "opencode"}
	for _, name := range expected {
		p, ok := r.Get(name)
		if !ok {
			t.Errorf("missing builtin preset %q", name)
			continue
		}
		if p.Command == "" {
			t.Errorf("preset %q has empty command", name)
		}
	}
}

func TestPresetRegistry_CustomPreset(t *testing.T) {
	r := NewPresetRegistry()
	r.Register(&AgentPreset{
		Name:    "custom",
		Command: "my-agent",
	})

	p, ok := r.Get("custom")
	if !ok {
		t.Fatal("custom preset not found")
	}
	if p.Command != "my-agent" {
		t.Errorf("command = %q, want my-agent", p.Command)
	}
}

func TestPresetRegistry_List(t *testing.T) {
	r := NewPresetRegistry()
	names := r.List()
	if len(names) < 6 {
		t.Errorf("expected at least 6 presets, got %d", len(names))
	}
}

func TestAgentPreset_BuildCommand_Claude(t *testing.T) {
	r := NewPresetRegistry()
	p, _ := r.Get("claude")

	args := p.BuildCommand(ExecOptions{
		Prompt:          "Fix the bug in main.go",
		Model:           "sonnet",
		SessionID:       "session-123",
		SystemPrompt:    "You are working on ycode.",
		SkipPermissions: true,
	})

	assertContains(t, args, "claude")
	assertContains(t, args, "-p")
	assertContains(t, args, "Fix the bug in main.go")
	assertContains(t, args, "--model")
	assertContains(t, args, "sonnet")
	assertContains(t, args, "--resume")
	assertContains(t, args, "session-123")
	assertContains(t, args, "--append-system-prompt")
	assertContains(t, args, "--dangerously-skip-permissions")
	assertContains(t, args, "--output-format")
	assertContains(t, args, "json")
}

func TestAgentPreset_BuildCommand_Codex(t *testing.T) {
	r := NewPresetRegistry()
	p, _ := r.Get("codex")

	args := p.BuildCommand(ExecOptions{
		Prompt:          "Add tests",
		SkipPermissions: true,
	})

	assertContains(t, args, "codex")
	assertContains(t, args, "-p")
	assertContains(t, args, "Add tests")
	assertContains(t, args, "--dangerously-bypass-approvals-and-sandbox")
}

func TestAgentPreset_BuildCommand_MinimalOptions(t *testing.T) {
	p := &AgentPreset{
		Name:       "minimal",
		Command:    "my-agent",
		PromptFlag: "-p",
	}

	args := p.BuildCommand(ExecOptions{
		Prompt: "hello",
	})

	if len(args) != 3 {
		t.Errorf("expected 3 args, got %d: %v", len(args), args)
	}
	if args[0] != "my-agent" || args[1] != "-p" || args[2] != "hello" {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestAgentPreset_BuildCommand_ExtraArgs(t *testing.T) {
	p := &AgentPreset{Name: "test", Command: "agent"}

	args := p.BuildCommand(ExecOptions{
		ExtraArgs: []string{"--verbose", "--dry-run"},
	})

	if len(args) != 3 {
		t.Errorf("expected 3 args, got %d: %v", len(args), args)
	}
	assertContains(t, args, "--verbose")
	assertContains(t, args, "--dry-run")
}

func TestAgentPreset_BuildCommand_NoFlagSkipsField(t *testing.T) {
	p := &AgentPreset{
		Name:    "noop",
		Command: "agent",
		// No PromptFlag, ModelFlag, etc.
	}

	args := p.BuildCommand(ExecOptions{
		Prompt: "ignored because no flag",
		Model:  "ignored",
	})

	// Should only contain the command itself.
	if len(args) != 1 {
		t.Errorf("expected 1 arg (command only), got %d: %v", len(args), args)
	}
}

func assertContains(t *testing.T, args []string, want string) {
	t.Helper()
	for _, a := range args {
		if a == want {
			return
		}
	}
	t.Errorf("args %v does not contain %q", args, want)
}
