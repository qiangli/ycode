package collector

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// Manager manages the OTEL Collector subprocess lifecycle.
type Manager struct {
	binDir     string // ~/.ycode/bin/
	dataDir    string // ~/.ycode/otel/collector/
	version    string
	cmd        *exec.Cmd
	configPath string
}

// NewManager creates a collector manager.
func NewManager(binDir, dataDir, version string) *Manager {
	return &Manager{
		binDir:  binDir,
		dataDir: dataDir,
		version: version,
	}
}

// Start downloads the binary if needed, writes the config, and starts the collector.
func (m *Manager) Start(ctx context.Context, cfg Config) error {
	binPath, err := EnsureBinary(ctx, m.binDir, m.version)
	if err != nil {
		return fmt.Errorf("ensure collector binary: %w", err)
	}

	configPath, err := WriteConfig(m.dataDir, cfg)
	if err != nil {
		return fmt.Errorf("write collector config: %w", err)
	}
	m.configPath = configPath

	logPath := filepath.Join(m.dataDir, "otelcol.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open collector log: %w", err)
	}

	m.cmd = exec.CommandContext(ctx, binPath, "--config", configPath)
	m.cmd.Stdout = logFile
	m.cmd.Stderr = logFile
	// Set process group so we can kill the whole group on stop.
	m.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := m.cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start collector: %w", err)
	}

	// Write PID file.
	pidPath := filepath.Join(m.dataDir, "otelcol.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(m.cmd.Process.Pid)), 0o644); err != nil {
		slog.Debug("collector: write pid file", "error", err)
	}

	slog.Info("collector: started", "pid", m.cmd.Process.Pid, "config", configPath)
	return nil
}

// Stop sends SIGTERM to the collector process and waits.
func (m *Manager) Stop() error {
	if m.cmd == nil || m.cmd.Process == nil {
		return nil
	}
	slog.Info("collector: stopping", "pid", m.cmd.Process.Pid)
	if err := m.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal collector: %w", err)
	}
	_ = m.cmd.Wait()

	// Remove PID file.
	pidPath := filepath.Join(m.dataDir, "otelcol.pid")
	os.Remove(pidPath)

	return nil
}

// Running returns true if the collector process is alive.
func (m *Manager) Running() bool {
	if m.cmd == nil || m.cmd.Process == nil {
		return false
	}
	// Check if process is still alive.
	return m.cmd.Process.Signal(syscall.Signal(0)) == nil
}

// PID returns the collector process PID, or 0 if not running.
func (m *Manager) PID() int {
	if m.cmd != nil && m.cmd.Process != nil {
		return m.cmd.Process.Pid
	}
	return 0
}
