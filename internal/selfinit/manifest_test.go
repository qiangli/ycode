package selfinit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCapabilities_BaselineWhenMissing(t *testing.T) {
	home := t.TempDir() // empty — no manifest
	caps := LoadCapabilities(home, 12345)
	if len(caps) < 4 {
		t.Fatalf("baseline should have at least 4 caps, got %d: %+v", len(caps), caps)
	}
	wantNames := map[string]bool{
		"ycode-stdio": true,
		"ycode-pulse": true,
		"ycode-gitea": true,
		"ycode-loom":  true,
	}
	for _, c := range caps {
		delete(wantNames, c.Name)
		if c.Transport == "http" {
			if c.URL == "" {
				t.Errorf("http cap %q missing URL", c.Name)
			}
		}
	}
	if len(wantNames) > 0 {
		t.Errorf("baseline missing caps: %v", wantNames)
	}
}

func TestLoadCapabilities_ParsesManifest(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".agents", "ycode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := manifestShape{
		SchemaVersion: "2",
		MCP: manifestMCP{
			Stdio: manifestStdio{Command: "ycode", Args: []string{"mcp", "serve"}},
			HTTP: map[string]string{
				"loom":  "http://127.0.0.1:58080/loom-mcp/",
				"gitea": "http://127.0.0.1:58080/gitea-mcp/",
			},
		},
	}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	caps := LoadCapabilities(home, DefaultPort)
	if len(caps) != 3 {
		t.Fatalf("expected 3 (stdio + 2 http), got %d: %+v", len(caps), caps)
	}
}

func TestLoadCapabilities_FallsBackOnCorruptManifest(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".agents", "ycode")
	_ = os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	caps := LoadCapabilities(home, DefaultPort)
	if len(caps) < 4 {
		t.Errorf("expected baseline fallback on corrupt manifest, got %d caps", len(caps))
	}
}

func TestFamilyDescription_KnownAndFallback(t *testing.T) {
	if got := FamilyDescription("loom"); got == "" {
		t.Error("expected non-empty for loom")
	}
	if got := FamilyDescription("nope-no-such"); got == "" {
		t.Error("expected fallback string")
	}
}
