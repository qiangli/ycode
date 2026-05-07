package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/runtime/mcp"
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

			// Phase 0: empty composite. Phase 1+ capability families register here.
			composite := mcp.NewCompositeHandler()

			// Standalone stdio invocation has no human-loop client to prompt for
			// permission, so the default gate denies anything above ReadOnly.
			// `ycode serve` will install a prompting gate when this same wiring
			// is mounted under HTTP for in-session use.
			gate := mcp.StaticGate{Ceiling: mcp.ModeReadOnly}
			handler := mcp.NewGatedHandler(composite, gate)

			if err := mcp.RunServer(ctx, handler); err != nil {
				return fmt.Errorf("mcp serve: %w", err)
			}
			return nil
		},
	}
}
