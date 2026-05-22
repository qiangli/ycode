package selfinit

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestClaude_WriteMCP_FreshConfig(t *testing.T) {
	home := withFakeHome(t)
	c := &claude{}

	caps := []CapabilitySpec{
		{Name: "ycode-stdio", Transport: "stdio", Command: "ycode", Args: []string{"mcp", "serve"}, Family: "stdio"},
		{Name: "ycode-loom", Transport: "http", URL: fmt.Sprintf("http://127.0.0.1:%d/loom-mcp/", DefaultPort), Family: "loom"},
	}
	changed, err := c.WriteMCP(context.Background(), caps)
	if err != nil {
		t.Fatalf("WriteMCP: %v", err)
	}
	if !changed {
		t.Errorf("expected changed=true on fresh config")
	}

	body, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, body)
	}
	servers, _ := parsed["mcpServers"].(map[string]any)
	if _, ok := servers["ycode-stdio"]; !ok {
		t.Errorf("ycode-stdio missing")
	}
	if _, ok := servers["ycode-loom"]; !ok {
		t.Errorf("ycode-loom missing")
	}

	// Idempotent.
	changed2, err := c.WriteMCP(context.Background(), caps)
	if err != nil {
		t.Fatalf("WriteMCP#2: %v", err)
	}
	if changed2 {
		t.Errorf("idempotent re-write should report changed=false")
	}
}

func TestClaude_WriteMCP_PreservesUserServers(t *testing.T) {
	home := withFakeHome(t)
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"my-tool": map[string]any{"command": "my-tool", "args": []any{}},
		},
		"otherKey": "preserved",
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	c := &claude{}
	caps := []CapabilitySpec{
		{Name: "ycode-loom", Transport: "http", URL: "http://x/", Family: "loom"},
	}
	if _, err := c.WriteMCP(context.Background(), caps); err != nil {
		t.Fatal(err)
	}

	body, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	var parsed map[string]any
	_ = json.Unmarshal(body, &parsed)

	if v := parsed["otherKey"]; v != "preserved" {
		t.Errorf("top-level keys lost: %v", v)
	}
	servers, _ := parsed["mcpServers"].(map[string]any)
	if _, ok := servers["my-tool"]; !ok {
		t.Errorf("user's my-tool server lost")
	}
	if _, ok := servers["ycode-loom"]; !ok {
		t.Errorf("ycode-loom not added")
	}
}

func TestClaude_WriteMCP_DropsStaleYcodeEntries(t *testing.T) {
	home := withFakeHome(t)
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"ycode-pulse": map[string]any{"type": "http", "url": "http://old/"},
			"ycode-old-x": map[string]any{"type": "http", "url": "http://gone/"}, // family no longer in manifest
			"unrelated":   map[string]any{"command": "x"},
		},
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	c := &claude{}
	// Manifest now only advertises pulse — old-x must be removed.
	caps := []CapabilitySpec{
		{Name: "ycode-pulse", Transport: "http", URL: "http://new/", Family: "pulse"},
	}
	if _, err := c.WriteMCP(context.Background(), caps); err != nil {
		t.Fatal(err)
	}

	body, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	var parsed map[string]any
	_ = json.Unmarshal(body, &parsed)
	servers, _ := parsed["mcpServers"].(map[string]any)
	if _, ok := servers["ycode-old-x"]; ok {
		t.Errorf("stale ycode-old-x not removed")
	}
	if _, ok := servers["unrelated"]; !ok {
		t.Errorf("unrelated server removed by stale-cleanup")
	}
	pulse, _ := servers["ycode-pulse"].(map[string]any)
	if pulse["url"] != "http://new/" {
		t.Errorf("ycode-pulse URL not updated: %v", pulse)
	}
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
// opt-in gate works end-to-end: default Run() does not touch
// ~/.claude.json, but Run(..., RegisterForeignTools: true) writes both
// the stdio and HTTP composite entries with correct command/args.
func TestClaudeRoundTrip(t *testing.T) {
	home := withFakeHome(t)
	repo := makeRepo(t)
	t.Setenv("YCODE_SELFINIT_FOREIGN", "") // ensure env doesn't leak opt-in

	// Detect() returns false unless `claude` is on PATH OR ~/.claude/
	// exists. Hosts running this test typically have one of those (the
	// developer's own Claude install), so the test "passes" via ambient
	// state. In a sparse environment (container, fresh CI runner) Run()
	// short-circuits on Detect()==false, never writes ~/.claude.json,
	// and the assertion fails. Seed the dir so the test is hermetic.
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatalf("seed ~/.claude: %v", err)
	}
	// DetectYcodeCommand probes PATH and ~/go/bin/ycode and ~/.local/bin/ycode
	// before falling back to `go run github.com/qiangli/ycode/...@latest`.
	// On a developer host one of those usually exists, so the assertion
	// below (`args == [mcp serve ...]`) passes via ambient state. In a
	// container the fallback kicks in and args becomes
	// `[run github.com/.../cmd/ycode@latest mcp serve]`. Drop an
	// executable stub at ~/.local/bin/ycode so detection is deterministic.
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("seed bin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "ycode"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("seed ycode stub: %v", err)
	}

	configPath := filepath.Join(home, ".claude.json")

	// Default Run: opt-in gate is closed, no write.
	if _, err := Run(context.Background(), Options{
		Cwd: repo, Home: home, YcodeVersion: "test-default",
	}); err != nil {
		t.Fatalf("Run default: %v", err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Errorf("default Run wrote ~/.claude.json without opt-in (err=%v)", err)
	}

	// Opt-in Run: entries must appear.
	if _, err := Run(context.Background(), Options{
		Cwd: repo, Home: home, YcodeVersion: "test-optin", Force: true,
		RegisterForeignTools: true,
	}); err != nil {
		t.Fatalf("Run opt-in: %v", err)
	}

	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read ~/.claude.json: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, body)
	}
	servers, _ := parsed["mcpServers"].(map[string]any)

	stdio, ok := servers["ycode-stdio"].(map[string]any)
	if !ok {
		t.Fatalf("ycode-stdio missing or wrong shape: %v", servers["ycode-stdio"])
	}
	if cmd, _ := stdio["command"].(string); cmd == "" {
		t.Errorf("ycode-stdio.command empty: %v", stdio)
	}
	args, _ := stdio["args"].([]any)
	if len(args) < 2 || args[0] != "mcp" || args[1] != "serve" {
		t.Errorf("ycode-stdio.args want [mcp serve ...], got %v", args)
	}

	http, ok := servers["ycode"].(map[string]any)
	if !ok {
		t.Fatalf("ycode (HTTP composite) missing: %v", servers["ycode"])
	}
	if typ, _ := http["type"].(string); typ != "http" {
		t.Errorf("ycode.type want http, got %v", http["type"])
	}
	if url, _ := http["url"].(string); !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Errorf("ycode.url unexpected: %v", http["url"])
	}

	// L2 memory file must also be written under opt-in.
	memBody, err := os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md"))
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
