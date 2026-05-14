package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/container"
	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/backlog"
	"github.com/qiangli/ycode/internal/gitserver/projects"
	"github.com/qiangli/ycode/internal/inference"
	"github.com/qiangli/ycode/internal/observability"
	"github.com/qiangli/ycode/internal/observability/dashboards"
	"github.com/qiangli/ycode/internal/runtime/config"
	mcppkg "github.com/qiangli/ycode/internal/runtime/mcp"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/browsermcp"
	"github.com/qiangli/ycode/internal/runtime/origin"
	"github.com/qiangli/ycode/internal/runtime/skills"
	"github.com/qiangli/ycode/internal/runtime/treesitter"
	"github.com/qiangli/ycode/internal/runtime/widget"
	"github.com/qiangli/ycode/internal/shell"
	_ "github.com/qiangli/ycode/internal/shell/agentmode"
	_ "github.com/qiangli/ycode/internal/shell/builtins"
	"github.com/qiangli/ycode/internal/tools"
	loompkg "github.com/qiangli/ycode/pkg/loom"
)

var (
	servePort   int
	serveDetach bool
	serveAuto   bool // auto-started by TUI; enables idle shutdown
)

// overrideGitServerURL is set by runAllServices to communicate the embedded
// Gitea URL to buildPromptContext (which runs later inside newApp).
var overrideGitServerURL string

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run all ycode services (observability, API, NATS)",
	Long: `Start all ycode services: observability stack (OTEL, Prometheus, Jaeger, dashboards),
HTTP/WebSocket API server (for web UI and remote clients), and embedded NATS server.

Use --no-api or --no-nats to disable specific services.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		origin.SetAgentTool(origin.ToolServe)
		fullCfg, obsCfg, dataDir, err := loadFullServeConfig()
		if err != nil {
			return err
		}
		if servePort > 0 {
			obsCfg.ProxyPort = servePort
		}

		if serveDetach {
			return detachServer(obsCfg, dataDir)
		}

		return runAllServices(cmd.Context(), fullCfg, obsCfg, dataDir)
	},
}

var serveStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running server (idempotent — no-op when nothing is running)",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		pidPath := filepath.Join(home, ".agents", "ycode", "serve.pid")
		data, err := os.ReadFile(pidPath)
		if err != nil {
			// Missing PID file is the common "already stopped" case.
			// Stay idempotent so `ycode serve stop` is safe to call
			// from shell aliases / scripts that don't know whether
			// the server is currently up.
			if errors.Is(err, os.ErrNotExist) {
				fmt.Println("ycode serve not running (no PID file).")
				return nil
			}
			return fmt.Errorf("read PID file %s: %w", pidPath, err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			// Garbage PID file — clean it up and report rather than
			// leave the user with an unactionable error.
			_ = os.Remove(pidPath)
			return fmt.Errorf("invalid PID in %s (file removed): %w", pidPath, err)
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("find process %d: %w", pid, err)
		}
		// Send SIGTERM. ESRCH means the process is already gone — the
		// PID file outlived the server (crash, reboot, manual kill).
		// Treat it the same as the missing-PID-file path: print + return
		// success, and reap the stale file.
		if err := proc.Signal(syscall.SIGTERM); err != nil {
			if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
				_ = os.Remove(pidPath)
				fmt.Printf("ycode serve not running (PID %d already gone; stale PID file removed).\n", pid)
				return nil
			}
			return fmt.Errorf("signal process %d: %w", pid, err)
		}
		_ = os.Remove(pidPath)
		fmt.Printf("Sent SIGTERM to server (PID %d)\n", pid)
		return nil
	},
}

var serveStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of the server and components",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, cfg, _, err := loadFullServeConfig()
		if err != nil {
			return err
		}
		if servePort > 0 {
			cfg.ProxyPort = servePort
		}
		port := cfg.ProxyPort
		if port == 0 {
			port = 58080
		}

		if !alreadyRunning(port) {
			fmt.Printf("ycode serve not running on http://127.0.0.1:%d/\n", port)
			return nil
		}

		home, _ := os.UserHomeDir()
		fmt.Printf("ycode serve running at http://127.0.0.1:%d/\n", port)
		if pid := readServePID(home); pid > 0 {
			fmt.Printf("PID: %d\n", pid)
		}

		// Read endpoints from the manifest the running server wrote.
		// Avoids re-constructing a stack manager here — which would
		// re-bind OTLP ports already held by the live process and fail
		// with "port 4317 already in use".
		endpoints, err := readManifestEndpoints(home)
		if err != nil {
			fmt.Printf("(manifest unavailable: %v)\n", err)
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "\nENDPOINT\tURL")
		names := make([]string, 0, len(endpoints))
		for k := range endpoints {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, name := range names {
			if endpoints[name] == "" {
				continue
			}
			fmt.Fprintf(w, "%s\t%s\n", name, endpoints[name])
		}
		w.Flush()
		return nil
	},
}

var serveDashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Open the dashboard in browser",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _, err := loadServeConfig()
		if err != nil {
			return err
		}
		port := cfg.ProxyPort
		if port == 0 {
			port = 58080
		}
		if servePort > 0 {
			port = servePort
		}
		return openBrowser(fmt.Sprintf("http://127.0.0.1:%d/dashboard/", port))
	},
}

var serveResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Remove all observability data",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		dataDir := filepath.Join(home, ".agents", "ycode", "observability")
		otelDir := filepath.Join(home, ".agents", "ycode", "otel")

		fmt.Printf("This will remove all data in:\n  %s\n  %s\n", dataDir, otelDir)
		fmt.Print("Continue? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" {
			fmt.Println("Aborted.")
			return nil
		}
		_ = os.RemoveAll(dataDir)
		_ = os.RemoveAll(otelDir)
		fmt.Println("Observability data removed.")
		return nil
	},
}

var serveAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Show recent conversation records",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		lastN, _ := cmd.Flags().GetInt("last")
		if lastN <= 0 {
			lastN = 10
		}
		logsDir := filepath.Join(home, ".agents", "ycode", "otel", "logs")
		entries, err := os.ReadDir(logsDir)
		if err != nil {
			return fmt.Errorf("read logs dir: %w (is observability enabled?)", err)
		}
		var allLines []string
		for i := len(entries) - 1; i >= 0 && len(allLines) < lastN; i-- {
			e := entries[i]
			if e.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(logsDir, e.Name()))
			if err != nil {
				continue
			}
			lines := splitLines(string(data))
			allLines = append(lines, allLines...)
		}
		start := 0
		if len(allLines) > lastN {
			start = len(allLines) - lastN
		}
		for _, line := range allLines[start:] {
			fmt.Println(line)
		}
		if len(allLines) == 0 {
			fmt.Println("No conversation records found.")
		}
		return nil
	},
}

// runAllServices starts the full ycode server stack:
// observability, API/WebSocket server, NATS server, and chat hub.
func runAllServices(ctx context.Context, fullCfg *config.Config, cfg *config.ObservabilityConfig, dataDir string) error {
	home, _ := os.UserHomeDir()

	port := cfg.ProxyPort
	if port == 0 {
		port = 58080
	}

	// Idempotency guard. Probing the public manifest endpoint is the
	// authoritative test — it survives PID reuse after a crash or
	// reboot, where a bare PID-liveness check would false-positive on an
	// unrelated process. Lets a `SessionStart` hook collapse from
	// `pgrep -f 'ycode serve' || ycode serve &` to just `ycode serve &`.
	if alreadyRunning(port) {
		if pid := readServePID(home); pid > 0 {
			fmt.Printf("ycode serve already running (PID %d) at http://127.0.0.1:%d/\n", pid, port)
		} else {
			fmt.Printf("ycode serve already running at http://127.0.0.1:%d/\n", port)
		}
		return nil
	}
	// Nothing is listening — drop any stale PID file from a prior crash
	// before we proceed to bind ports below.
	_ = os.Remove(filepath.Join(home, ".agents", "ycode", "serve.pid"))

	// 1. Build and start observability stack first (no dependencies on API).
	stack, err := buildStackManager(cfg, dataDir, fullCfg.Inference, fullCfg.Container, fullCfg.GitServer)
	if err != nil {
		return fmt.Errorf("build stack: %w", err)
	}
	mgr := stack.mgr
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("start observability stack: %w", err)
	}
	fmt.Printf("Observability at http://127.0.0.1:%d/\n", port)

	// Collected MCP sub-handlers fanned out by the composite /mcp/ endpoint
	// (G6). Each capability family (gitea, loom, pulse, future memex/repomap/
	// inference) appends its mcp.ServerHandler here. The composite is built
	// once at the bottom of this function, after every family has had its
	// chance to register.
	var compositeMCP []mcppkg.ServerHandler

	// Propagate the dynamically-allocated collector gRPC address so that
	// setupOTEL/setupFileOTEL (called inside newApp via buildAPIStack) connects
	// to the actual embedded collector instead of the default 127.0.0.1:4317.
	if stack.collectorAddr != "" {
		// Set package-level override for newApp → setupOTEL/setupFileOTEL.
		overrideCollectorAddr = stack.collectorAddr

		// Write a discovery file so standalone CLI processes can find the collector.
		addrPath := filepath.Join(home, ".agents", "ycode", "collector.addr")
		_ = os.WriteFile(addrPath, []byte(stack.collectorAddr), 0o644)
		defer os.Remove(addrPath)
	}

	// Wire Memos store for agent tools (if memos started successfully).
	if stack.memos != nil && stack.memos.Healthy() {
		tools.SetMemosStore(stack.memos.Store())
		fmt.Printf("Memos at           http://127.0.0.1:%d/memos/\n", port)
	}

	// Bonsai graph Explorer + DQL endpoint.
	if stack.bonsai != nil && stack.bonsai.Healthy() {
		fmt.Printf("Graph at           http://127.0.0.1:%d/graph/\n", port)
	}

	// Skill-router LLM warmup: when YCODE_SKILL_ROUTER_LLM_MODEL is
	// set, kick off a background pull of the rerank model so the first
	// routing request doesn't pay cold-pull latency. Best-effort —
	// failures are logged and the Cascade falls back to its TF-IDF
	// primary when the LLM isn't ready.
	warmSkillRouterModel(ctx, stack)

	// Wire Git server client for agent collaboration tools.
	if stack.gitServer != nil && stack.gitServer.Healthy() {
		token, err := resolveGitServerToken(ctx, home, fullCfg.GitServer.Token, stack.gitServer)
		if err != nil {
			slog.Warn("gitserver: token bootstrap failed; API calls will be unauthenticated", "error", err)
		}
		giteaClient := gitserver.NewClient(stack.gitServer.BaseURL(), token)
		tools.SetGitServer(giteaClient)
		overrideGitServerURL = fmt.Sprintf("http://127.0.0.1:%d/git/", port)
		fmt.Printf("Git server at      %s\n", overrideGitServerURL)

		// Backlog reconciler (backlog markdown ↔ Gitea issues). The backlog
		// lives at ~/.agents/ycode/projects/<id>/backlog/ — see
		// docs/backlog.md. Two checkouts of the same repo share one
		// backlog because the id is logical (git remote / explicit), not
		// keyed by cwd path.
		if cwd, err := os.Getwd(); err == nil {
			if reg, err := projects.NewRegistry(filepath.Join(home, ".agents", "ycode", "gitea")); err == nil {
				if proj, err := reg.Resolve(ctx, cwd); err == nil {
					if _, err := projects.EnsureRepo(ctx, giteaClient, proj); err == nil {
						bdir, derr := backlogDir()
						if derr != nil {
							slog.Warn("backlog: resolve dir", "error", derr)
						} else {
							if err := backlog.MigrateLegacy(filepath.Join(cwd, "docs", "backlog"), bdir, slog.Default()); err != nil {
								slog.Warn("backlog: legacy migration failed", "error", err)
							}
							if err := startBacklogReconciler(ctx, slog.Default(), bdir, giteaClient, proj); err != nil {
								slog.Warn("backlog: reconciler not started", "error", err)
							}
						}
						// Foreman state migration: <cwd>/.agents/ycode/foreman → user-home.
						if fdir, ferr := foremanDir(); ferr == nil {
							if err := migrateLegacyForemanDir(filepath.Join(cwd, ".agents", "ycode", "foreman"), fdir, slog.Default()); err != nil {
								slog.Warn("foreman: legacy migration failed", "error", err)
							}
						}
					} else {
						slog.Warn("backlog: ensure repo", "error", err)
					}
				} else {
					slog.Warn("backlog: project resolve", "error", err)
				}
			} else {
				slog.Warn("backlog: project registry", "error", err)
			}
		}

		// Discovery files for the `ycode tasks` / `ycode collab` CLIs to
		// find the live Gitea without parsing settings.json.
		_ = os.WriteFile(filepath.Join(home, ".agents", "ycode", "gitea.url"),
			[]byte(stack.gitServer.BaseURL()), 0o644)
		if token != "" {
			_ = os.WriteFile(filepath.Join(home, ".agents", "ycode", "gitea.token"),
				[]byte(token), 0o600)
		}

		// Gitea MCP handler — fanned out by the composite /mcp/ endpoint.
		giteaMCPHandler := gitserver.NewGiteaMCPHandler(giteaClient)
		compositeMCP = append(compositeMCP, giteaMCPHandler)

		// Loom — workspace-isolation substrate for foreign agentic tools.
		// Hands each foreign sub-agent an isolated clone+branch+author so
		// N parallel sub-agents converge through the merger/CI gate
		// without stepping on each other. See docs/loom.md. Registered as
		// a lifecycle component (merger pool, service close on shutdown);
		// its MCP handler is exposed via the composite /mcp/ endpoint, not
		// a per-family route.
		giteaDataDir := filepath.Join(dataDir, "gitea")
		loomComp, loomSvc, err := buildLoomComponent(ctx, giteaClient, token, giteaDataDir)
		if err != nil {
			slog.Warn("Loom not available", "error", err)
		} else {
			if err := mgr.AddLateComponent(ctx, loomComp); err != nil {
				slog.Warn("Loom not available", "error", err)
			} else {
				stack.loom = loomComp
				stack.loomSvc = loomSvc
				if h := loomComp.MCPHandler(); h != nil {
					compositeMCP = append(compositeMCP, h)
				}
			}
		}
	}

	// Experimental ycode-native browser modes (live / probe / solo).
	// The hub for live mode is durable — bind it here so the Chrome
	// extension stays connected across TUI/prompt lifecycles. The
	// TUI/prompt processes will see the port in use and forward via
	// HTTP /dispatch instead of trying to bind.
	//
	// We discard the returned browser.Client: tool dispatch happens
	// inside per-App newApp() flows, each of which calls
	// setupBrowserBackend and installs its own Client on its rootCtx.
	// This call's only purpose is the side effect of binding the live
	// hub port.
	_ = setupBrowserBackend(ctx, fullCfg)

	// 2. Build API/WebSocket + NATS (may take time or fail if no API key).
	var api *apiStack
	if !serveNoAPI || !serveNoNATS {
		var err error
		api, err = buildAPIStack(serveNoNATS)
		if err != nil {
			slog.Warn("API stack not available", "error", err)
			fmt.Printf("Web UI:            not available (%s)\n", err)
		} else {
			if !serveNoAPI && api.handler != nil {
				// Add web UI as a late component — the proxy is already running,
				// so we use AddLateComponent to register the handler on the mux.
				webComp := observability.NewWebUIComponent(api.handler)
				if err := mgr.AddLateComponent(ctx, webComp); err != nil {
					fmt.Printf("Web UI error: %v\n", err)
				} else {
					fmt.Printf("Web UI at          http://127.0.0.1:%d/ycode/\n", port)
				}
			}
			// Pulse telemetry handler — exposes the observability stack
			// (traces, logs, metrics, alerts, dashboards) via the composite
			// /mcp/ endpoint. No per-family HTTP route.
			proxyURL := fmt.Sprintf("http://127.0.0.1:%d", port)
			persesDBDir := filepath.Join(dataDir, "perses", "data")
			alertRulesDir := filepath.Join(home, ".agents", "ycode", "configs", "prometheus", "alerts")
			if err := dashboards.ProvisionAlertRules(alertRulesDir); err != nil {
				slog.Warn("failed to provision default alert rules", "error", err)
			}
			pulseHandler := observability.NewTelemetryHandler(proxyURL, persesDBDir, alertRulesDir)
			compositeMCP = append(compositeMCP, pulseHandler)

			if api.natsSrv != nil {
				fmt.Printf("NATS server at     nats://127.0.0.1:%d\n", apiNATSPort)
			}

			// 3. Start chat hub. Defaults to enabled when config is absent.
			chatCfg := fullCfg.Chat
			if chatCfg.IsEnabled() && api.natsSrv != nil {
				chatHub := buildChatHub(api.natsSrv.Conn(), chatCfg, filepath.Join(home, ".agents", "ycode", "chat"), api.svc)
				if err := mgr.AddLateComponent(ctx, chatHub); err != nil {
					slog.Warn("chat hub not available", "error", err)
				} else {
					fmt.Printf("Chat at            http://127.0.0.1:%d/chat/\n", port)
				}
			}
		}
	}

	// Always-on families (no live-stack dependency). Treesitter and skills
	// constructors are no-arg, stateless, and safe to share across requests
	// — same handlers wired into the stdio composite at cmd/ycode/mcp.go.
	// Shell exposes the agent_shell tool; HTTP callers thread their own
	// project root via the per-call `cwd` field (see internal/shell/mcpserver.go).
	shellRT, _ := shell.New(shell.Options{Permission: "danger-full-access"})
	compositeMCP = append(compositeMCP,
		treesitter.NewMCPHandler(),
		skills.NewMCPHandler(),
		shell.NewMCPHandler(shellRT),
	)

	// Browser automation family. Always registered so foreign agents
	// discover the tools — when no `browser.mode` is configured each
	// call returns the friendly "configure browser.mode" message
	// rather than a "tool not found" error. When a mode is set, the
	// same client backs both the in-process LLM tools and the public
	// MCP boundary, so attach state (probe Chrome, live extension
	// hub) is shared across both surfaces.
	compositeMCP = append(compositeMCP,
		browsermcp.NewMCPHandler(setupBrowserBackend(ctx, fullCfg)),
	)

	// Canvas / generative-UI tools. Foreign agents (claude-code, opencode,
	// codex, gemini-cli) and ycode's own runtime publish A2UI ops + iframe
	// widgets through these tools, routed onto the same in-process bus the
	// /canvas/ route subscribes to. Requires an api stack so we have a bus
	// to publish onto — if the API stack didn't come up (no provider, e.g.
	// air-gapped first-time run), skip silently.
	if api != nil && api.memBus != nil {
		compositeMCP = append(compositeMCP, widget.NewMCPHandler(api.memBus))

		// Alert → incident overlay hook: subscribes to EventAlertFired on
		// the same bus and renders a static-template incident overlay onto
		// the canvas-default session. Agent IS the SRE — alerts become
		// canvas events automatically, no Alertmanager UI click-through.
		// v1.5 swaps the static template for an agent-composed overlay
		// with correlated logs / traces / recent commits.
		widget.NewAlertHook(api.memBus, widget.DefaultSession).Start(ctx)

		// Service-health A2UI surface: first-class structured view that
		// auto-emits on canvas-default. Bootstraps the schema once and
		// refreshes data every 30s. The agent can call agent_render_a2ui
		// against the same surface ID to enrich the view (correlated
		// telemetry, runbook excerpts) without needing to redeclare the
		// component tree.
		widget.NewHealthHook(api.memBus, widget.DefaultSession, func(_ context.Context) widget.HealthData {
			return widget.HealthData{
				YcodeVersion: version,
				// alertsFiring / sessions are populated by agent enrichment in v1.5;
				// the static placeholder demonstrates the surface end-to-end.
				Incidents: []widget.HealthRow{},
				Deploys:   []widget.HealthRow{},
			}
		}).Start(ctx)
	}

	// Composite MCP endpoint (G6) — single /mcp/ URL that fans out to every
	// registered capability family. This is the Agent OS "syscall interface":
	// every client (claude code, opencode, codex, gemini-cli, ycode's own
	// TUI/web UI) configures ONE MCP entry pointing here. Tool name
	// collisions across families would panic at construction; current
	// families (treesitter, skills, shell, gitea, loom, pulse) use distinct
	// verbs or prefixes so this is safe.
	if len(compositeMCP) > 0 {
		composite := mcppkg.NewCompositeHandler(compositeMCP...)
		// Server-side ceiling. Mirrors `ycode mcp serve --permission ...`
		// at cmd/ycode/mcp.go. Independent of opencode/claude-code's
		// own per-agent permission policy — defense in depth.
		gateMode := parseMCPPermission(serveMCPPermission)
		gated := mcppkg.NewGatedHandler(composite, mcppkg.StaticGate{Ceiling: gateMode})
		compositeHTTP := observability.NewMCPHTTPHandler(gated)
		compositeComp := observability.NewMCPCompositeComponent(compositeHTTP)
		if err := mgr.AddLateComponent(ctx, compositeComp); err != nil {
			slog.Warn("Composite MCP not available", "error", err)
		} else {
			fmt.Printf("ycode MCP at       http://127.0.0.1:%d/mcp/  (ceiling: %s)\n", port, gateMode)
		}
	}

	fmt.Println("\nPress Ctrl+C to stop.")

	// Write PID and port files for client discovery.
	pidPath := filepath.Join(home, ".agents", "ycode", "serve.pid")
	portPath := filepath.Join(home, ".agents", "ycode", "serve.port")
	_ = os.MkdirAll(filepath.Dir(pidPath), 0o755)
	_ = os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644)
	_ = os.WriteFile(portPath, []byte(strconv.Itoa(port)), 0o644)
	defer os.Remove(pidPath)
	defer os.Remove(portPath)

	// Lighthouse manifest — single self-describing file foreign coding agents
	// read to discover every live ycode endpoint. See docs/lighthouse.md.
	// Same data is also served over HTTP (G7) for remote-first clients that
	// can't read the local filesystem.
	apiUp := api != nil && api.handler != nil
	manifestData := buildServeManifest(home, port, apiNATSPort, stack, apiUp, version)
	if manifestPath, err := writeServeManifest(home, port, apiNATSPort, stack, apiUp, version); err != nil {
		slog.Warn("failed to write manifest", "error", err)
	} else {
		fmt.Printf("Manifest at        %s\n", manifestPath)
		defer os.Remove(manifestPath)
	}
	// G7 — HTTP-served manifest endpoints. Remote clients call these instead
	// of reading ~/.agents/ycode/manifest.json from disk:
	//   GET /.well-known/ycode-manifest.json — public subset (URLs only)
	//   GET /manifest                        — full, bearer-authenticated
	tokenFile := filepath.Join(home, ".agents", "ycode", "server.token")
	mgr.AddHandler("/.well-known/ycode-manifest.json", manifestPublicHandler(manifestData))
	mgr.AddHandler("/manifest", manifestFullHandler(manifestData, tokenFile, serveNoAuth))
	fmt.Printf("Manifest HTTP at   http://127.0.0.1:%d/.well-known/ycode-manifest.json (public), /manifest (authed)\n", port)

	// If auto-started, write sentinel and enable idle shutdown.
	autoPath := filepath.Join(home, ".agents", "ycode", "serve.auto")
	if serveAuto {
		_ = os.WriteFile(autoPath, []byte(strconv.Itoa(os.Getpid())), 0o644)
		defer os.Remove(autoPath)
	}

	// Wait for signal (or idle timeout for auto-started servers).
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	if serveAuto && api != nil {
		// Auto-started servers shut down after idle timeout.
		go func() {
			const idleTimeout = 5 * time.Minute
			const checkInterval = 30 * time.Second
			const initialGrace = 2 * time.Minute

			// Give clients time to connect after server starts.
			time.Sleep(initialGrace)

			for {
				time.Sleep(checkInterval)
				if api.srv != nil && api.srv.ConnCount() == 0 && time.Since(api.srv.LastActivity()) > idleTimeout {
					slog.Info("auto-server idle timeout, shutting down")
					sigCh <- syscall.SIGTERM
					return
				}
			}
		}()
	}

	<-sigCh

	fmt.Println("\nShutting down...")
	if api != nil {
		api.stop()
	}
	if err := mgr.Stop(context.Background()); err != nil {
		slog.Warn("observability: stop error", "error", err)
	}
	return nil
}

// alreadyRunning reports whether a healthy ycode server is already
// listening on `port`. It probes the unauthenticated public manifest
// endpoint; a 200 there is definitive. Anything slower than the short
// timeout is treated as "not us" so an unrelated listener that grabbed
// the same port doesn't trick us into a false-positive no-op.
func alreadyRunning(port int) bool {
	client := &http.Client{Timeout: time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/.well-known/ycode-manifest.json", port)
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// readManifestEndpoints loads endpoints from ~/.agents/ycode/manifest.json,
// the lighthouse file written by the running server. Used by `serve status`
// to enumerate live URLs without spinning up a local stack manager (which
// would re-bind ports the running server already holds).
func readManifestEndpoints(home string) (map[string]string, error) {
	path := filepath.Join(home, ".agents", "ycode", "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m struct {
		Endpoints map[string]string `json:"endpoints"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m.Endpoints, nil
}

