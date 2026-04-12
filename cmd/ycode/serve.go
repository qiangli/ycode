package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/cluster"
	"github.com/qiangli/ycode/internal/collector"
	"github.com/qiangli/ycode/internal/observability"
	"github.com/qiangli/ycode/internal/runtime/config"
)

var (
	servePort   int
	serveDetach bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the embedded observability server",
	Long:  "Start the embedded OTEL collector, Prometheus, alertmanager, log store, and dashboard server.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, dataDir, err := loadServeConfig()
		if err != nil {
			return err
		}
		if servePort > 0 {
			cfg.ProxyPort = servePort
		}

		if serveDetach {
			return detachServer(cfg, dataDir)
		}

		return runServerForeground(cmd.Context(), cfg, dataDir)
	},
}

var serveStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running observability server",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		pidPath := filepath.Join(home, ".ycode", "serve.pid")
		data, err := os.ReadFile(pidPath)
		if err != nil {
			return fmt.Errorf("no server PID file found: %w", err)
		}
		pid, err := strconv.Atoi(string(data))
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
	Short: "Show status of the observability server and cluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, dataDir, err := loadServeConfig()
		if err != nil {
			return err
		}
		if servePort > 0 {
			cfg.ProxyPort = servePort
		}

		mgr := buildStackManager(cfg, dataDir)
		port := cfg.ProxyPort
		if port == 0 {
			port = 58080
		}
		fmt.Printf("Observability Server — http://127.0.0.1:%d/\n", port)

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "COMPONENT\tHEALTH")
		for _, s := range mgr.Status() {
			health := "unknown"
			if s.Healthy {
				health = "healthy"
			}
			fmt.Fprintf(w, "%s\t%s\n", s.Name, health)
		}
		w.Flush()

		// Show cluster info.
		home, _ := os.UserHomeDir()
		clusterDir := filepath.Join(home, ".ycode", "cluster")
		cl := cluster.New(clusterDir, "status-check", cluster.Options{})
		fmt.Println()

		if info, err := cl.LeaderInfo(); err == nil {
			fmt.Printf("Cluster Leader: %s (PID %d, port %d)\n", info.InstanceID, info.PID, info.Port)
		} else {
			fmt.Println("Cluster Leader: none")
		}

		if members, err := cl.Members(); err == nil && len(members) > 0 {
			fmt.Println()
			mw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(mw, "MEMBER\tPID\tROLE\tHEARTBEAT")
			for _, m := range members {
				fmt.Fprintf(mw, "%s\t%d\t%s\t%s\n",
					m.ID, m.PID, m.Role, m.Heartbeat.Format("15:04:05"))
			}
			mw.Flush()
		}
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
		dataDir := filepath.Join(home, ".ycode", "observability")
		otelDir := filepath.Join(home, ".ycode", "otel")
		clusterDir := filepath.Join(home, ".ycode", "cluster")

		fmt.Printf("This will remove all data in:\n  %s\n  %s\n  %s\n", dataDir, otelDir, clusterDir)
		fmt.Print("Continue? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" {
			fmt.Println("Aborted.")
			return nil
		}
		_ = os.RemoveAll(dataDir)
		_ = os.RemoveAll(otelDir)
		_ = os.RemoveAll(clusterDir)
		fmt.Println("Observability and cluster data removed.")
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
		logsDir := filepath.Join(home, ".ycode", "otel", "logs")
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

// runServerForeground starts the stack via cluster (as forced master) and blocks until interrupted.
func runServerForeground(ctx context.Context, cfg *config.ObservabilityConfig, dataDir string) error {
	home, _ := os.UserHomeDir()
	clusterDir := filepath.Join(home, ".ycode", "cluster")

	port := cfg.ProxyPort
	if port == 0 {
		port = 58080
	}

	var stackMgr *observability.StackManager

	cl := cluster.New(clusterDir, fmt.Sprintf("serve-%d", os.Getpid()), cluster.Options{
		NATSPort:    port + 100,
		ForceMaster: true,
		OnPromoted: func(ctx context.Context) error {
			mgr := buildStackManager(cfg, dataDir)
			if err := mgr.Start(ctx); err != nil {
				return err
			}
			stackMgr = mgr
			return nil
		},
		OnDemoted: func(ctx context.Context) error {
			if stackMgr != nil {
				return stackMgr.Stop(ctx)
			}
			return nil
		},
	})

	if err := cl.Join(ctx); err != nil {
		return fmt.Errorf("cluster join: %w", err)
	}

	fmt.Printf("ycode observability server running at http://127.0.0.1:%d/\n", port)
	fmt.Println("Press Ctrl+C to stop.")

	// Write PID file.
	pidPath := filepath.Join(home, ".ycode", "serve.pid")
	_ = os.MkdirAll(filepath.Dir(pidPath), 0o755)
	_ = os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644)
	defer os.Remove(pidPath)

	// Wait for signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nShutting down...")
	if err := cl.Leave(context.Background()); err != nil {
		slog.Warn("cluster: leave on shutdown", "error", err)
	}
	return nil
}

