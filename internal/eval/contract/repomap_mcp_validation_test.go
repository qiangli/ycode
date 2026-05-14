// Phase-2 contract for the repomap MCP family. Asserts the handler's
// tools/list surface and end-to-end tools/call round trips for both
// build_repomap and repomap_for_files. Hermetic — tempdir + tiny Go
// source, no external services.
package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/repomap"
)

func writeRepoFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := "package x\n\nfunc Hello() string { return \"hi\" }\n"
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return dir
}

func TestRepomapMCP_ToolsListExposesBothTools(t *testing.T) {
	t.Parallel()
	srv := buildPhase0Server(repomap.NewMCPHandler())

	resp := mustReq(t, srv, "tools/list", nil)
	if resp.Error != nil {
		t.Fatalf("tools/list error: %v", resp.Error)
	}
	body := string(resp.Result)
	if !strings.Contains(body, `"build_repomap"`) {
		t.Fatalf("tools/list missing build_repomap: %s", body)
	}
	if !strings.Contains(body, `"repomap_for_files"`) {
		t.Fatalf("tools/list missing repomap_for_files: %s", body)
	}
}

func TestRepomapMCP_BuildRepomapRoundTrip(t *testing.T) {
	t.Parallel()
	root := writeRepoFixture(t)
	srv := buildPhase0Server(repomap.NewMCPHandler())

	resp := mustReq(t, srv, "tools/call", map[string]any{
		"name": "build_repomap",
		"arguments": map[string]any{
			"root":   root,
			"budget": 4096, // exercise the alias
		},
	})
	if resp.Error != nil {
		t.Fatalf("build_repomap error: %v", resp.Error)
	}
	body := string(resp.Result)
	if !strings.Contains(body, "Hello") {
		t.Fatalf("build_repomap result missing Hello: %s", body)
	}
}

func TestRepomapMCP_ForFilesRoundTrip(t *testing.T) {
	t.Parallel()
	root := writeRepoFixture(t)
	srv := buildPhase0Server(repomap.NewMCPHandler())

	resp := mustReq(t, srv, "tools/call", map[string]any{
		"name": "repomap_for_files",
		"arguments": map[string]any{
			"root":  root,
			"files": []string{"x.go"},
		},
	})
	if resp.Error != nil {
		t.Fatalf("repomap_for_files error: %v", resp.Error)
	}
	body := string(resp.Result)
	if !strings.Contains(body, "Hello") {
		t.Fatalf("repomap_for_files result missing Hello: %s", body)
	}
}

func TestRepomapMCP_ForFilesRequiresFiles(t *testing.T) {
	t.Parallel()
	srv := buildPhase0Server(repomap.NewMCPHandler())

	resp := mustReq(t, srv, "tools/call", map[string]any{
		"name":      "repomap_for_files",
		"arguments": map[string]any{},
	})
	if resp.Error == nil {
		t.Fatalf("repomap_for_files with no files should error")
	}
	if !strings.Contains(resp.Error.Message, "files is required") {
		t.Fatalf("expected 'files is required', got: %s", resp.Error.Message)
	}
}