// readServePID is best-effort decoration for the "already running" log
// line — returns 0 on any read/parse failure rather than propagating.
func readServePID(home string) int {
	data, err := os.ReadFile(filepath.Join(home, ".agents", "ycode", "serve.pid"))
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

// detachServer forks the current process as a background server.
func detachServer(cfg *config.ObservabilityConfig, dataDir string) error {
	args := []string{"serve"}
	if servePort > 0 {
		args = append(args, "--port", strconv.Itoa(servePort))
	}
	if serveAuto {
		args = append(args, "--auto")
	}
	if serveNoAPI {
		args = append(args, "--no-api")
	}
	if serveNoNATS {
		args = append(args, "--no-nats")
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".agents", "ycode", "observability")
	_ = os.MkdirAll(logDir, 0o755)
	logFile, err := os.OpenFile(filepath.Join(logDir, "serve.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	attr := &os.ProcAttr{
		Dir:   ".",
		Files: []*os.File{os.Stdin, logFile, logFile},
		Sys:   &syscall.SysProcAttr{Setsid: true},
	}

	proc, err := os.StartProcess(exe, append([]string{filepath.Base(exe)}, args...), attr)
	if err != nil {
		logFile.Close()
		return fmt.Errorf("start background server: %w", err)
	}
	logFile.Close()

	fmt.Printf("Server started in background (PID %d)\n", proc.Pid)
	_ = proc.Release()
	return nil
}

// stackComponents holds references to key components for post-start wiring.
type stackComponents struct {
	mgr           *observability.StackManager
	memos         *observability.MemosComponent
	bonsai        *observability.BonsaiComponent
	ollama        *inference.OllamaComponent
	containers    *container.ContainerComponent
	gitServer     *gitserver.GitServerComponent
	loom          *loomComponent // workspace substrate; nil if gitserver disabled
	loomSvc       *loompkg.Service
	collectorAddr string // actual gRPC address of the embedded collector (e.g. "127.0.0.1:54321")
}

// buildStackManager creates and configures a StackManager with all embedded components.
// All internal ports are allocated dynamically to avoid conflicts when running
// multiple instances. Only the proxy port (--port) is user-specified.
func buildStackManager(cfg *config.ObservabilityConfig, dataDir string, inferCfg *config.InferenceConfig, containerCfg *config.ContainerConfig, gitServerCfg *config.GitServerConfig) (*stackComponents, error) {
	// Normalize nil sub-configs to empty structs. IsEnabled() is already
	// nil-safe (returns true on nil receiver per the default-on policy),
	// so the conditional branches enter — and then immediately dereferenced
	// nil pointers when reading fields like containerCfg.SocketPath. Empty
	// structs make IsEnabled() return the same default-true while making
	// every field read return its zero value. Mirrors the fix that landed
	// for `ycode serve status` (34fb72b) but applied at the constructor
	// so all callers stay safe.
	if inferCfg == nil {
		inferCfg = &config.InferenceConfig{}
	}
	if containerCfg == nil {
		containerCfg = &config.ContainerConfig{}
	}
	if gitServerCfg == nil {
		gitServerCfg = &config.GitServerConfig{}
	}

	mgr := observability.NewStackManager(cfg, dataDir)

	// Allocate ephemeral ports for all internal components.
	allocate := observability.AllocatePort
	vlogsPort, err := allocate()
	if err != nil {
		return nil, fmt.Errorf("victorialogs port: %w", err)
	}
	mgr.AddComponent(observability.NewVictoriaLogsComponent(vlogsPort, filepath.Join(dataDir, "vlogs")))

	jaegerOTLPPort, err := allocate()
	if err != nil {
		return nil, fmt.Errorf("jaeger otlp port: %w", err)
	}
	jaegerQueryPort, err := allocate()
	if err != nil {
		return nil, fmt.Errorf("jaeger query port: %w", err)
	}
	mgr.AddComponent(observability.NewJaegerComponent(jaegerOTLPPort, jaegerQueryPort, filepath.Join(dataDir, "jaeger")))

	// OTLP ingress ports are pinned to well-known defaults (4317 gRPC, 4318 HTTP)
	// so any standard OTLP client (third-party or ycode-internal) can publish
	// without lookup. Set OTLPGRPCPort / OTLPHTTPPort in observability config
	// to override; set to a negative value to fall back to ephemeral allocation.
	collGRPCPort, err := resolveOTLPPort(cfg.OTLPGRPCPort, 4317, "OTLP gRPC", allocate)
	if err != nil {
		return nil, err
	}
	collHTTPPort, err := resolveOTLPPort(cfg.OTLPHTTPPort, 4318, "OTLP HTTP", allocate)
	if err != nil {
		return nil, err
	}
	// Prometheus exporter port is internal scrape target — keep ephemeral.
	collPromPort, err := allocate()
	if err != nil {
		return nil, fmt.Errorf("collector prometheus port: %w", err)
	}
	collCfg := observability.CollectorConfig{
		GRPCPort:               collGRPCPort,
		HTTPPort:               collHTTPPort,
		PrometheusPort:         collPromPort,
		VictoriaLogsPort:       vlogsPort,
		VictoriaLogsPathPrefix: "/logs",
		JaegerOTLPPort:         jaegerOTLPPort,
	}
	coll := observability.NewEmbeddedCollector(collCfg, filepath.Join(dataDir, "collector"))
	mgr.AddComponent(coll)

	mgr.AddComponent(observability.NewPrometheusComponent(
		filepath.Join(dataDir, "prometheus"),
		fmt.Sprintf("127.0.0.1:%d", collCfg.PrometheusPort),
	))

	mgr.AddComponent(observability.NewAlertmanagerComponent())

	persesPort, err := allocate()
	if err != nil {
		return nil, fmt.Errorf("perses port: %w", err)
	}
	proxyPort := cfg.ProxyPort
	if proxyPort == 0 {
		proxyPort = 58080
	}
	mgr.AddComponent(observability.NewPersesComponent(
		persesPort,
		fmt.Sprintf("http://127.0.0.1:%d/prometheus", proxyPort),
		filepath.Join(dataDir, "perses"),
	))

	// Memos — persistent long-term memory storage.
	memosComp := observability.NewMemosComponent(filepath.Join(dataDir, "memos"))
	mgr.AddComponent(memosComp)

	// Bonsai — embeddable graph DB for memex DQL queries + Explorer UI.
	bonsaiComp := observability.NewBonsaiComponent(filepath.Join(dataDir, "graph"))
	mgr.AddComponent(bonsaiComp)

	// Ollama — local inference engine (optional managed runner).
	var ollamaComp *inference.OllamaComponent
	if inferCfg.IsEnabled() {
		ollamaComp = inference.NewOllamaComponent(inferCfg, filepath.Join(dataDir, "inference"))
		mgr.AddComponent(ollamaComp)
	} else {
		// Always register the Ollama management UI — auto-detects standalone Ollama.
		mgr.AddComponent(inference.NewOllamaUIComponent())
	}

	// Container isolation — Podman-based agent sandboxing (optional).
	var containerComp *container.ContainerComponent
	if containerCfg.IsEnabled() {
		containerComp = container.NewContainerComponent(
			&container.ComponentConfig{
				Enabled:      true,
				SocketPath:   containerCfg.SocketPath,
				Image:        containerCfg.Image,
				Network:      containerCfg.Network,
				ReadOnlyRoot: containerCfg.ReadOnlyRoot,
				PoolSize:     containerCfg.PoolSize,
				CPUs:         containerCfg.CPUs,
				Memory:       containerCfg.Memory,
			},
			filepath.Join(dataDir, "container"),
		)
		// Wire service ports for container environment injection.
		ollamaPort := 0
		if ollamaComp != nil {
			ollamaPort = ollamaComp.Port()
		}
		containerComp.SetServicePorts(ollamaPort, collGRPCPort, proxyPort)
		mgr.AddComponent(containerComp)
	}

	// Git server — embedded Gitea for agent coordination (optional).
	var gitComp *gitserver.GitServerComponent
	if gitServerCfg.IsEnabled() {
		gitComp = gitserver.NewGitServerComponent(&gitserver.ComponentConfig{
			Enabled:  true,
			DataDir:  gitServerCfg.DataDir,
			AppName:  gitServerCfg.AppName,
			HTTPOnly: gitServerCfg.HTTPOnly,
			Token:    gitServerCfg.Token,
		}, filepath.Join(dataDir, "gitea"))
		mgr.AddComponent(gitComp)
	}

	return &stackComponents{
		mgr:           mgr,
		memos:         memosComp,
		bonsai:        bonsaiComp,
		ollama:        ollamaComp,
		containers:    containerComp,
		gitServer:     gitComp,
		collectorAddr: coll.GRPCAddr(),
	}, nil
}

// resolveOTLPPort returns the OTLP receiver port to bind, applying this policy:
//   - configured > 0       → use that port; fail loud if unavailable.
//   - configured == 0      → use the well-known default; fail loud if unavailable.
//   - configured  < 0      → opt-in to ephemeral allocation.
//
// Pinning OTLP ingress to 4317/4318 is a hard requirement for the third-party
// OTLP-hub role: a publisher with no knowledge of ycode's port allocator must
// be able to discover ycode purely from the standard OTel default endpoints.
func resolveOTLPPort(configured, defaultPort int, label string, alloc func() (int, error)) (int, error) {
	if configured < 0 {
		p, err := alloc()
		if err != nil {
			return 0, fmt.Errorf("%s ephemeral port: %w", label, err)
		}
		return p, nil
	}
	port := configured
	if port == 0 {
		port = defaultPort
	}
	if !observability.IsPortAvailable(port) {
		return 0, fmt.Errorf("%s port %d already in use; configure observability.otlp%sPort to override or set negative to allocate ephemerally", label, port, "GRPC/HTTP")
	}
	return port, nil
}

// warmSkillRouterModel ensures both Ollama models the skill router
// can use — the embedder primary AND the LLM rerank — are pulled in
// the background before the first routing request arrives. No-op for
// any model env var that's unset.
//
// Models pulled:
//   - YCODE_SKILL_ROUTER_EMBED_MODEL (e.g. mxbai-embed-large:latest)
//   - YCODE_SKILL_ROUTER_LLM_MODEL   (e.g. qwen2.5:7b)
//
// Base URL precedence (shared across both pulls):
//  1. YCODE_SKILL_ROUTER_OLLAMA_BASE_URL (explicit override)
//  2. The managed Ollama runner, if stack.ollama is healthy
//  3. inference.DefaultOllamaURL() — respects OLLAMA_HOST, falls back
//     to http://127.0.0.1:11434
//
// Each pull is independent and best-effort: a failure on one doesn't
// affect the other, and a failure on either silently degrades the
// skill router to its TF-IDF lexical fallback. No 5xx ever leaks
// from a warmup failure — routing always produces an answer.
func warmSkillRouterModel(ctx context.Context, stack *stackComponents) {
	embedModel := os.Getenv("YCODE_SKILL_ROUTER_EMBED_MODEL")
	llmModel := os.Getenv("YCODE_SKILL_ROUTER_LLM_MODEL")
	if embedModel == "" && llmModel == "" {
		return
	}

	var baseURL string
	if v := os.Getenv("YCODE_SKILL_ROUTER_OLLAMA_BASE_URL"); v != "" {
		baseURL = v
	} else if stack.ollama != nil && stack.ollama.Healthy() {
		baseURL = stack.ollama.BaseURL()
	} else {
		baseURL = inference.DefaultOllamaURL()
	}

	if embedModel != "" {
		go pullSkillRouterModel(ctx, baseURL, embedModel, "embed")
	}
	if llmModel != "" {
		go pullSkillRouterModel(ctx, baseURL, llmModel, "rerank")
	}
}

// pullSkillRouterModel is the per-model body of warmSkillRouterModel.
// Lists the Ollama models, no-ops if the target is already present,
// else issues a pull. Logs at info/warn levels — no progress noise.
func pullSkillRouterModel(ctx context.Context, baseURL, model, role string) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	models, err := inference.OllamaListModels(probeCtx, baseURL)
	if err != nil {
		slog.Info("skill-router: ollama not reachable; skipping model warmup",
			"role", role, "base_url", baseURL, "model", model, "err", err)
		return
	}
	for _, m := range models {
		if m.Name == model {
			slog.Info("skill-router: model already present", "role", role, "model", model)
			return
		}
	}
	slog.Info("skill-router: pulling model in background",
		"role", role, "model", model, "base_url", baseURL)
	if err := inference.OllamaPullModel(ctx, baseURL, model, nil); err != nil {
		slog.Warn("skill-router: background pull failed",
			"role", role, "model", model, "err", err)
		return
	}
	slog.Info("skill-router: model ready", "role", role, "model", model)
}

func loadServeConfig() (*config.ObservabilityConfig, string, error) {
	_, obsCfg, dataDir, err := loadFullServeConfig()
	return obsCfg, dataDir, err
}

func loadFullServeConfig() (*config.Config, *config.ObservabilityConfig, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, "", fmt.Errorf("user home dir: %w", err)
	}

	cwd, _ := os.Getwd()
	loader := config.NewLoader(
		filepath.Join(home, ".config", "ycode"),
		filepath.Join(cwd, ".agents", "ycode"),
		filepath.Join(cwd, ".agents", "ycode"),
	)
	cfg, err := loader.Load()
	if err != nil {
		return nil, nil, "", fmt.Errorf("load config: %w", err)
	}

	obsCfg := cfg.Observability
	if obsCfg == nil {
		obsCfg = &config.ObservabilityConfig{}
	}

	dataDir := filepath.Join(home, ".agents", "ycode", "observability")
	return cfg, obsCfg, dataDir, nil
}

