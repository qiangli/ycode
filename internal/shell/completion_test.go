package shell

import (
	"context"
	"testing"

	"github.com/qiangli/ycode/internal/commands"
)

func newRegistryWithShellSafe(t *testing.T) *commands.Registry {
	t.Helper()
	r := commands.NewRegistry()
	r.Register(&commands.Spec{Name: "help", ShellSafe: true,
		Handler: func(_ context.Context, _ string) (string, error) { return "h", nil }})
	r.Register(&commands.Spec{Name: "clear", ShellSafe: true,
		Handler: func(_ context.Context, _ string) (string, error) { return "", nil }})
	r.Register(&commands.Spec{Name: "deploy", ShellSafe: false,
		Handler: func(_ context.Context, _ string) (string, error) { return "", nil }})
	return r
}

func TestCompleteSlash(t *testing.T) {
	rt := newTestRuntime(t, Options{Registry: newRegistryWithShellSafe(t)})
	cs := completeSlash(rt, "h")
	if len(cs) != 1 {
		t.Fatalf("expected 1 candidate for 'h' prefix, got %d: %+v", len(cs), cs)
	}
	if cs[0].Display != "/help" {
		t.Errorf("got %q, want /help", cs[0].Display)
	}
	if cs[0].Kind != CompletionSlash {
		t.Errorf("kind = %v, want %v", cs[0].Kind, CompletionSlash)
	}

	// Empty prefix returns all shell-safe commands; deploy is excluded.
	cs = completeSlash(rt, "")
	if len(cs) != 2 {
		t.Fatalf("empty prefix expected 2 (help+clear) shell-safe, got %d: %+v", len(cs), cs)
	}
}

func TestCompleteSkill(t *testing.T) {
	skills := []string{"review", "summarize", "security-review"}
	cs := completeSkill(skills, "s")
	if len(cs) != 2 {
		t.Fatalf("expected 2 candidates ('summarize','security-review'), got %d: %+v", len(cs), cs)
	}
}

func TestCompleteForRouting(t *testing.T) {
	rt := newTestRuntime(t, Options{
		Registry: newRegistryWithShellSafe(t),
		Skills:   &fakeSkillResolver{byName: map[string]string{"review": "r"}},
	})

	if cs := CompleteFor(rt, "/h"); len(cs) == 0 || cs[0].Kind != CompletionSlash {
		t.Errorf("'/h' should route to slash, got %+v", cs)
	}
	if cs := CompleteFor(rt, "@r"); len(cs) == 0 || cs[0].Kind != CompletionSkill {
		t.Errorf("'@r' should route to skill, got %+v", cs)
	}
	// Bare prefix should hit PATH; the test environment generally has /bin/ls.
	if cs := CompleteFor(rt, "ls"); len(cs) == 0 {
		t.Skip("no PATH match for 'ls' in this environment — skipping")
	}
}
