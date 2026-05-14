// Phase-2 contract for the memex memory MCP family. Asserts the handler
// exposes the expected tool surface, allows project-scoped writes when
// the gate permits, denies privileged-scope writes at the handler
// boundary regardless of gate, and returns recall hits for a stored fact.
// Hermetic — tmpdir-backed Manager, no network or external services.
package contract

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/mcp"
	"github.com/qiangli/ycode/internal/runtime/memexmcp"
	"github.com/qiangli/ycode/pkg/memex/memory"
)

func newMemexManager(t *testing.T) *memory.Manager {
	t.Helper()
	root := t.TempDir()
	mgr, err := memory.NewManagerWithGlobal(
		filepath.Join(root, "global"),
		filepath.Join(root, "project"),
	)
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	return mgr
}

func TestMemexMCP_ToolsListExposesPhase2Tools(t *testing.T) {
	t.Parallel()
	srv := buildPhase0Server(memexmcp.NewMCPHandler(newMemexManager(t)))

	resp := mustReq(t, srv, "tools/list", nil)
	if resp.Error != nil {
		t.Fatalf("tools/list error: %v", resp.Error)
	}
	body := string(resp.Result)
	for _, name := range []string{"memex_recall", "memex_save", "memex_list", "memex_forget", "memex_index", "search_memex", "list_memory_types"} {
		if !strings.Contains(body, `"`+name+`"`) {
			t.Fatalf("tools/list missing %s: %s", name, body)
		}
	}
}

func TestMemexMCP_SaveRecallRoundTrip(t *testing.T) {
	t.Parallel()
	srv := buildPhase2Server(mcp.ModeWorkspaceWrite, memexmcp.NewMCPHandler(newMemexManager(t)))

	resp := mustReq(t, srv, "tools/call", map[string]any{
		"name": "memex_save",
		"arguments": map[string]any{
			"name":        "build-script-location",
			"type":        "project",
			"scope":       "project",
			"description": "where the build script lives",
			"content":     "the project build script is at scripts/build.sh",
		},
	})
	if resp.Error != nil {
		t.Fatalf("memex_save error: %v", resp.Error)
	}
	if !strings.Contains(string(resp.Result), `\"ok\":true`) {
		t.Fatalf("memex_save did not return ok: %s", resp.Result)
	}

	resp = mustReq(t, srv, "tools/call", map[string]any{
		"name": "memex_recall",
		"arguments": map[string]any{
			"query":       "build script",
			"max_results": 10,
		},
	})
	if resp.Error != nil {
		t.Fatalf("memex_recall error: %v", resp.Error)
	}
	if !strings.Contains(string(resp.Result), `\"build-script-location\"`) {
		t.Fatalf("memex_recall did not return stored memory: %s", resp.Result)
	}
}

func TestMemexMCP_PrivilegedScopeWriteRejected(t *testing.T) {
	t.Parallel()
	// Even at DangerFullAccess the handler rejects user/global/team writes
	// at the MCP boundary — those scopes are operator-driven by design.
	srv := buildPhase2Server(mcp.ModeDangerFullAccess, memexmcp.NewMCPHandler(newMemexManager(t)))

	for _, scope := range []string{"global", "user", "team"} {
		resp := mustReq(t, srv, "tools/call", map[string]any{
			"name": "memex_save",
			"arguments": map[string]any{
				"name":        "leaky",
				"type":        "user",
				"scope":       scope,
				"description": "should not land",
				"content":     "should not land",
			},
		})
		if resp.Error == nil {
			t.Fatalf("memex_save with scope=%s should be rejected at the MCP boundary", scope)
		}
		if !strings.Contains(resp.Error.Message, "not allowed over MCP") {
			t.Fatalf("scope=%s rejection message unexpected: %s", scope, resp.Error.Message)
		}
	}
}

func TestMemexMCP_SaveDeniedUnderReadOnlyGate(t *testing.T) {
	t.Parallel()
	srv := buildPhase0Server(memexmcp.NewMCPHandler(newMemexManager(t)))

	resp := mustReq(t, srv, "tools/call", map[string]any{
		"name": "memex_save",
		"arguments": map[string]any{
			"name":        "x",
			"type":        "project",
			"description": "x",
			"content":     "x",
		},
	})
	if resp.Error == nil {
		t.Fatalf("memex_save should be denied by the ReadOnly gate")
	}
	if !strings.Contains(resp.Error.Message, "permission denied") {
		t.Fatalf("expected permission denied, got: %s", resp.Error.Message)
	}
}

func TestMemexMCP_ListMemoryTypesReturnsSeven(t *testing.T) {
	t.Parallel()
	srv := buildPhase0Server(memexmcp.NewMCPHandler(newMemexManager(t)))

	resp := mustReq(t, srv, "tools/call", map[string]any{
		"name":      "list_memory_types",
		"arguments": map[string]any{},
	})
	if resp.Error != nil {
		t.Fatalf("list_memory_types error: %v", resp.Error)
	}
	body := string(resp.Result)
	for _, want := range []string{`\"user\"`, `\"feedback\"`, `\"project\"`, `\"reference\"`, `\"episodic\"`, `\"procedural\"`, `\"task\"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("list_memory_types missing %s: %s", want, body)
		}
	}
}

func TestMemexMCP_SearchMemexFiltersBySources(t *testing.T) {
	t.Parallel()
	srv := buildPhase2Server(mcp.ModeWorkspaceWrite, memexmcp.NewMCPHandler(newMemexManager(t)))

	// Save a record so there's something to recall.
	mustReq(t, srv, "tools/call", map[string]any{
		"name": "memex_save",
		"arguments": map[string]any{
			"name":        "alpha",
			"type":        "project",
			"description": "alpha record",
			"content":     "alpha content for search",
		},
	})

	// search_memex with a non-matching source filter must return an empty
	// array (filter applied) — not an error.
	resp := mustReq(t, srv, "tools/call", map[string]any{
		"name": "search_memex",
		"arguments": map[string]any{
			"query":   "alpha",
			"sources": []string{"definitely-not-a-real-source"},
		},
	})
	if resp.Error != nil {
		t.Fatalf("search_memex error: %v", resp.Error)
	}
	if !strings.Contains(string(resp.Result), `"text":"[]"`) {
		t.Fatalf("search_memex with bogus source filter should return empty array, got: %s", resp.Result)
	}

	// search_memex with no filter must return the saved record.
	resp = mustReq(t, srv, "tools/call", map[string]any{
		"name": "search_memex",
		"arguments": map[string]any{
			"query": "alpha",
		},
	})
	if resp.Error != nil {
		t.Fatalf("search_memex unfiltered error: %v", resp.Error)
	}
	if !strings.Contains(string(resp.Result), `\"name\":\"alpha\"`) {
		t.Fatalf("search_memex unfiltered should return alpha, got: %s", resp.Result)
	}
}
