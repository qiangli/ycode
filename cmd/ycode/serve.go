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

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/collector"
	"github.com/qiangli/ycode/internal/memos"
	"github.com/qiangli/ycode/internal/observability"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/tools"
)

var (
	servePort   int
	serveDetach bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run all ycode services (observability, API, NATS)",
	Long: `Start all ycode services: observability stack (OTEL, Prometheus, Jaeger, dashboards),
HTTP/WebSocket API server (for web UI and remote clients), and embedded NATS server.

Use --no-api or --no-nats to disable specific services.`,
	RunE: func(cmd *cobra.Command, args []string) error {
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

		stack := buildStackManager(cfg, dataDir)
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

	// 1. Build and start observability stack first (no dependencies on API).
	stack := buildStackManager(cfg, dataDir)
	mgr := stack.mgr
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("start observability stack: %w", err)
	}
	fmt.Printf("Observability at http://127.0.0.1:%d/\n", port)

	// Wire Memos client for agent tools (if memos started successfully).
	if stack.memos != nil && stack.memos.Healthy() {
		mc := memos.NewClient(fmt.Sprintf("http://%s", stack.memos.MemosAddr()))
		tools.SetMemosClient(mc)
		fmt.Printf("Memos at           http://127.0.0.1:%d/memos/\n", port)
	}

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
			mcpHandler := observability.NewTelemetryHandler(proxyURL, persesDBDir, alertRulesDir)
			mcpHTTP := observability.NewMCPHTTPHandler(mcpHandler)
			mcpComp := observability.NewMCPComponent(mcpHTTP)
			if err := mgr.AddLateComponent(ctx, mcpComp); err != nil {
				slog.Warn("Pulse MCP not available", "error", err)
			} else {
				fmt.Printf("Pulse MCP at       http://127.0.0.1:%d/pulse/\n", port)
			}

			if api.natsSrv != nil {
				fmt.Printf("NATS server at     nats://127.0.0.1:%d\n", apiNATSPort)
			}

			// 3. Start chat hub. Defaults to enabled when config is absent.
			chatCfg := fullCfg.Chat
			if chatCfg == nil {
				chatCfg = &config.ChatConfig{Enabled: true}
			}
			if chatCfg.Enabled && api.natsSrv != nil {
				chatHub := buildChatHub(api.natsSrv.Conn(), chatCfg, filepath.Join(home, ".agents", "ycode", "chat"), api.svc)
				if err := mgr.AddLateComponent(ctx, chatHub); err != nil {
					slog.Warn("chat hub not available", "error", err)
				} else {
					fmt.Printf("Chat at            http://127.0.0.1:%d/chat/\n", port)
				}
			}
		}
	}

	fmt.Println("\nPress Ctrl+C to stop.")

	// Write PID file.
	pidPath := filepath.Join(home, ".agents", "ycode", "serve.pid")
	_ = os.MkdirAll(filepath.Dir(pidPath), 0o755)
	_ = os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644)
	defer os.Remove(pidPath)

	// Wait for signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
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
	mgr   *observability.StackManager
	memos *observability.MemosComponent
}

// buildStackManager creates and configures a StackManager with all embedded components.
func buildStackManager(cfg *config.ObservabilityConfig, dataDir string) *stackComponents {
	mgr := observability.NewStackManager(cfg, dataDir)

	vlogsPort := 9428
	mgr.AddComponent(observability.NewVictoriaLogsComponent(vlogsPort, filepath.Join(dataDir, "vlogs")))

	jaegerOTLPPort := 14317
	jaegerQueryPort := 16686
	mgr.AddComponent(observability.NewJaegerComponent(jaegerOTLPPort, jaegerQueryPort, filepath.Join(dataDir, "jaeger")))

	collCfg := collector.Config{
		GRPCPort:               4317,
		HTTPPort:               4318,
		PrometheusPort:         8889,
		VictoriaLogsPort:       vlogsPort,
		VictoriaLogsPathPrefix: "/logs",
		JaegerOTLPPort:         jaegerOTLPPort,
	}
	mgr.AddComponent(collector.NewEmbeddedCollector(collCfg, filepath.Join(dataDir, "collector")))

	mgr.AddComponent(observability.NewPrometheusComponent(
		filepath.Join(dataDir, "prometheus"),
		fmt.Sprintf("127.0.0.1:%d", collCfg.PrometheusPort),
	))

	mgr.AddComponent(observability.NewAlertmanagerComponent())

	persesPort := 18080
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

	return &stackComponents{mgr: mgr, memos: memosComp}
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

// pulseCmd is the branded alias for serve — starts the ycode Pulse observability hub.
var pulseCmd = &cobra.Command{
	Use:   "pulse",
	Short: "Start ycode Pulse — the observability and agent coordination hub",
	Long: `Pulse is ycode's nervous system — traces, metrics, logs, dashboards, alerts,
and MCP server for external agent access, all in one command.

Services started:
  Traces       Jaeger for distributed tracing
  Metrics      Prometheus + Perses dashboards
  Logs         VictoriaLogs for structured log search
  Alerts       Alertmanager for alert routing
  MCP          Model Context Protocol server at /pulse/
  API          HTTP/WebSocket server + web UI
  NATS         Message bus for multi-instance coordination

Connect from Claude Code or any MCP client:
  {"mcpServers": {"ycode-pulse": {"url": "http://localhost:58080/pulse/"}}}

This is equivalent to 'ycode serve'.`,
	RunE: serveCmd.RunE,
}

func init() {
	serveCmd.PersistentFlags().IntVar(&servePort, "port", 58080, "Port for the observability server")
	serveCmd.Flags().BoolVar(&serveDetach, "detach", false, "Run server in background")
	serveCmd.Flags().BoolVar(&serveNoAPI, "no-api", false, "Disable the API/WebSocket server")
	serveCmd.Flags().BoolVar(&serveNoNATS, "no-nats", false, "Disable the embedded NATS server")
	serveCmd.Flags().IntVar(&apiNATSPort, "nats-port", 4222, "Port for the embedded NATS server")

	serveAuditCmd.Flags().Int("last", 10, "Number of records to show")

	serveCmd.AddCommand(serveStopCmd, serveStatusCmd, serveDashboardCmd, serveResetCmd, serveAuditCmd)
	rootCmd.AddCommand(serveCmd)

	// Pulse is the branded alias — shares flags and subcommands with serve.
	pulseCmd.PersistentFlags().IntVar(&servePort, "port", 58080, "Port for Pulse server")
	pulseCmd.Flags().BoolVar(&serveDetach, "detach", false, "Run server in background")
	pulseCmd.Flags().BoolVar(&serveNoAPI, "no-api", false, "Disable the API/WebSocket server")
	pulseCmd.Flags().BoolVar(&serveNoNATS, "no-nats", false, "Disable the embedded NATS server")
	pulseCmd.Flags().IntVar(&apiNATSPort, "nats-port", 4222, "Port for the embedded NATS server")
	pulseCmd.AddCommand(
		&cobra.Command{Use: "stop", Short: "Stop Pulse", RunE: serveStopCmd.RunE},
		&cobra.Command{Use: "status", Short: "Show Pulse component health", RunE: serveStatusCmd.RunE},
		&cobra.Command{Use: "dashboard", Short: "Open Pulse dashboard in browser", RunE: serveDashboardCmd.RunE},
		&cobra.Command{Use: "reset", Short: "Remove all Pulse data", RunE: serveResetCmd.RunE},
	)
	rootCmd.AddCommand(pulseCmd)
}
