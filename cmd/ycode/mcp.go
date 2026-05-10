package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/runtime/mcp"
	"github.com/qiangli/ycode/internal/runtime/skills"
	"github.com/qiangli/ycode/internal/runtime/treesitter"
	"github.com/qiangli/ycode/internal/shell"
	_ "github.com/qiangli/ycode/internal/shell/agentmode"
	_ "github.com/qiangli/ycode/internal/shell/builtins"
)

// newMcpCmd builds the `ycode mcp` subcommand tree. Today only `serve` exists,
// exposing ycode capabilities to external coding agents (Claude Code, Codex,
// Cursor, Continue, older ycode builds) over the Model Context Protocol.
//
// See docs/lighthouse.md for the broader pattern: foreign agents in this tree
// discover ycode via .mcp.json + ~/.agents/ycode/manifest.json, consume what
// is exposed, and contribute new capabilities back as small handler files.
func newMcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Model Context Protocol server commands",
		Long: "Run a ycode MCP server so external coding agents can use ycode's " +
			"capabilities (AST search, repo-map, memex, Ollama, Gitea workspaces, " +
			"podman sandbox, observability, ...) without plugins or shell exec.",
	}
	cmd.AddCommand(newMcpServeCmd())
	return cmd
}

func newMcpServeCmd() *cobra.Command {
	var ceiling string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run an MCP server over stdio (JSON-RPC + Content-Length framing)",
		Long: "Speaks the MCP protocol on stdin/stdout. External agents connect by " +
			"spawning `ycode mcp serve` per their .mcp.json. Today the surface is " +
			"empty (Phase 0 — infrastructure only); Phase 1+ capability handlers " +
			"plug in via internal/runtime/mcp.CompositeHandler.\n\n" +
			"The default permission ceiling is danger-full-access — agents that " +
			"intentionally configure ycode mcp serve in their settings.json have " +
			"opted into ycode's full capability surface. Lower with " +
			"--permission=read-only or workspace-write for sandboxed integrations.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				select {
				case <-sigCh:
					cancel()
				case <-ctx.Done():
				}
			}()

			// Capability families register here. Each is one mcp.ServerHandler
			// implementing the family's tools — see docs/lighthouse.md for the
			// recipe and internal/observability/mcpserver.go for the canonical
			// template.
			//
			// agent_shell exposes ycode shell's --agent --json semantics as a
			// single MCP tool: foreign agents send a `command` and get the
			// envelope back (stdout, stderr, exit, hints[]). The shared
			// runtime is built with no provider/skills/registry — agents
			// usually want pure bash + yc-builtins, not the LLM sentinels.
			shellRT, _ := shell.New(shell.Options{
				Permission: "danger-full-access",
			})
			composite := mcp.NewCompositeHandler(
				treesitter.NewMCPHandler(),
				shell.NewMCPHandler(shellRT),
				skills.NewMCPHandler(),
			)

			// Permission ceiling. Default is DangerFullAccess so the
			// agent_shell tool (and any other write-capable handler)
			// works out of the box for foreign-agent integrations like
			// Claude Code's mcpServers config. Operators wanting a
			// sandboxed surface pass --permission=read-only or
			// =workspace-write. `ycode serve` will install a prompting
			// gate when this same wiring is mounted under HTTP.
			gateMode := mcp.ModeDangerFullAccess
			switch ceiling {
			case "read-only", "readonly":
				gateMode = mcp.ModeReadOnly
			case "workspace-write", "write":
				gateMode = mcp.ModeWorkspaceWrite
			case "", "danger-full-access", "full", "danger":
				gateMode = mcp.ModeDangerFullAccess
			default:
				return fmt.Errorf("unknown --permission %q (try: read-only, workspace-write, danger-full-access)", ceiling)
			}
			gate := mcp.StaticGate{Ceiling: gateMode}
			handler := mcp.NewGatedHandler(composite, gate)

			if err := mcp.RunServer(ctx, handler); err != nil {
				return fmt.Errorf("mcp serve: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&ceiling, "permission", "", "Permission ceiling: read-only | workspace-write | danger-full-access (default)")
	return cmd
}
