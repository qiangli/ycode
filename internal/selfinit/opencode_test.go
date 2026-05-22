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

// withFakeXDG sets XDG_CONFIG_HOME and HOME so OpenCode paths resolve
// inside a tempdir. Returns the XDG dir.
func withFakeXDG(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	xdg := filepath.Join(dir, "xdg")
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	return xdg
}

func TestOpenCode_WriteMCP_FreshConfig(t *testing.T) {
	xdg := withFakeXDG(t)
	o := &opencodeTool{}

	loomURL := fmt.Sprintf("http://127.0.0.1:%d/loom-mcp/", DefaultPort)
	caps := []CapabilitySpec{
		{Name: "ycode-stdio", Transport: "stdio", Command: "ycode", Args: []string{"mcp", "serve"}, Family: "stdio"},
		{Name: "ycode-loom", Transport: "http", URL: loomURL, Family: "loom"},
	}
	changed, err := o.WriteMCP(context.Background(), caps)
	if err != nil {
		t.Fatalf("WriteMCP: %v", err)
	}
	if !changed {
		t.Errorf("expected changed=true on fresh config")
	}

	body, err := os.ReadFile(filepath.Join(xdg, "opencode", "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, body)
	}

	// OpenCode's root key is "mcp", not "mcpServers".
	servers, ok := parsed["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("expected root.mcp to be a map, got %T: %+v", parsed["mcp"], parsed)
	}

	stdio, _ := servers["ycode-stdio"].(map[string]any)
	if stdio["type"] != "local" {
		t.Errorf("ycode-stdio type=%v want local", stdio["type"])
	}
	cmd, ok := stdio["command"].([]any)
	if !ok || len(cmd) != 3 {
		t.Errorf("ycode-stdio command=%v (want flattened [ycode mcp serve])", stdio["command"])
	}
	if stdio["enabled"] != true {
		t.Errorf("ycode-stdio enabled flag missing/false: %v", stdio["enabled"])
	}

	loom, _ := servers["ycode-loom"].(map[string]any)
	if loom["type"] != "remote" {
		t.Errorf("ycode-loom type=%v want remote", loom["type"])
	}
	if loom["url"] != loomURL {
		t.Errorf("ycode-loom url=%v", loom["url"])
	}

	// Idempotent.
	changed2, err := o.WriteMCP(context.Background(), caps)
	if err != nil {
		t.Fatalf("WriteMCP#2: %v", err)
	}
	if changed2 {
		t.Errorf("idempotent re-write should report changed=false")
	}
}

func TestOpenCode_WriteMCP_PreservesUserServers(t *testing.T) {
	xdg := withFakeXDG(t)
	cfg := map[string]any{
		"mcp": map[string]any{
			"my-tool": map[string]any{"type": "local", "command": []any{"my-tool"}, "enabled": true},
		},
		"theme": "dark",
	}
	dir := filepath.Join(xdg, "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	o := &opencodeTool{}
	caps := []CapabilitySpec{
		{Name: "ycode-loom", Transport: "http", URL: "http://x/", Family: "loom"},
	}
	if _, err := o.WriteMCP(context.Background(), caps); err != nil {
		t.Fatal(err)
	}

	body, _ := os.ReadFile(filepath.Join(dir, "opencode.json"))
	var parsed map[string]any
	_ = json.Unmarshal(body, &parsed)

	if v := parsed["theme"]; v != "dark" {
		t.Errorf("top-level keys lost: %v", v)
	}
	servers, _ := parsed["mcp"].(map[string]any)
	if _, ok := servers["my-tool"]; !ok {
		t.Errorf("user's my-tool server lost")
	}
	if _, ok := servers["ycode-loom"]; !ok {
		t.Errorf("ycode-loom not added")
	}
}

func TestOpenCode_WriteInstructions(t *testing.T) {
	xdg := withFakeXDG(t)
	o := &opencodeTool{}
	caps := []CapabilitySpec{
		{Name: "ycode-loom", Family: "loom"},
	}
	changed, err := o.WriteInstructions(context.Background(), caps)
	if err != nil {
		t.Fatalf("WriteInstructions: %v", err)
	}
	if !changed {
		t.Errorf("expected changed=true on fresh memory file")
	}
	body, _ := os.ReadFile(filepath.Join(xdg, "opencode", "AGENTS.md"))
	if !strings.Contains(string(body), BeginMarker) {
		t.Errorf("missing BEGIN marker:\n%s", body)
	}
	if !strings.Contains(string(body), "ycode-loom") {
		t.Errorf("missing ycode-loom mention:\n%s", body)
	}

	changed2, err := o.WriteInstructions(context.Background(), caps)
	if err != nil {
		t.Fatalf("WriteInstructions#2: %v", err)
	}
	if changed2 {
		t.Errorf("idempotent re-write should report changed=false")
	}
}
