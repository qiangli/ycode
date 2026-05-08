package selfinit

import (
	"context"
	"encoding/json"
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
		{Name: "ycode-loom", Transport: "http", URL: "http://127.0.0.1:58080/loom-mcp/", Family: "loom"},
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
