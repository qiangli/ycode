package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/container"
	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/projects"
	"github.com/qiangli/ycode/internal/inference"
	"github.com/qiangli/ycode/internal/observability"
	"github.com/qiangli/ycode/internal/observability/dashboards"
	"github.com/qiangli/ycode/internal/runtime/config"
	mcppkg "github.com/qiangli/ycode/internal/runtime/mcp"
	"github.com/qiangli/ycode/internal/runtime/origin"
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
	Short: "Stop the running server",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		pidPath := filepath.Join(home, ".agents", "ycode", "serve.pid")
		data, err := os.ReadFile(pidPath)
		if err != nil {
			return fmt.Errorf("no server PID file found: %w", err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			return fmt.Errorf("invalid PID: %w", err)
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("find process %d: %w", pid, err)
		}
		if err := proc.Signal(syscall.SIGTERM); err != nil {
			return fmt.Errorf("signal process %d: %w", pid, err)
		}
		os.Remove(pidPath)
		fmt.Printf("Sent SIGTERM to server (PID %d)\n", pid)
		return nil
	},
}

var serveStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of the server and components",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, dataDir, err := loadServeConfig()
		if err != nil {
			return err
		}
		if servePort > 0 {
			cfg.ProxyPort = servePort
		}

		stack, err := buildStackManager(cfg, dataDir, nil, nil, nil)
		if err != nil {
			return err
		}
		port := cfg.ProxyPort
		if port == 0 {
			port = 58080
		}
		fmt.Printf("Observability Server — http://127.0.0.1:%d/\n", port)

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "COMPONENT\tHEALTH")
		for _, s := range stack.mgr.Status() {
			health := "unknown"
			if s.Healthy {
				health = "healthy"
			}
			fmt.Fprintf(w, "%s\t%s\n", s.Name, health)
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

	// Check for an already-running instance.
	pidPath := filepath.Join(home, ".agents", "ycode", "serve.pid")
	if data, err := os.ReadFile(pidPath); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			if proc, err := os.FindProcess(pid); err == nil {
				// Signal 0 checks if process exists without actually signaling it.
				if proc.Signal(syscall.Signal(0)) == nil {
					return fmt.Errorf("another ycode server is already running (PID %d). Stop it with 'ycode serve stop' or 'ycode pulse stop'", pid)
				}
			}
		}
		// Stale PID file — clean it up.
		os.Remove(pidPath)
	}

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

		// Backlog reconciler (docs/backlog/ ↔ Gitea issues). Resolves the
		// project for the cwd and seeds Gitea from the markdown source-
		// of-truth. See docs/backlog.md.
		if cwd, err := os.Getwd(); err == nil {
			if reg, err := projects.NewRegistry(filepath.Join(home, ".agents", "ycode", "gitea")); err == nil {
				if proj, err := reg.Resolve(ctx, cwd); err == nil {
					if _, err := projects.EnsureRepo(ctx, giteaClient, proj); err == nil {
						if err := startBacklogReconciler(ctx, slog.Default(), cwd, giteaClient, proj); err != nil {
							slog.Warn("backlog: reconciler not started", "error", err)
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

		// Gitea MCP — expose git server API via MCP protocol for external AI agents.
		giteaMCPHandler := gitserver.NewGiteaMCPHandler(giteaClient)
		giteaMCPHTTP := observability.NewMCPHTTPHandler(giteaMCPHandler)
		giteaMCPComp := gitserver.NewGiteaMCPComponent(giteaMCPHTTP)
		if err := mgr.AddLateComponent(ctx, giteaMCPComp); err != nil {
			slog.Warn("Gitea MCP not available", "error", err)
		} else {
			fmt.Printf("Gitea MCP at       http://127.0.0.1:%d/gitea-mcp/\n", port)
			compositeMCP = append(compositeMCP, giteaMCPHandler)
		}

		// Loom — workspace-isolation substrate for foreign agentic tools.
		// Hands each foreign sub-agent an isolated clone+branch+author so
		// N parallel sub-agents converge through the merger/CI gate
		// without stepping on each other. See docs/loom.md.
		giteaDataDir := filepath.Join(dataDir, "gitea")
		loomComp, loomSvc, err := buildLoomComponent(ctx, giteaClient, token, giteaDataDir)
		if err != nil {
			slog.Warn("Loom not available", "error", err)
		} else {
			if err := mgr.AddLateComponent(ctx, loomComp); err != nil {
				slog.Warn("Loom MCP not available", "error", err)
			} else {
				fmt.Printf("Loom MCP at        http://127.0.0.1:%d/loom-mcp/\n", port)
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
			// Add MCP server — exposes the entire observability stack to external AI agents.
			proxyURL := fmt.Sprintf("http://127.0.0.1:%d", port)
			persesDBDir := filepath.Join(dataDir, "perses", "data")
			alertRulesDir := filepath.Join(home, ".agents", "ycode", "configs", "prometheus", "alerts")
			if err := dashboards.ProvisionAlertRules(alertRulesDir); err != nil {
				slog.Warn("failed to provision default alert rules", "error", err)
			}
			mcpHandler := observability.NewTelemetryHandler(proxyURL, persesDBDir, alertRulesDir)
			mcpHTTP := observability.NewMCPHTTPHandler(mcpHandler)
			mcpComp := observability.NewMCPComponent(mcpHTTP)
			if err := mgr.AddLateComponent(ctx, mcpComp); err != nil {
				slog.Warn("Pulse MCP not available", "error", err)
			} else {
				fmt.Printf("Pulse MCP at       http://127.0.0.1:%d/pulse/\n", port)
				compositeMCP = append(compositeMCP, mcpHandler)
			}

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

	// Composite MCP endpoint (G6) — single /mcp/ URL that fans out to every
	// registered capability family. This is the Agent OS "syscall interface":
	// every client (claude code, opencode, codex, gemini-cli, ycode's own
	// TUI/web UI) configures ONE MCP entry pointing here instead of one
	// entry per family. Tool name collisions across families would panic at
	// construction; current families (gitea, loom, pulse) use distinct
	// prefixes so this is safe. Backward-compat: individual mounts at
	// /gitea-mcp/, /loom-mcp/, /pulse/ remain available.
	if len(compositeMCP) > 0 {
		composite := mcppkg.NewCompositeHandler(compositeMCP...)
		compositeHTTP := observability.NewMCPHTTPHandler(composite)
		compositeComp := observability.NewMCPCompositeComponent(compositeHTTP)
		if err := mgr.AddLateComponent(ctx, compositeComp); err != nil {
			slog.Warn("Composite MCP not available", "error", err)
		} else {
			fmt.Printf("ycode MCP at       http://127.0.0.1:%d/mcp/\n", port)
		}
	}

	fmt.Println("\nPress Ctrl+C to stop.")

	// Write PID and port files for client discovery.
	pidPath = filepath.Join(home, ".agents", "ycode", "serve.pid")
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
