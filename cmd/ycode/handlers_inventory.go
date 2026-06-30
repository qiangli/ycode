package main

import (
	"github.com/qiangli/ycode/internal/docs"
	"github.com/qiangli/ycode/internal/extractmcp"
	"github.com/qiangli/ycode/internal/runtime/codegraph"
	gh "github.com/qiangli/ycode/internal/runtime/github"
	"github.com/qiangli/ycode/internal/runtime/mcp"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/browsermcp"
	"github.com/qiangli/ycode/internal/runtime/repomap"
	"github.com/qiangli/ycode/internal/runtime/skills"
	"github.com/qiangli/ycode/internal/runtime/treesitter"
	"github.com/qiangli/ycode/internal/shell"
)

// alwaysOnMCPHandlers constructs every MCP handler that:
//   - can be built without runtime deps (no memory.Manager, no provider,
//     no live serve stack), and
//   - is registered into BOTH the stdio composite (cmd/ycode/mcp.go)
//     and the HTTP composite (cmd/ycode/serve.go).
//
// Single source of truth for two callers:
//
//   - capabilities_test.go uses this list to assert every declared
//     MCP tool in registry.yaml resolves to a real handler.
//   - tools_cmd.go uses it to enumerate the MCP surface for
//     `ycode tools list --mcp`.
//
// When you add a new always-on handler, update this function AND the
// composite registrations in mcp.go + serve.go in lock-step. Forgetting
// either causes the lint to fail or the inventory to drift — both are
// loud failures, by design.
//
// Handlers NOT included here (and the reason): memexmcp (needs
// memory.Manager), gitea/loom (need Gitea), widget (needs bus),
// extractmcp.JSONHandler (needs provider).
// Their capabilities are declared in registry.yaml with a `gaps:` entry
// so the lint suppresses MCP validation for them.
func alwaysOnMCPHandlers() []mcp.ServerHandler {
	// shell needs a runtime but ListTools doesn't call into it; nil is
	// safe for the inventory.
	shellRT, _ := shell.New(shell.Options{Permission: "danger-full-access"})

	return []mcp.ServerHandler{
		treesitter.NewMCPHandler(),
		skills.NewMCPHandler(),
		docs.NewMCPHandler(),
		newCobraMCPHandler(),
		extractmcp.NewDocumentHandler(),
		repomap.NewMCPHandler(),
		codegraph.NewMCPHandler(),
		NewMCPHandler(),
		gh.NewMCPHandler(),
		browsermcp.NewMCPHandler(nil),
		shell.NewMCPHandler(shellRT),
	}
}
