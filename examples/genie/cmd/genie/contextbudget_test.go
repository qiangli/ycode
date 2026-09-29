package main

// Sprint: #322; Story: #1160; Story-ID: 6ce0239a60a5
//
// genie on an external model gets its context window from the caller
// (GENIE_EXTERNAL_CONTEXT, the bench's BENCH_CTX, default 32768). The
// profile-model target writes that window into the profile; ycode's
// context.measure then derives
//
//	contextBudget  = contextTokens - min(model/route maxOutputTokens) - reserveTokens
//	truncateBudget = contextBudget - reserveTokens
//
// and agent-step clears every tool result older than four turns once the
// context passes truncateBudget. At 32768 the fixed 128k-sized reserves left a
// truncateBudget of 384 tokens: in Sprint 322 every opus5 episode worked from
// its last four observations (django-13512 fixed forms and never saw the admin
// path it had grepped; 13121 and 15280 looped to the wall clock).
// mini-swe-agent keeps its whole history. Truncation must start no earlier
// than half the window, at every window a profile can be given.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// profileModelScript is the bsh body of dag.md's profile-model target.
func profileModelScript(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "dag.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, "### profile-model\n")
	if start < 0 {
		t.Fatal("dag.md has no profile-model target")
	}
	body := text[start:]
	open := strings.Index(body, "```bsh\n")
	if open < 0 {
		t.Fatal("profile-model has no bsh block")
	}
	body = body[open+len("```bsh\n"):]
	end := strings.Index(body, "\n```")
	if end < 0 {
		t.Fatal("profile-model bsh block is not closed")
	}
	return body[:end]
}

func yamlInts(t *testing.T, doc, key string) []int {
	t.Helper()
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `:\s*(\d+)\s*$`)
	var out []int
	for _, m := range re.FindAllStringSubmatch(doc, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		t.Fatalf("profile has no %s", key)
	}
	return out
}

func minInt(values []int) int {
	m := values[0]
	for _, v := range values[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func TestProfileTruncatesNoEarlierThanHalfTheWindow(t *testing.T) {
	bash := "/bin/bash"
	if _, err := os.Stat(bash); err != nil {
		t.Skip("no /bin/bash")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not on PATH")
	}
	script := profileModelScript(t)
	agent, err := os.ReadFile(filepath.Join("..", "..", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(filepath.Join("..", "..", "prompts", "system.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, window := range []int{4096, 8192, 16384, 32768, 40000, 65536, 128000, 200000} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), agent, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "prompts", "system.md"), prompt, 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(bash, "-c", script)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GENIE_PROFILE=p", "GENIE_MODEL_ID=claude:opus5",
			"GENIE_CONTEXT_TOKENS="+strconv.Itoa(window), "GENIE_REQUEST_TIMEOUT_MS=",
			"BASHY_SELF=/nonexistent/bashy")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("window %d: profile-model: %v\n%s", window, err, out)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "dist", "profiles", "p", "agent.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		doc := string(raw)
		if got := yamlInts(t, doc, "contextTokens")[0]; got != window {
			t.Fatalf("window %d: profile contextTokens = %d", window, got)
		}
		output := minInt(yamlInts(t, doc, "maxOutputTokens"))
		reserve := yamlInts(t, doc, "reserveTokens")[0]
		contextBudget := window - output - reserve
		truncateBudget := contextBudget - reserve
		if truncateBudget*2 < window {
			t.Fatalf("window %d: tool results are cleared past %d tokens (output reserve %d, compaction reserve %d); want at least half the window",
				window, truncateBudget, output, reserve)
		}
	}
}
