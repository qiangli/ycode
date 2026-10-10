package main

// Sprint: #412; Story: #896; Story-ID: 1ce7b1d56527
//
// How hard the model thinks is model policy, authored on the model resource as
// `effort`. A profile build may override it for one run -- a local 4B model
// reasons for nothing, a hosted frontier model earns xhigh -- so profile-model
// takes GENIE_EFFORT the same way it takes the context window and the timeout:
// validated, written into the profile, and nowhere else.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// runProfileModel executes dag.md's profile-model target in a scratch copy of
// the bundle and returns the generated profile document.
func runProfileModel(t *testing.T, env ...string) (string, error) {
	t.Helper()
	bash := "/bin/bash"
	if _, err := os.Stat(bash); err != nil {
		t.Skip("no /bin/bash")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not on PATH")
	}
	agent, err := os.ReadFile(filepath.Join("..", "..", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(filepath.Join("..", "..", "prompts", "system.md"))
	if err != nil {
		t.Fatal(err)
	}
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
	cmd := exec.Command(bash, "-c", profileModelScript(t))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GENIE_PROFILE=p", "GENIE_MODEL_ID=claude:opus5",
		"GENIE_CONTEXT_TOKENS=", "GENIE_REQUEST_TIMEOUT_MS=", "GENIE_EFFORT=",
		"BASHY_SELF=/nonexistent/bashy")
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), err
	}
	raw, readErr := os.ReadFile(filepath.Join(dir, "dist", "profiles", "p", "agent.yaml"))
	if readErr != nil {
		t.Fatalf("profile-model: %v\n%s", readErr, out)
	}
	return string(raw), nil
}

func yamlStrings(t *testing.T, doc, key string) []string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `:\s*(\S+)\s*$`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(doc, -1) {
		out = append(out, m[1])
	}
	return out
}

// The bundle declares an effort, so every profile has one even when the build
// says nothing about it.
func TestProfileKeepsDeclaredEffortByDefault(t *testing.T) {
	doc, err := runProfileModel(t)
	if err != nil {
		t.Fatalf("profile-model: %v\n%s", err, doc)
	}
	if got := yamlStrings(t, doc, "effort"); len(got) != 1 || got[0] != "high" {
		t.Fatalf("effort = %v, want one high", got)
	}
}

// GENIE_EFFORT replaces it, for exactly the one model the bundle declares.
func TestProfileTakesEffortFromTheEnvironment(t *testing.T) {
	for _, effort := range []string{"none", "minimal", "low", "medium", "high", "xhigh"} {
		doc, err := runProfileModel(t, "GENIE_EFFORT="+effort)
		if err != nil {
			t.Fatalf("effort %q: %v\n%s", effort, err, doc)
		}
		if got := yamlStrings(t, doc, "effort"); len(got) != 1 || got[0] != effort {
			t.Fatalf("effort = %v, want one %q", got, effort)
		}
	}
}

// An effort ycode's schema does not accept fails the build instead of writing
// a profile that cannot compile.
func TestProfileRejectsUnknownEffort(t *testing.T) {
	out, err := runProfileModel(t, "GENIE_EFFORT=maximum")
	if err == nil {
		t.Fatalf("profile-model accepted an unknown effort:\n%s", out)
	}
	if !strings.Contains(out, "GENIE_EFFORT") {
		t.Fatalf("error does not name GENIE_EFFORT:\n%s", out)
	}
}
