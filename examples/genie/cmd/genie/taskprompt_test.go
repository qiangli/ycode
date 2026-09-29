package main

// Sprint: #322; Story: #1168; Story-ID: 6b10fe9133a2
//
// django-12774 on the gpt55 seat: genie 954 s / 35 calls vs mini-swe-agent
// 167 s / 9 calls, resolved both. 17 calls went to regression tests nobody
// scores (the grader replaces test files), 3 to a README hunt that reached
// outside the workspace (`find ..`, denied), and one retry per run to a
// bare python edit. These tests pin the prompt side of the fix.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The SWE-bench task template, like mini-swe-agent's instance template, is
// source-only: no test edits, verify with a throwaway script plus the
// existing tests, and leave the script out of the diff.
func TestSWEBenchTaskIsSourceOnly(t *testing.T) {
	prompt := taskPrompt("django__django-12774", "in_bulk() fails for UniqueConstraint fields")
	for _, want := range []string{
		"non-test source files only",
		"Do not add, edit, or delete test files",
		"throwaway reproduction script",
		"existing tests",
		"delete the script",
		"Stay inside the repository",
		"Instance: django__django-12774",
		"Issue:\nin_bulk() fails for UniqueConstraint fields",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("task prompt lacks %q:\n%s", want, prompt)
		}
	}
}

// prompts/system.md is shared with chat (`bashy genie`), where a user may ask
// for tests: it must not carry the benchmark's source-only rule, and it must
// not send the model hunting for instructions or outside the workspace.
func TestSystemPromptScopesOrientationAndLeavesTestsToTheTask(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "prompts", "system.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"a quick look at the repository root is enough",
		"do not search further for instructions",
		"Stay inside the repository",
		"tests unless the task asks for it",
		"already wrapped: send that",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("prompts/system.md lacks %q", want)
		}
	}
	for _, banned := range []string{"repository instructions first", "non-test source files only"} {
		if strings.Contains(text, banned) {
			t.Errorf("prompts/system.md still says %q", banned)
		}
	}
}

// The deny-command node hands an unprovable command back already wrapped,
// so the model's next call is the allowed form (#1168 cause 2). The
// preflight/policy round trip of that form is tested in ycode's
// internal/harness/turn against this same agent.yaml.
func TestDenialCarriesTheWrappedForm(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, "\n    deny-command:\n")
	end := strings.Index(text, "\n    approve-command:\n")
	if start < 0 || end < start {
		t.Fatal("agent.yaml: no deny-command pipeline")
	}
	section := text[start:end]
	for _, want := range []string{
		"remedy:",
		"rules: [incomplete-preflight]",
		`@effects("read,write,exec")`,
		`@contain(net: "deny")`,
		"{{command}}",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("deny-command lacks %q", want)
		}
	}
}
