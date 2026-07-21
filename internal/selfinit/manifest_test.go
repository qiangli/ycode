package selfinit

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestPath(t *testing.T) {
	home := t.TempDir()
	want := filepath.Join(home, ".agents", "ycode", "manifest.json")
	if got := ManifestPath(home); got != want {
		t.Errorf("ManifestPath() = %q, want %q", got, want)
	}
}

// TestGeneratedDocsAdvertiseNoMCPServer is the regression gate for the
// bug this package caused: SelfInit used to write "ycode-stdio" /
// "ycode" MCP server entries into foreign tools' configs and memory
// files. Neither server exists — `ycode mcp serve` is not a command and
// `ycode serve` mounts no /mcp/ route — so every foreign CLI launched in
// an init'd repo reported a failed MCP server. Nothing SelfInit
// generates may name an MCP server or endpoint again.
func TestGeneratedDocsAdvertiseNoMCPServer(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"instructions", buildInstructionsBlock()},
		{"long-form", buildLongFormDoc()},
	} {
		lower := strings.ToLower(tc.body)
		// Names of the two servers that never existed, plus the shapes a
		// foreign tool would actually copy into its config.
		for _, banned := range []string{"ycode-stdio", "ycode mcp", "/mcp/", "\"mcpservers\":"} {
			if strings.Contains(lower, banned) {
				t.Errorf("%s doc advertises retired MCP surface %q", tc.name, banned)
			}
		}
		if !strings.Contains(lower, "does not run an mcp server") {
			t.Errorf("%s doc should state plainly that ycode runs no MCP server", tc.name)
		}
	}
}
