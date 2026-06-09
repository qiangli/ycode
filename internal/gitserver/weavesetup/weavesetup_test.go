package weavesetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderParseRoundtrip(t *testing.T) {
	src := Config{
		Slug:              "myapp",
		DefaultBaseBranch: "main",
		IdentityTier:      "ephemeral",
		BackendMode:       "forge",
		DefaultTool:       "claude-code",
		LabelsCreated:     true,
		HookInstalled:     true,
		SetupComplete:     true,
	}
	text := renderConfig(src)
	if !strings.Contains(text, "slug: myapp") {
		t.Errorf("rendered text missing slug field: %s", text)
	}
	got, err := parseConfig(text)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got != src {
		t.Errorf("roundtrip mismatch:\n got %+v\nwant %+v", got, src)
	}
}

func TestParseIgnoresCommentsAndBlanks(t *testing.T) {
	in := `
# top-level comment
slug: alpha

# midline comment
default_tool: codex
`
	got, err := parseConfig(in)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Slug != "alpha" {
		t.Errorf("Slug=%q want alpha", got.Slug)
	}
	if got.DefaultTool != "codex" {
		t.Errorf("DefaultTool=%q want codex", got.DefaultTool)
	}
}

func TestLoadConfig_MissingReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	cfg, exists, err := loadConfig(dir)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if exists {
		t.Errorf("exists=true for missing file")
	}
	if cfg != (Config{}) {
		t.Errorf("expected zero-value config, got %+v", cfg)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	src := Config{Slug: "demo", DefaultTool: "codex", SetupComplete: true}
	if err := saveConfig(dir, src); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}
	got, exists, err := loadConfig(dir)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if !exists {
		t.Fatalf("config not found after save")
	}
	if got.Slug != src.Slug || got.DefaultTool != src.DefaultTool || got.SetupComplete != src.SetupComplete {
		t.Errorf("roundtrip: got %+v want %+v", got, src)
	}
}

// initGitRepo creates a minimal .git directory so installPreCommitHook
// has somewhere to land. We do not run `git init` here to avoid the
// dependency on git in the test environment — installPreCommitHook
// only checks that .git is a directory.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
}

func TestInstallPreCommitHook_FreshRepo(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	if err := installPreCommitHook(dir); err != nil {
		t.Fatalf("installPreCommitHook: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, ".git", "hooks", "pre-commit"))
	if err != nil {
		t.Fatalf("read hook: %v", err)
	}
	if !strings.Contains(string(body), "@ycode.local") {
		t.Errorf("hook body missing email-pattern guard")
	}
}

func TestInstallPreCommitHook_IdempotentSameContent(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	if err := installPreCommitHook(dir); err != nil {
		t.Fatalf("first install: %v", err)
	}
	// Second call should succeed without error.
	if err := installPreCommitHook(dir); err != nil {
		t.Errorf("second install (same content) errored: %v", err)
	}
}

func TestInstallPreCommitHook_RefusesUserHook(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	hooks := filepath.Join(dir, ".git", "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("seed user hook: %v", err)
	}
	err := installPreCommitHook(dir)
	if err == nil {
		t.Fatal("expected error refusing to clobber user hook")
	}
	if !strings.Contains(err.Error(), "different pre-commit hook") {
		t.Errorf("error message=%q does not mention conflict", err.Error())
	}
}

func TestInstallPreCommitHook_NotGitRepo(t *testing.T) {
	dir := t.TempDir()
	err := installPreCommitHook(dir)
	if err == nil {
		t.Fatal("expected error on non-git dir")
	}
}

func TestDetectBackendMode(t *testing.T) {
	dir := t.TempDir()
	if got := detectBackendMode(dir); got != "local" {
		t.Errorf("empty dir backend=%q want local", got)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	if got := detectBackendMode(dir); got != "forge" {
		t.Errorf("with workflows backend=%q want forge", got)
	}
}

// NOTE: a Run() integration test is intentionally omitted here
// because Run requires a fully-wired *weaveapi.Client which itself
// needs a live gitserver. Integration coverage lives in the e2e
// tests under internal/gitserver/loom. The unit tests above cover
// the standalone pieces (config, hook, backend detection) on their
// own, which is where regressions are most likely.
