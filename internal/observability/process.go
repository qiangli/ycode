package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// Process manages a child process lifecycle for an observability component.
type Process struct {
	Name       string
	BinaryPath string
	Args       []string
	Port       int
	DataDir    string // component-specific data dir
	HealthPath string // HTTP health check path, e.g. "/-/healthy"

	cmd     *exec.Cmd
	logFile *os.File
}

// Start launches the process. It binds to 127.0.0.1 on the configured port.
func (p *Process) Start(ctx context.Context) error {
	if err := os.MkdirAll(p.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir for %s: %w", p.Name, err)
	}

	logPath := filepath.Join(p.DataDir, p.Name+".log")
	var err error
	p.logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log for %s: %w", p.Name, err)
	}

	p.cmd = exec.CommandContext(ctx, p.BinaryPath, p.Args...)
	p.cmd.Stdout = p.logFile
	p.cmd.Stderr = p.logFile
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := p.cmd.Start(); err != nil {
		p.logFile.Close()
		return fmt.Errorf("start %s: %w", p.Name, err)
	}

	// Write PID file.
	pidPath := filepath.Join(p.DataDir, p.Name+".pid")
	_ = os.WriteFile(pidPath, []byte(strconv.Itoa(p.cmd.Process.Pid)), 0o644)

	slog.Info("observability: started", "component", p.Name, "pid", p.cmd.Process.Pid, "port", p.Port)
	return nil
}

// Stop sends SIGTERM and waits for the process to exit.
func (p *Process) Stop() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	slog.Info("observability: stopping", "component", p.Name, "pid", p.cmd.Process.Pid)
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	_ = p.cmd.Wait()
	if p.logFile != nil {
		p.logFile.Close()
	}
	// Remove PID file.
	pidPath := filepath.Join(p.DataDir, p.Name+".pid")
	os.Remove(pidPath)
	return nil
}

// Healthy checks the HTTP health endpoint.
func (p *Process) Healthy(ctx context.Context) bool {
	if p.Port == 0 {
		return false
	}
	healthURL := fmt.Sprintf("http://127.0.0.1:%d%s", p.Port, p.HealthPath)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// PID returns the process PID, or 0 if not running.
func (p *Process) PID() int {
	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Pid
	}
	return 0
}

// Running returns true if the process is still alive.
func (p *Process) Running() bool {
	if p.cmd == nil || p.cmd.Process == nil {
		return false
	}
	return p.cmd.Process.Signal(syscall.Signal(0)) == nil
}
