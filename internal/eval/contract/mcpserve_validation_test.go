// MCP serve integration tests. Exercises the protocol + composite + permission
// gate end-to-end against an in-memory request stream — same handler chain as
// `ycode mcp serve` constructs in cmd/ycode/mcp.go, just without spawning a
// subprocess. Validates the lighthouse Phase-0 contract: tools/list answers,
// permission gate denies above-ceiling calls, composite routes by tool name.
package contract

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/mcp"
)

// fakeFamily is a stub capability handler that mimics the Phase-1+ shape:
// declares its tools, declares the required mode for each, and records calls.
type fakeFamily struct {
	tools map[string]mcp.PermissionMode
	calls []string
}

func (f *fakeFamily) ListTools() []mcp.Tool {
	out := make([]mcp.Tool, 0, len(f.tools))
	for name := range f.tools {
		out = append(out, mcp.Tool{Name: name, Description: name})
	}
	return out
}
func (f *fakeFamily) ListResources() []mcp.Resource { return nil }
func (f *fakeFamily) HandleToolCall(_ context.Context, name string, _ json.RawMessage) (string, error) {
	f.calls = append(f.calls, name)
	return "ok:" + name, nil
}
func (f *fakeFamily) ReadResource(_ context.Context, _ string) (string, error) { return "", nil }
func (f *fakeFamily) RequiredMode(name string) mcp.PermissionMode {
	if m, ok := f.tools[name]; ok {
		return m
	}
	return mcp.ModeReadOnly
}

// buildPhase0Server mirrors cmd/ycode/mcp.go: composite under a static
// ReadOnly ceiling. Tests pass a family to mount under the composite.
func buildPhase0Server(families ...mcp.ServerHandler) *mcp.Server {
	composite := mcp.NewCompositeHandler(families...)
	gated := mcp.NewGatedHandler(composite, mcp.StaticGate{Ceiling: mcp.ModeReadOnly})
	return mcp.NewServer(gated)
}

func mustReq(t *testing.T, srv *mcp.Server, method string, params any) *mcp.JSONRPCResponse {
	t.Helper()
	resp, err := srv.HandleRequest(context.Background(), &mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	})
	if err != nil {
		t.Fatalf("%s: HandleRequest err: %v", method, err)
	}
	return resp
}

func TestMCPServe_Phase0_EmptyToolsList(t *testing.T) {
	t.Parallel()
	srv := buildPhase0Server() // no families — Phase-0 default

	resp := mustReq(t, srv, "tools/list", nil)
	if resp.Error != nil {
		t.Fatalf("tools/list returned error: %v", resp.Error)
	}

	// The result must marshal `tools` as `[]`, not `null` — some MCP clients
	// (notably Claude Code) reject the null form.
	if !strings.Contains(string(resp.Result), `"tools":[]`) {
		t.Fatalf("expected empty tools array, got: %s", resp.Result)
	}
}

func TestMCPServe_GateDeniesAboveCeiling(t *testing.T) {
	t.Parallel()
	fam := &fakeFamily{tools: map[string]mcp.PermissionMode{
		"read_thing":  mcp.ModeReadOnly,
		"write_thing": mcp.ModeWorkspaceWrite,
		"shell_thing": mcp.ModeDangerFullAccess,
	}}
	srv := buildPhase0Server(fam)

	// Read-only call passes.
	resp := mustReq(t, srv, "tools/call", map[string]any{"name": "read_thing"})
	if resp.Error != nil {
		t.Fatalf("read_thing should pass under ReadOnly ceiling, got error: %v", resp.Error)
	}

	// Write call is blocked by gate before reaching inner.
	resp = mustReq(t, srv, "tools/call", map[string]any{"name": "write_thing"})
	if resp.Error == nil {
		t.Fatalf("write_thing should be denied under ReadOnly ceiling")
	}
	if !strings.Contains(resp.Error.Message, "permission denied") {
		t.Fatalf("expected permission denied, got: %s", resp.Error.Message)
	}

	// Shell call is also blocked.
	resp = mustReq(t, srv, "tools/call", map[string]any{"name": "shell_thing"})
	if resp.Error == nil {
		t.Fatalf("shell_thing should be denied under ReadOnly ceiling")
	}

	// Inner handler must have seen exactly one call (read_thing); the other
	// two must have been denied before reaching inner.
	if got := strings.Join(fam.calls, ","); got != "read_thing" {
		t.Fatalf("inner handler should have seen only 'read_thing', saw: %q", got)
	}
}

func TestMCPServe_CompositeRoutesByToolName(t *testing.T) {
	t.Parallel()
	famA := &fakeFamily{tools: map[string]mcp.PermissionMode{"a_tool": mcp.ModeReadOnly}}
	famB := &fakeFamily{tools: map[string]mcp.PermissionMode{"b_tool": mcp.ModeReadOnly}}
	srv := buildPhase0Server(famA, famB)

	mustReq(t, srv, "tools/call", map[string]any{"name": "a_tool"})
	mustReq(t, srv, "tools/call", map[string]any{"name": "b_tool"})

	if len(famA.calls) != 1 || famA.calls[0] != "a_tool" {
		t.Fatalf("famA should have seen 'a_tool' once, saw: %v", famA.calls)
	}
	if len(famB.calls) != 1 || famB.calls[0] != "b_tool" {
		t.Fatalf("famB should have seen 'b_tool' once, saw: %v", famB.calls)
	}
}

func TestMCPServe_UnknownToolReturnsError(t *testing.T) {
	t.Parallel()
	srv := buildPhase0Server()
	resp := mustReq(t, srv, "tools/call", map[string]any{"name": "nope"})
	if resp.Error == nil {
		t.Fatalf("expected error for unknown tool")
	}
	if !strings.Contains(resp.Error.Message, "unknown tool") {
		t.Fatalf("expected 'unknown tool' error, got: %s", resp.Error.Message)
	}
}
