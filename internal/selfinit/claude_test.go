package selfinit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withFakeHome redirects HOME so the writer's UserHomeDir() resolves
// inside a tempdir. Restores HOME on cleanup.
func withFakeHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	return dir
}

func TestClaude_WriteInstructions(t *testing.T) {
	home := withFakeHome(t)
	c := &claude{}
	caps := []CapabilitySpec{
		{Name: "ycode-loom", Family: "loom"},
	}
	changed, err := c.WriteInstructions(context.Background(), caps)
	if err != nil {
		t.Fatalf("WriteInstructions: %v", err)
	}
	if !changed {
		t.Errorf("expected changed=true on fresh memory file")
	}
	body, _ := os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md"))
	if !strings.Contains(string(body), BeginMarker) {
		t.Errorf("missing BEGIN marker:\n%s", body)
	}
	if !strings.Contains(string(body), "ycode-loom") {
		t.Errorf("missing ycode-loom mention:\n%s", body)
	}

	// Idempotent.
	changed2, err := c.WriteInstructions(context.Background(), caps)
	if err != nil {
		t.Fatalf("WriteInstructions#2: %v", err)
	}
	if changed2 {
		t.Errorf("idempotent re-write should report changed=false")
	}
}

// TestClaudeRoundTrip runs the full SelfInit orchestrator with the real
// claude Tool implementation against a tempdir HOME, asserting that the
// opt-in gate works end-to-end: default Run() does not touch the user
// config, but Run(..., RegisterForeignTools: true) writes the L2
// instructions memory file.
func TestClaudeRoundTrip(t *testing.T) {
	home := withFakeHome(t)
	repo := makeRepo(t)
	t.Setenv("YCODE_SELFINIT_FOREIGN", "") // ensure env doesn't leak opt-in

	// Detect() returns false unless `claude` is on PATH OR ~/.claude/
	// exists. Hosts running this test typically have one of those (the
	// developer's own Claude install), so the test "passes" via ambient
	// state. In a sparse environment (container, fresh CI runner) Run()
	// short-circuits on Detect()==false, never writes anything, and the
	// assertion fails. Seed the dir so the test is hermetic.
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatalf("seed ~/.claude: %v", err)
	}

	memPath := filepath.Join(home, ".claude", "CLAUDE.md")

	// Default Run: opt-in gate is closed, no write.
	if _, err := Run(context.Background(), Options{
		Cwd: repo, Home: home, YcodeVersion: "test-default",
	}); err != nil {
		t.Fatalf("Run default: %v", err)
	}
	if _, err := os.Stat(memPath); !os.IsNotExist(err) {
		t.Errorf("default Run wrote CLAUDE.md without opt-in (err=%v)", err)
	}

	// Opt-in Run: L2 instructions memory file must appear.
	if _, err := Run(context.Background(), Options{
		Cwd: repo, Home: home, YcodeVersion: "test-optin", Force: true,
		RegisterForeignTools: true,
	}); err != nil {
		t.Fatalf("Run opt-in: %v", err)
	}

	memBody, err := os.ReadFile(memPath)
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(memBody), BeginMarker) {
		t.Errorf("CLAUDE.md missing BEGIN marker:\n%s", memBody)
	}
}

func TestClaude_WriteInstructions_PreservesUserContent(t *testing.T) {
	home := withFakeHome(t)
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	user := "# My Claude memory\n\nUser-curated notes that must not be lost.\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &claude{}
	caps := []CapabilitySpec{{Name: "ycode-loom", Family: "loom"}}
	if _, err := c.WriteInstructions(context.Background(), caps); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(string(body), "User-curated notes that must not be lost.") {
		t.Errorf("user content lost:\n%s", body)
	}
	if !strings.Contains(string(body), BeginMarker) {
		t.Errorf("BEGIN marker missing")
	}
}