// detachServer forks the current process as a background server.
func detachServer(cfg *config.ObservabilityConfig, dataDir string) error {
	// Re-exec ourselves with the same args minus --detach.
	args := []string{"serve"}
	if servePort > 0 {
		args = append(args, "--port", strconv.Itoa(servePort))
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	// Create log file for detached process.
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".ycode", "observability")
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

// buildStackManager creates and configures a StackManager with all embedded components.
// All components run as goroutines — no external processes.
// Start order: VictoriaLogs → Jaeger → Collector → Prometheus → Alertmanager → Perses
func buildStackManager(cfg *config.ObservabilityConfig, dataDir string) *observability.StackManager {
	mgr := observability.NewStackManager(cfg, dataDir)

	// 1. VictoriaLogs (log sink — must be up before collector sends logs).
	vlogsPort := 9428
	mgr.AddComponent(observability.NewVictoriaLogsComponent(vlogsPort, filepath.Join(dataDir, "vlogs")))

	// 2. Jaeger (trace sink — must be up before collector sends traces).
	jaegerOTLPPort := 14317
	jaegerQueryPort := 16686
	mgr.AddComponent(observability.NewJaegerComponent(jaegerOTLPPort, jaegerQueryPort, filepath.Join(dataDir, "jaeger")))

	// 3. OTEL Collector (routes: metrics→Prometheus, logs→VictoriaLogs, traces→Jaeger).
	collCfg := collector.Config{
		GRPCPort:               4317,
		HTTPPort:               4318,
		PrometheusPort:         8889,
		VictoriaLogsPort:       vlogsPort,
		VictoriaLogsPathPrefix: "/logs",
		JaegerOTLPPort:         jaegerOTLPPort,
	}
	mgr.AddComponent(collector.NewEmbeddedCollector(collCfg, filepath.Join(dataDir, "collector")))

	// 4. Prometheus (scrapes collector's /metrics endpoint).
	mgr.AddComponent(observability.NewPrometheusComponent(
		filepath.Join(dataDir, "prometheus"),
		fmt.Sprintf("127.0.0.1:%d", collCfg.PrometheusPort),
	))

	// 5. Alertmanager.
	mgr.AddComponent(observability.NewAlertmanagerComponent())

	// 6. Perses dashboards (queries embedded Prometheus).
	persesPort := 18080
	mgr.AddComponent(observability.NewPersesComponent(
		persesPort,
		fmt.Sprintf("http://127.0.0.1:%d", collCfg.PrometheusPort),
		filepath.Join(dataDir, "perses"),
	))

	return mgr
}

func loadServeConfig() (*config.ObservabilityConfig, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", fmt.Errorf("user home dir: %w", err)
	}

	cwd, _ := os.Getwd()
	loader := config.NewLoader(
		filepath.Join(home, ".config", "ycode"),
		filepath.Join(cwd, ".ycode"),
		filepath.Join(cwd, ".ycode"),
	)
	cfg, err := loader.Load()
	if err != nil {
		return nil, "", fmt.Errorf("load config: %w", err)
	}

	obsCfg := cfg.Observability
	if obsCfg == nil {
		obsCfg = &config.ObservabilityConfig{}
	}

	dataDir := filepath.Join(home, ".ycode", "observability")
	return obsCfg, dataDir, nil
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 58080, "Port for the observability server")
	serveCmd.Flags().BoolVar(&serveDetach, "detach", false, "Run server in background")

	serveAuditCmd.Flags().Int("last", 10, "Number of records to show")

	serveCmd.AddCommand(serveStopCmd, serveStatusCmd, serveDashboardCmd, serveResetCmd, serveAuditCmd)
	rootCmd.AddCommand(serveCmd)
}