// pulseCmd manages the Pulse observability stack as a detached background process.
// 'pulse start' forks a detached 'ycode serve', 'pulse stop' sends SIGTERM.
// Each ycode CLI session auto-connects to the running collector.
var pulseCmd = &cobra.Command{
	Use:   "pulse",
	Short: "Manage the Pulse observability stack (start/stop/status)",
	Long: `Pulse is ycode's nervous system — traces, metrics, logs, dashboards, alerts,
and MCP server for external agent access.

'ycode pulse start' forks a detached 'ycode serve' process that stays
running across CLI sessions until explicitly stopped.

Subcommands:
  start       Start Pulse as a background process
  stop        Stop the running Pulse process
  status      Show Pulse component health
  dashboard   Open Pulse dashboard in browser
  reset       Remove all Pulse data

Each ycode CLI session auto-connects to the running Pulse collector.

Connect from Claude Code or any MCP client:
  {"mcpServers": {"ycode-pulse": {"url": "http://localhost:58080/pulse/"}}}`,
}

// parseMCPPermission maps the user-facing flag value to an mcp.PermissionMode.
// Unknown values fall back to DangerFullAccess with a warning so the server
// still starts; the operator can fix the flag without a restart loop.
func parseMCPPermission(s string) mcppkg.PermissionMode {
	switch s {
	case "read-only", "readonly":
		return mcppkg.ModeReadOnly
	case "workspace-write", "write":
		return mcppkg.ModeWorkspaceWrite
	case "", "danger-full-access", "full", "danger":
		return mcppkg.ModeDangerFullAccess
	default:
		slog.Warn("unknown --mcp-permission, defaulting to danger-full-access", "value", s)
		return mcppkg.ModeDangerFullAccess
	}
}

