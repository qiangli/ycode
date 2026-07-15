package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
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

	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/origin"
	"github.com/qiangli/ycode/internal/runtime/unattended"
	"github.com/qiangli/ycode/internal/selfinit"
	_ "github.com/qiangli/ycode/internal/shell/agentmode"
	_ "github.com/qiangli/ycode/internal/shell/builtins"
)

var (
	servePort   int
	serveDetach bool
	serveAuto   bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the ycode API, WebSocket, NATS, manifest, and debug server",
	Long: `Start the lean ycode server: HTTP/WebSocket API, optional embedded NATS,
manifest, and pprof debug surface. Observability is client-side OTEL export only; run an external
collector such as bashy otel when you want dashboards or local storage.`,
	RunE: runServe,
}

func runServe(cmd *cobra.Command, args []string) error {
	origin.SetAgentTool(origin.ToolServe)
	fullCfg, _, _, err := loadFullServeConfig()
	if err != nil {
		return err
	}
	if servePort <= 0 {
		servePort = selfinit.DefaultPort
	}
	if serveDetach {
		return detachServer()
	}
	return runAllServices(cmd.Context(), fullCfg)
}

var serveStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start ycode services (same as `ycode serve` with no subcommand)",
	RunE:  runServe,
}

var serveStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running server (idempotent when nothing is running)",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		pidPath := filepath.Join(home, ".agents", "ycode", "serve.pid")
		data, err := os.ReadFile(pidPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				fmt.Println("ycode serve not running (no PID file).")
				return nil
			}
			return fmt.Errorf("read PID file %s: %w", pidPath, err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			_ = os.Remove(pidPath)
			return fmt.Errorf("invalid PID in %s (file removed): %w", pidPath, err)
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("find process %d: %w", pid, err)
		}
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
	Short: "Show status of the server and endpoints",
	RunE: func(cmd *cobra.Command, args []string) error {
		port := servePort
		if port == 0 {
			port = selfinit.DefaultPort
		}
		home, _ := os.UserHomeDir()
		if !alreadyRunning(port) {
			pid := readServePID(home)
			alive := processAlive(pid)
			bound := !isPortAvailable(port)
			switch {
			case alive:
				fmt.Printf("ycode serve PID %d is running but its manifest endpoint is not responding on http://127.0.0.1:%d/\n", pid, port)
			case bound:
				fmt.Printf("Port %d is bound by another process.\n", port)
			default:
				fmt.Printf("ycode serve not running on http://127.0.0.1:%d/\n", port)
			}
			return nil
		}
		fmt.Printf("ycode serve running at http://127.0.0.1:%d/\n", port)
		if pid := readServePID(home); pid > 0 {
			fmt.Printf("PID: %d\n", pid)
		}
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
			if endpoints[name] != "" {
				fmt.Fprintf(w, "%s\t%s\n", name, endpoints[name])
			}
		}
		return w.Flush()
	},
}

var serveResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Remove file-backed OTEL data",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := ctxWithUnattendedFlag(cmd.Context(), cmd)
		home, _ := os.UserHomeDir()
		otelDir := filepath.Join(home, ".agents", "ycode", "otel")
		fmt.Printf("This will remove all data in:\n  %s\n", otelDir)
		if !unattended.IsUnattended(ctx) {
			fmt.Print("Continue? [y/N] ")
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}
		_ = os.RemoveAll(otelDir)
		fmt.Println("OTEL data removed.")
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
			return fmt.Errorf("read logs dir: %w (is OTEL logging enabled?)", err)
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

type stackComponents struct{}

