package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/runtime/mcp"
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
	return &cobra.Command{
		Use:   "serve",
		Short: "Run an MCP server over stdio (JSON-RPC + Content-Length framing)",
		Long: "Speaks the MCP protocol on stdin/stdout. External agents connect by " +
			"spawning `ycode mcp serve` per their .mcp.json. Today the surface is " +
			"empty (Phase 0 — infrastructure only); Phase 1+ capability handlers " +
			"plug in via internal/runtime/mcp.CompositeHandler.",
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
			)

			// Standalone stdio invocation has no human-loop client to prompt for
			// permission, so the default gate denies anything above ReadOnly.
			// `ycode serve` will install a prompting gate when this same wiring
			// is mounted under HTTP for in-session use.
			//
			// To allow agent_shell (DangerFullAccess), agents must explicitly
			// raise the ceiling — see docs/shell-agent.md.
			gate := mcp.StaticGate{Ceiling: mcp.ModeReadOnly}
			handler := mcp.NewGatedHandler(composite, gate)

			if err := mcp.RunServer(ctx, handler); err != nil {
				return fmt.Errorf("mcp serve: %w", err)
			}
			return nil
		},
	}
}