func init() {
	serveCmd.PersistentFlags().IntVar(&servePort, "port", 58080, "Port for the observability server")
	serveCmd.Flags().BoolVar(&serveDetach, "detach", false, "Run server in background")
	serveCmd.Flags().BoolVar(&serveAuto, "auto", false, "Auto-started by TUI (enables idle shutdown)")
	serveCmd.Flags().BoolVar(&serveNoAPI, "no-api", false, "Disable the API/WebSocket server")
	serveCmd.Flags().BoolVar(&serveNoNATS, "no-nats", false, "Disable the embedded NATS server")
	serveCmd.Flags().BoolVar(&serveNoAuth, "no-auth", false, "Disable Bearer-token authentication on the API (dev only)")
	serveCmd.Flags().BoolVar(&serveNoPersona, "no-persona", false, "Disable persona inference for shared/multi-tenant deployments")
	serveCmd.Flags().StringSliceVar(&serveToolsAllowlist, "tools-allowlist", nil, "Register only these built-in tool names (process-wide; mutually exclusive with --tools-blocklist)")
	serveCmd.Flags().StringSliceVar(&serveToolsBlocklist, "tools-blocklist", nil, "Register every built-in tool except these (process-wide; ignored when --tools-allowlist is set)")
	serveCmd.Flags().StringVar(&serveMCPPermission, "mcp-permission", "danger-full-access", "Server-side MCP permission ceiling: read-only | workspace-write | danger-full-access")
	_ = serveCmd.Flags().MarkHidden("auto")
	serveCmd.Flags().IntVar(&apiNATSPort, "nats-port", 4222, "Port for the embedded NATS server")

	serveAuditCmd.Flags().Int("last", 10, "Number of records to show")

	serveCmd.AddCommand(serveStopCmd, serveStatusCmd, serveDashboardCmd, serveResetCmd, serveAuditCmd)
	rootCmd.AddCommand(serveCmd)

	// Pulse manages a detached 'ycode serve' process.
	// 'pulse start' delegates to detachServer(), 'pulse stop' delegates to serveStopCmd.
	pulseStartCmd := &cobra.Command{
		Use:   "start",
		Short: "Start Pulse as a background process",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, obsCfg, dataDir, err := loadFullServeConfig()
			if err != nil {
				return err
			}
			if servePort > 0 {
				obsCfg.ProxyPort = servePort
			}
			return detachServer(obsCfg, dataDir)
		},
	}
	pulseStartCmd.Flags().IntVar(&servePort, "port", 58080, "Port for Pulse server")

	pulseCmd.AddCommand(
		pulseStartCmd,
		&cobra.Command{Use: "stop", Short: "Stop the running Pulse process", RunE: serveStopCmd.RunE},
		&cobra.Command{Use: "status", Short: "Show Pulse component health", RunE: serveStatusCmd.RunE},
		&cobra.Command{Use: "dashboard", Short: "Open Pulse dashboard in browser", RunE: serveDashboardCmd.RunE},
		&cobra.Command{Use: "reset", Short: "Remove all Pulse data", RunE: serveResetCmd.RunE},
	)
	rootCmd.AddCommand(pulseCmd)
}

