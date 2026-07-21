package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestServeManifestAdvertisesNoMCP is the regression gate for the bug
// where ~/.agents/ycode/manifest.json advertised an "mcp" block (stdio
// `ycode mcp serve`) and an "mcp" endpoint (<proxy>/mcp/). Neither
// exists: there is no `mcp` cobra command and serve mounts no /mcp/
// route, so every foreign CLI that read this manifest reported a dead
// server at startup. The manifest must describe only what serve mounts.
//
// If you add a real MCP server back, wire the route first and assert
// the route here — not the other way round.
func TestServeManifestAdvertisesNoMCP(t *testing.T) {
	home := t.TempDir()
	for _, apiUp := range []bool{true, false} {
		full := buildServeManifest(home, 31415, 4222, nil, apiUp, "test")
		if _, ok := full["mcp"]; ok {
			t.Errorf("apiUp=%v: manifest has an mcp block", apiUp)
		}
		endpoints, _ := full["endpoints"].(map[string]string)
		if _, ok := endpoints["mcp"]; ok {
			t.Errorf("apiUp=%v: endpoints advertise an mcp URL", apiUp)
		}
		// Serialized form is what foreign tools actually read — check it
		// end to end so a nested block can't smuggle the route back in.
		for _, m := range []map[string]any{full, publicServeManifest(full)} {
			data, err := json.Marshal(m)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if strings.Contains(string(data), "/mcp/") {
				t.Errorf("apiUp=%v: manifest names the retired /mcp/ route:\n%s", apiUp, data)
			}
		}
	}
}

// TestPublicManifestStripsLocalPaths guards the split the public
// manifest exists for: remote callers get URLs and auth shape, never
// local filesystem paths.
func TestPublicManifestStripsLocalPaths(t *testing.T) {
	home := t.TempDir()
	pub := publicServeManifest(buildServeManifest(home, 31415, 4222, nil, true, "test"))
	if _, ok := pub["discoveryFiles"]; ok {
		t.Error("public manifest leaks discoveryFiles")
	}
	data, _ := json.Marshal(pub)
	if strings.Contains(string(data), home) {
		t.Errorf("public manifest leaks a local path:\n%s", data)
	}
}
