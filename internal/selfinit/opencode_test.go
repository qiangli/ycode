package selfinit

import (
	"context"
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
