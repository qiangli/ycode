package main

// Guardrails for Sprint 316 story 7f33c57b: the 16 lost episodes in
// docs/sprint-316-gap-analysis.md (4 harness-cap, 10 no-diff, 2 timeout).
// Each test below fails on the pre-fix genie and passes after.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// agentConfigValue scans ../../agent.yaml for `key: N` inside the named
// hooks/pipelines section (a `    name:` line at 4-space indent).
func agentConfigValue(t *testing.T, section, key string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	sectionRe := regexp.MustCompile(`^    ([A-Za-z0-9_-]+):\s*$`)
	valueRe := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `:\s*(\d+)\s*$`)
	current := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if m := sectionRe.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		if m := valueRe.FindStringSubmatch(line); m != nil && current == section {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
	}
	t.Fatalf("agent.yaml: no %q inside section %q", key, section)
	return 0
}

// The after-tool-audit hook fires once per tool execution, while the agent
// loop runs up to maxIterations steps of up to maxItems parallel calls each.
// Four opus5 episodes died on `hook "after-tool-audit" exceeded
// maxInvocations 25` with zero scored diff; the cap must cover the loop
// budget (mini-swe-agent's 250 steps) instead of cutting it short.
func TestAfterToolAuditCapCoversLoopBudget(t *testing.T) {
	cap := agentConfigValue(t, "after-tool-audit", "maxInvocations")
	steps := agentConfigValue(t, "turn", "maxIterations")
	perStep := agentConfigValue(t, "execute-tool-calls", "maxItems")
	if cap < steps*perStep {
		t.Fatalf("after-tool-audit maxInvocations %d < loop budget %d steps x %d calls = %d",
			cap, steps, perStep, steps*perStep)
	}
}

// The no-diff episodes ended with the model answering text instead of
// calling Bashy (one gpt55 episode finished with zero tool calls; two opus5
// episodes finished with a {"tool_calls": ...} JSON blob as answer text
// after probing absolute paths like /testbed). The system prompt must name
// all three guards.
func TestSystemPromptGuardsEmptyFinish(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "prompts", "system.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"tool_calls",                // never emit a tool call as answer text
		"current working directory", // the repo is here, not /testbed
		"git diff",                  // verify the fix is in the diff
		"empty diff",                // an empty diff means keep working
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("prompts/system.md lacks %q", want)
		}
	}
}

// initGitRepo makes a committed repo dir for workspacePatch/run tests.
func initGitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir())
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "base")
	return dir
}

// django__django-11790 (opus5) left a stray forms.py.bak in model.patch: the
// model edits via backup copies, and workspacePatch diffs every untracked
// file. Backup artifacts are never an intended change.
func TestWorkspacePatchSkipsBackupArtifacts(t *testing.T) {
	repo := initGitRepo(t, map[string]string{"stats.py": "MEAN=1\n"})
	if err := os.WriteFile(filepath.Join(repo, "stats.py"), []byte("MEAN=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backups := map[string]string{
		"stats.py.bak":  "MEAN=BAK\n",
		"stats.py.orig": "MEAN=ORIG\n",
		"stats.py.rej":  "MEAN=REJ\n",
		"stats.py~":     "MEAN=TILDE\n",
	}
	for name, content := range backups {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	patch, err := workspacePatch(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "MEAN=2") {
		t.Fatalf("patch lost the tracked change:\n%s", patch)
	}
	for _, marker := range []string{"MEAN=BAK", "MEAN=ORIG", "MEAN=REJ", "MEAN=TILDE"} {
		if strings.Contains(patch, marker) {
			t.Fatalf("patch contains backup artifact %q:\n%s", marker, patch)
		}
	}
}

// Ten episodes ended `ycode completed without producing a git diff`: the
// adapter accepted the model's empty finish. It must resume the same durable
// session once instead. The fake ycode below always succeeds with no repo
// changes; the assertion is the engine invocation count (1 before, 2 after)
// and that both runs share one session id.
func TestEmptyDiffResumesSessionOnce(t *testing.T) {
	repo := initGitRepo(t, map[string]string{"stats.py": "MEAN=1\n"})
	artifacts := t.TempDir()
	log := filepath.Join(t.TempDir(), "engine.log")
	fake := filepath.Join(t.TempDir(), "ycode")
	// Flatten newlines: the prompt argument is multi-line, and the log needs
	// exactly one line per engine run.
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" | tr '\\n' '|' >> " + strconv.Quote(log) + "\n" +
		"printf '\\n' >> " + strconv.Quote(log) + "\n" +
		"exit 0\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YCODE_BIN", fake)
	t.Setenv("GENIE_MODEL_CHOICE", "")
	req := `{"instance_id":"guardrail-no-diff","problem_statement":"fix","repo_path":` +
		strconv.Quote(repo) + `,"artifact_dir":` + strconv.Quote(artifacts) +
		`,"run_id":"run1","model_name_or_path":"m"}` + "\n"
	var output, diagnostic strings.Builder
	err := run(strings.NewReader(req), &output, &diagnostic,
		filepath.Join("..", "..", "agent.yaml"))
	if err == nil || !strings.Contains(err.Error(), "without producing a git diff") {
		t.Fatalf("expected the empty-diff error, got: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(readFile(t, log)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected one resume (2 engine runs), got %d", len(lines))
	}
	session := func(line string) string {
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "--session" && i+1 < len(fields) {
				return fields[i+1]
			}
		}
		return ""
	}
	first, second := session(lines[0]), session(lines[1])
	if first == "" || first != second {
		t.Fatalf("resume must reuse the session id, got %q then %q", first, second)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
