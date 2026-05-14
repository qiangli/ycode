// Shared helpers for Phase-2 contract tests. The Phase-0 harness in
// mcpserve_validation_test.go pins the gate to ReadOnly so it can verify
// the deny path; Phase-2 tests exercise write tools (memex_save) and
// need a configurable ceiling.
package contract

import (
	"github.com/qiangli/ycode/internal/runtime/mcp"
)

// buildPhase2Server builds an MCP server with the requested permission
// ceiling and one or more family handlers behind the composite. Use this
// when a tools/call exercise requires more than ReadOnly.
func buildPhase2Server(ceiling mcp.PermissionMode, families ...mcp.ServerHandler) *mcp.Server {
	composite := mcp.NewCompositeHandler(families...)
	gated := mcp.NewGatedHandler(composite, mcp.StaticGate{Ceiling: ceiling})
	return mcp.NewServer(gated)
}