// resolveGitServerToken returns a Gitea API token, in order of preference:
//  1. The explicit token from settings.json (if non-empty).
//  2. A persisted token from ~/.agents/ycode/gitea/admin.token.
//  3. A freshly bootstrapped token via EnsureAdmin + IssueToken,
//     persisted to admin.token for future starts.
//
// Returns "" + error only if bootstrap fails; serve.go logs the warning
// and continues with an unauthenticated client (broken, but at least the
// rest of the stack still boots).
func resolveGitServerToken(ctx context.Context, home, configToken string, comp *gitserver.GitServerComponent) (string, error) {
	if configToken != "" {
		return configToken, nil
	}
	persistPath := filepath.Join(home, ".agents", "ycode", "gitea", "admin.token")
	if data, err := os.ReadFile(persistPath); err == nil {
		if t := strings.TrimSpace(string(data)); t != "" {
			return t, nil
		}
	}
	tok, err := comp.Bootstrap(ctx, "admin", "admin@ycode.local", gitserver.RandomPassword(), "ycode-admin")
	if err != nil {
		return "", fmt.Errorf("gitserver bootstrap: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(persistPath), 0o755); err != nil {
		return tok, fmt.Errorf("persist admin token: %w", err)
	}
	if err := os.WriteFile(persistPath, []byte(tok), 0o600); err != nil {
		return tok, fmt.Errorf("persist admin token: %w", err)
	}
	return tok, nil
}
