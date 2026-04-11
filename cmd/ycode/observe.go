package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/observability"
	"github.com/qiangli/ycode/internal/runtime/config"
)

var observeCmd = &cobra.Command{
	Use:   "observe",
	Short: "Manage the built-in observability stack",
	Long:  "Start, stop, and manage the OTEL collector and Prometheus observability stack.",
}

var observeStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the observability stack (collector + Prometheus + dashboards)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, dirs, err := loadObserveConfig()
		if err != nil {
			return err
		}
		mgr := observability.NewStackManager(cfg, dirs.binDir, dirs.dataDir, dirs.otelDir)
		return mgr.Start(cmd.Context())
	},
}

var observeStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop all observability components",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, dirs, err := loadObserveConfig()
		if err != nil {
			return err
		}
		mgr := observability.NewStackManager(cfg, dirs.binDir, dirs.dataDir, dirs.otelDir)
		return mgr.Stop()
	},
}

var observeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of all observability components",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, dirs, err := loadObserveConfig()
		if err != nil {
			return err
		}
		mgr := observability.NewStackManager(cfg, dirs.binDir, dirs.dataDir, dirs.otelDir)

		proxyPort := cfg.ProxyPort
		if proxyPort == 0 {
			proxyPort = 58080
		}
		fmt.Printf("Observability Stack — http://127.0.0.1:%d/\n", proxyPort)

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "COMPONENT\tPID\tPORT\tPROXY PATH\tHEALTH")
		for _, s := range mgr.Status() {
			health := "unknown"
			if s.Healthy {
				health = "healthy"
			}
			fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\n", s.Name, s.PID, s.Port, s.ProxyPath, health)
		}
		return w.Flush()
	},
}

var observeDownloadCmd = &cobra.Command{
	Use:   "download",
	Short: "Pre-download all observability binaries",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, dirs, err := loadObserveConfig()
		if err != nil {
			return err
		}
		fmt.Println("Downloading observability binaries...")
		paths, err := observability.EnsureAllBinaries(cmd.Context(), dirs.binDir)
		if err != nil {
			return err
		}
		for name, path := range paths {
			fmt.Printf("  %s: %s\n", name, path)
		}
		fmt.Println("Done.")
		return nil
	},
}

var observeDashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Open the observability dashboard in browser",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _, err := loadObserveConfig()
		if err != nil {
			return err
		}
		port := cfg.ProxyPort
		if port == 0 {
			port = 58080
		}
		url := fmt.Sprintf("http://127.0.0.1:%d/", port)
		return openBrowser(url)
	},
}

var observeAlertsCmd = &cobra.Command{
	Use:   "alerts",
	Short: "Open the Karma alert dashboard in browser",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _, err := loadObserveConfig()
		if err != nil {
			return err
		}
		port := cfg.ProxyPort
		if port == 0 {
			port = 58080
		}
		url := fmt.Sprintf("http://127.0.0.1:%d/karma/", port)
		return openBrowser(url)
	},
}

var observeConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Print generated configs and port allocations",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, dirs, err := loadObserveConfig()
		if err != nil {
			return err
		}
		pa := observability.NewPortAllocator(dirs.dataDir)
		ports := pa.All()
		if len(ports) == 0 {
			fmt.Println("No port allocations found. Is the stack running?")
			return nil
		}
		fmt.Println("Port allocations:")
		for name, port := range ports {
			fmt.Printf("  %s: %d\n", name, port)
		}
		return nil
	},
}

var observeResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Remove all observability data (TSDB, logs, etc.)",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, dirs, err := loadObserveConfig()
		if err != nil {
			return err
		}
		fmt.Printf("This will remove all data in:\n  %s\n  %s\n", dirs.dataDir, dirs.otelDir)
		fmt.Print("Continue? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" {
			fmt.Println("Aborted.")
			return nil
		}
		if err := os.RemoveAll(dirs.dataDir); err != nil {
			return fmt.Errorf("remove %s: %w", dirs.dataDir, err)
		}
		if err := os.RemoveAll(dirs.otelDir); err != nil {
			return fmt.Errorf("remove %s: %w", dirs.otelDir, err)
		}
		fmt.Println("Observability data removed.")
		return nil
	},
}