func runAllServices(ctx context.Context, fullCfg *config.Config) error {
	home, _ := os.UserHomeDir()
	port := servePort
	if port == 0 {
		port = selfinit.DefaultPort
	}
	if alreadyRunning(port) {
		if pid := readServePID(home); pid > 0 {
			fmt.Printf("ycode serve already running (PID %d) at http://127.0.0.1:%d/\n", pid, port)
		} else {
			fmt.Printf("ycode serve already running at http://127.0.0.1:%d/\n", port)
		}
		return nil
	}
	if pid := readServePID(home); pid > 0 && processAlive(pid) {
		return fmt.Errorf("a ycode serve process (PID %d) is recorded as running but its manifest endpoint is not responding on port %d", pid, port)
	}
	_ = os.Remove(filepath.Join(home, ".agents", "ycode", "serve.pid"))

	mux := http.NewServeMux()
	var api *apiStack
	if !serveNoAPI || !serveNoNATS {
		var err error
		api, err = buildAPIStack(serveNoNATS)
		if err != nil {
			slog.Warn("API stack not available", "error", err)
			fmt.Printf("Web UI:            not available (%s)\n", err)
		} else {
			if !serveNoAPI && api.handler != nil {
				mux.Handle("/ycode/", http.StripPrefix("/ycode", api.handler))
				fmt.Printf("Web UI at          http://127.0.0.1:%d/ycode/\n", port)
			}
			if api.natsSrv != nil {
				fmt.Printf("NATS server at     nats://127.0.0.1:%d\n", apiNATSPort)
			}
		}
	}

	pidPath := filepath.Join(home, ".agents", "ycode", "serve.pid")
	portPath := filepath.Join(home, ".agents", "ycode", "serve.port")
	_ = os.MkdirAll(filepath.Dir(pidPath), 0o755)
	_ = os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644)
	_ = os.WriteFile(portPath, []byte(strconv.Itoa(port)), 0o644)
	defer os.Remove(pidPath)
	defer os.Remove(portPath)

	apiUp := api != nil && api.handler != nil
	stack := &stackComponents{}
	manifestData := buildServeManifest(home, port, apiNATSPort, stack, apiUp, version)
	if manifestPath, err := writeServeManifest(home, port, apiNATSPort, stack, apiUp, version); err != nil {
		slog.Warn("failed to write manifest", "error", err)
	} else {
		fmt.Printf("Manifest at        %s\n", manifestPath)
		defer os.Remove(manifestPath)
	}
	tokenFile := filepath.Join(home, ".agents", "ycode", "server.token")
	mux.Handle("/.well-known/ycode-manifest.json", manifestPublicHandler(manifestData))
	mux.Handle("/manifest", manifestFullHandler(manifestData, tokenFile, serveNoAuth))
	fmt.Printf("Manifest HTTP at   http://127.0.0.1:%d/.well-known/ycode-manifest.json (public), /manifest (authed)\n", port)

	mux.Handle("/debug/pprof/", debugPprofMux())
	fmt.Printf("Debug pprof at     http://127.0.0.1:%d/debug/pprof/\n", port)

	autoPath := filepath.Join(home, ".agents", "ycode", "serve.auto")
	if serveAuto {
		_ = os.WriteFile(autoPath, []byte(strconv.Itoa(os.Getpid())), 0o644)
		defer os.Remove(autoPath)
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		if api != nil {
			api.stop()
		}
		return fmt.Errorf("listen on 127.0.0.1:%d: %w", port, err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	fmt.Println("\nPress Ctrl+C to stop.")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	if serveAuto && api != nil {
		go func() {
			const idleTimeout = 5 * time.Minute
			const checkInterval = 30 * time.Second
			const initialGrace = 2 * time.Minute
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

	select {
	case <-sigCh:
	case err := <-errCh:
		if err != nil {
			if api != nil {
				api.stop()
			}
			return err
		}
	}

	fmt.Println("\nShutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	if api != nil {
		api.stop()
	}
	return nil
}

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

func readServePID(home string) int {
	data, err := os.ReadFile(filepath.Join(home, ".agents", "ycode", "serve.pid"))
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func isPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func detachServer() error {
	args := []string{"serve", "--detach=false"}
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
	if serveNoAuth {
		args = append(args, "--no-auth")
	}
	if serveNoPersona {
		args = append(args, "--no-persona")
	}
	if serveWorkspacePolicy != "" {
		args = append(args, "--workspace-policy", serveWorkspacePolicy)
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".agents", "ycode")
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
	dataDir := filepath.Join(home, ".agents", "ycode", "otel")
	return cfg, obsCfg, dataDir, nil
}

var pulseCmd = &cobra.Command{
	Use:   "pulse",
	Short: "Manage ycode's lightweight background server",
	Long:  "Pulse is now a compatibility alias for the lean ycode serve process. Observability dashboards run in an external collector such as bashy otel.",
}

func init() {
	serveCmd.PersistentFlags().IntVar(&servePort, "port", selfinit.DefaultPort, "Port for the ycode server")
	serveCmd.Flags().BoolVar(&serveDetach, "detach", true, "Run server in background (pass --detach=false to stay attached)")
	serveCmd.Flags().BoolVar(&serveAuto, "auto", false, "Auto-started by TUI (enables idle shutdown)")
	serveCmd.Flags().BoolVar(&serveNoAPI, "no-api", false, "Disable the API/WebSocket server")
	serveCmd.Flags().BoolVar(&serveNoNATS, "no-nats", false, "Disable the embedded NATS server")
	serveCmd.Flags().BoolVar(&serveNoAuth, "no-auth", false, "Disable Bearer-token authentication on the API (dev only)")
	serveCmd.Flags().BoolVar(&serveNoPersona, "no-persona", false, "Disable persona inference for shared/multi-tenant deployments")
	serveCmd.Flags().StringSliceVar(&serveToolsAllowlist, "tools-allowlist", nil, "Register only these built-in tool names (process-wide; mutually exclusive with --tools-blocklist)")
	serveCmd.Flags().StringSliceVar(&serveToolsBlocklist, "tools-blocklist", nil, "Register every built-in tool except these (process-wide; ignored when --tools-allowlist is set)")
	serveCmd.Flags().StringVar(&serveWorkspacePolicy, "workspace-policy", "per-session", "Web-session workspace policy: per-session (default), cwd (server startup dir).")
	_ = serveCmd.Flags().MarkHidden("auto")
	serveCmd.Flags().IntVar(&apiNATSPort, "nats-port", 4222, "Port for the embedded NATS server")
	serveStartCmd.Flags().AddFlagSet(serveCmd.Flags())
	serveAuditCmd.Flags().Int("last", 10, "Number of records to show")
	serveCmd.AddCommand(serveStartCmd, serveStopCmd, serveStatusCmd, serveResetCmd, serveAuditCmd)
	rootCmd.AddCommand(serveCmd)

	pulseStartCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the background ycode server",
		RunE: func(cmd *cobra.Command, args []string) error {
			serveDetach = true
			return detachServer()
		},
	}
	pulseStartCmd.Flags().IntVar(&servePort, "port", selfinit.DefaultPort, "Port for ycode server")
	pulseCmd.AddCommand(
		pulseStartCmd,
		&cobra.Command{Use: "stop", Short: "Stop the running server", RunE: serveStopCmd.RunE},
		&cobra.Command{Use: "status", Short: "Show server status", RunE: serveStatusCmd.RunE},
		&cobra.Command{Use: "reset", Short: "Remove file-backed OTEL data", RunE: serveResetCmd.RunE},
	)
	rootCmd.AddCommand(pulseCmd)
}
