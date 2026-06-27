package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/docs"
	"github.com/qiangli/ycode/internal/extractmcp"
	"github.com/qiangli/ycode/internal/inference"
	"github.com/qiangli/ycode/internal/memwatch"
	"github.com/qiangli/ycode/internal/runtime/codegraph"
	gh "github.com/qiangli/ycode/internal/runtime/github"
	"github.com/qiangli/ycode/internal/runtime/mcp"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/browsermcp"
	"github.com/qiangli/ycode/internal/runtime/memexmcp"
	"github.com/qiangli/ycode/internal/runtime/origin"
	"github.com/qiangli/ycode/internal/runtime/repomap"
	"github.com/qiangli/ycode/internal/runtime/skills"
	"github.com/qiangli/ycode/internal/runtime/treesitter"
	"github.com/qiangli/ycode/internal/shell"
	_ "github.com/qiangli/ycode/internal/shell/agentmode"
	_ "github.com/qiangli/ycode/internal/shell/builtins"
	"github.com/qiangli/ycode/pkg/memex/memory"
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
			"spawning `ycode mcp serve` per their .mcp.json.\n\n" +
			"Always-on stdio families: treesitter (list_symbols, " +
			"search_symbols_by_pattern, get_supported_languages), shell " +
			"(agent_shell), skills, docs (list_docs / get_doc / list_catalog), " +
			"cobra runner (list_ycode_commands / run_ycode_command{,_workspace}), " +
			"document extractor (extract_document), repomap (build_repomap, " +
			"repomap_for_files), codegraph (graph_*), podman sandbox " +
			"(sandbox_exec), GitHub (github_*), browser (browser_*), Ollama " +
			"(ollama_*), and — when the memex store is reachable — memex " +
			"(memex_*, search_memex, list_memory_types).\n\n" +
			"Loom workspaces and observability tools (promql_*, query_logs, " +
			"query_traces, etc.) are HTTP-only; reach them via the composite " +
			"endpoint that `ycode serve` mounts at /mcp/.\n\n" +
			"Default permission ceiling: danger-full-access — agents that " +
			"intentionally configure ycode mcp serve in their settings.json have " +
			"opted into ycode's full capability surface. Lower with " +
			"--permission=read-only or workspace-write for sandboxed integrations.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			origin.SetAgentTool(origin.ToolMCPServe)
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			// Self-instrumentation: agent harnesses spawn one of
			// these per session (~150MB resident each); the sampler
			// makes a ballooning instance name itself in the logs
			// instead of silently joining the next OOM pile-up.
			memwatch.Start(ctx, "ycode-mcp-serve", nil, nil)

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

			handlers := []mcp.ServerHandler{
				treesitter.NewMCPHandler(),
				shell.NewMCPHandler(shellRT),
				skills.NewMCPHandler(),

				// Agent-facing capability prompts. Exposes list_docs +
				// get_doc; resources/list surfaces one ycode://docs/<slug>
				// per topic plus the curated index. Pure-read,
				// stateless, embedded — safe in every permission tier.
				// See internal/docs/embed.go for the curation contract.
				docs.NewMCPHandler(),

				// Cobra→MCP runner. Exposes list_ycode_commands +
				// run_ycode_command{,_workspace}, gating each invocation
				// against an explicit per-verb allowlist (see
				// cmd/ycode/cmdmcp.go safeguards). Lets foreign agents
				// call `ycode doctor`, `ycode model list`, `ycode docs`,
				// etc. without shelling out via agent_shell.
				newCobraMCPHandler(),

				// Stateless document extractor (PDF/DOCX/XLSX/PPTX/CSV).
				// The provider-backed extract_json sibling is HTTP-only
				// (see serve.go) because stdio doesn't construct a provider.
				extractmcp.NewDocumentHandler(),

				// Family A.2: repomap. Token-budgeted file→symbol
				// overview. Stateless — each call walks the tree and
				// re-parses; foreign agents typically call once early
				// for system-prompt seeding.
				repomap.NewMCPHandler(),

				// Family A.4: codegraph. gfy-backed code knowledge
				// graph. Loads the cached graph from
				// .agents/ycode/graph.json when present; rebuilds on
				// first call (or `force_rebuild`). Read-only — the
				// cache write is project-managed scratch.
				codegraph.NewMCPHandler(),

				// Family B: podman sandbox. Mirrors `yc sandbox` —
				// one-shot `podman run --rm` with cwd mounted at
				// /workspace, network disabled. DangerFullAccess
				// since arbitrary code runs inside the container.
				NewMCPHandler(),

				// Family E: GitHub. PRs / issues / CI checks via
				// go-github. Auth from GITHUB_TOKEN / GH_TOKEN /
				// ~/.config/gh/hosts.yml. Reads ReadOnly; create_*
				// tools WorkspaceWrite.
				gh.NewMCPHandler(),

				// Family F: browser automation. Exposed without a
				// client so foreign agents discover the 14 browser_*
				// tools — each call returns the friendly "configure
				// browser.mode" message until the operator wires a
				// backend. The HTTP /mcp/ variant in serve.go shares
				// the same client ycode's runtime uses so attach state
				// is unified; the stdio variant here is short-lived,
				// so we don't try to share with a serve process.
				browsermcp.NewMCPHandler(nil),
			}

			// Family A.3: memex memory. Best-effort — if the manager
			// can't be opened (no writable home dir, missing memory
			// tree, etc.) we just skip the family rather than fail
			// the whole serve. The other capabilities still work.
			if memMgr, err := openMemexForMCP(); err == nil {
				handlers = append(handlers, memexmcp.NewMCPHandler(memMgr))
			} else {
				slog.Warn("memexmcp disabled (memory manager unavailable)", "error", err)
			}

			// Family D: Ollama proxy. Same-machine HTTP. The handler
			// resolves its base URL via env / default — no precheck
			// here, since Ollama may come up after the MCP serve
			// process starts and tools must list even when Ollama is
			// down.
			handlers = append(handlers, inference.NewMCPHandler(""))

			composite := mcp.NewCompositeHandler(handlers...)
			composite.SetTransport("stdio")

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

// openMemexForMCP opens a memory.Manager rooted at the same paths
// newApp() uses: ~/.agents/ycode/memory (global) + <cwd>/.agents/ycode/memory
// (project). Kept private to the MCP entry point — the standalone
// `ycode mcp serve` process doesn't share newApp's full storage stack,
// so we re-derive the dirs here rather than depending on cli.App.
func openMemexForMCP() (*memory.Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("locate home dir: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getwd: %w", err)
	}
	globalDir := filepath.Join(home, ".agents", "ycode", "memory")
	projectDir := filepath.Join(cwd, ".agents", "ycode", "memory")
	return memory.NewManagerWithGlobal(globalDir, projectDir)
}