type observeDirs struct {
	binDir  string
	dataDir string
	otelDir string
}

func loadObserveConfig() (*config.ObservabilityConfig, *observeDirs, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, fmt.Errorf("user home dir: %w", err)
	}

	cwd, _ := os.Getwd()
	loader := config.NewLoader(
		filepath.Join(home, ".config", "ycode"),
		filepath.Join(cwd, ".ycode"),
		filepath.Join(cwd, ".ycode"),
	)
	cfg, err := loader.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}

	obsCfg := cfg.Observability
	if obsCfg == nil {
		obsCfg = &config.ObservabilityConfig{}
	}

	ycodeDir := filepath.Join(home, ".ycode")
	dirs := &observeDirs{
		binDir:  filepath.Join(ycodeDir, "bin"),
		dataDir: filepath.Join(ycodeDir, "observability"),
		otelDir: filepath.Join(ycodeDir, "otel"),
	}
	if obsCfg.DataDir != "" {
		dirs.otelDir = obsCfg.DataDir
	}

	return obsCfg, dirs, nil
}

func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		fmt.Printf("Open %s in your browser\n", url)
		return nil
	}
	return exec.CommandContext(context.Background(), cmd, args...).Start()
}

var observeLogsCmd = &cobra.Command{
	Use:   "logs [component]",
	Short: "Tail log file for a specific observability component",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, dirs, err := loadObserveConfig()
		if err != nil {
			return err
		}
		component := args[0]
		logFile := filepath.Join(dirs.dataDir, component, component+".log")
		if _, err := os.Stat(logFile); os.IsNotExist(err) {
			// Try collector log.
			logFile = filepath.Join(dirs.otelDir, "collector", "otelcol.log")
			if component != "collector" && component != "otel-collector" {
				return fmt.Errorf("log file for %q not found", component)
			}
		}
		// Tail last 100 lines.
		data, err := os.ReadFile(logFile)
		if err != nil {
			return fmt.Errorf("read log: %w", err)
		}
		lines := splitLines(string(data))
		start := 0
		if len(lines) > 100 {
			start = len(lines) - 100
		}
		for _, line := range lines[start:] {
			fmt.Println(line)
		}
		return nil
	},
}

var observeAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Show recent conversation records from OTEL logs",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, dirs, err := loadObserveConfig()
		if err != nil {
			return err
		}
		lastN, _ := cmd.Flags().GetInt("last")
		if lastN <= 0 {
			lastN = 10
		}
		logsDir := filepath.Join(dirs.otelDir, "logs")
		entries, err := os.ReadDir(logsDir)
		if err != nil {
			return fmt.Errorf("read logs dir: %w (is observability enabled?)", err)
		}
		// Read from most recent file.
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
		// Show last N.
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

func splitLines(s string) []string {
	var lines []string
	for len(s) > 0 {
		i := 0
		for i < len(s) && s[i] != '\n' {
			i++
		}
		line := s[:i]
		if line != "" {
			lines = append(lines, line)
		}
		if i < len(s) {
			i++ // skip \n
		}
		s = s[i:]
	}
	return lines
}

func init() {
	observeAuditCmd.Flags().Int("last", 10, "Number of records to show")
	observeCmd.AddCommand(
		observeStartCmd,
		observeStopCmd,
		observeStatusCmd,
		observeDownloadCmd,
		observeDashboardCmd,
		observeAlertsCmd,
		observeConfigCmd,
		observeResetCmd,
		observeLogsCmd,
		observeAuditCmd,
	)
	rootCmd.AddCommand(observeCmd)
}
