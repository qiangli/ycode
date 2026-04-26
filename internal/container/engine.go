// Package container provides embedded Podman-based container management for ycode.
// ycode embeds Podman's Go container management layer to create, manage, and
// monitor isolated execution environments for agent swarms.
package container

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Engine wraps the Podman container management layer.
// It discovers the Podman socket (or starts a Podman service if needed)
// and provides container lifecycle operations.
type Engine struct {
	binaryPath string
	socketPath string
	apiURL     string

	healthy atomic.Bool
	mu      sync.Mutex
	cmd     *exec.Cmd // if we started our own podman service
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewEngine creates a container engine. It discovers the Podman binary and
// connects to an existing Podman socket or starts a new Podman service.
func NewEngine(ctx context.Context, cfg *EngineConfig) (*Engine, error) {
	path, err := discoverBinary(cfg.BinaryPath)
	if err != nil {
		return nil, fmt.Errorf("podman not found: %w (install podman or set container.binaryPath in config)", err)
	}

	e := &Engine{
		binaryPath: path,
		done:       make(chan struct{}),
	}

	// Try existing socket first.
	socketPath := cfg.SocketPath
	if socketPath == "" {
		socketPath = defaultSocketPath()
	}

	if socketPath != "" && socketAvailable(socketPath) {
		e.socketPath = socketPath
		e.apiURL = "unix://" + socketPath
		e.healthy.Store(true)
		slog.Info("container: connected to existing podman socket", "socket", socketPath)
		return e, nil
	}

	// Start our own podman service on an ephemeral socket.
	if err := e.startService(ctx, cfg); err != nil {
		return nil, fmt.Errorf("start podman service: %w", err)
	}

	return e, nil
}

// EngineConfig holds configuration for the container engine.
type EngineConfig struct {
	BinaryPath string // explicit path to podman binary (optional)
	SocketPath string // explicit socket path (optional)
	DataDir    string // data directory for podman storage
}

// Run executes a podman CLI command and returns the combined output.
func (e *Engine) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, e.binaryPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("podman %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return out, nil
}

// RunJSON executes a podman CLI command with --format=json and unmarshals the result.
func (e *Engine) RunJSON(ctx context.Context, result any, args ...string) error {
	args = append(args, "--format=json")
	out, err := e.Run(ctx, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, result); err != nil {
		return fmt.Errorf("parse podman json output: %w", err)
	}
	return nil
}

// Healthy returns true if the engine can communicate with podman.
func (e *Engine) Healthy() bool {
	return e.healthy.Load()
}

// BinaryPath returns the discovered podman binary path.
func (e *Engine) BinaryPath() string {
	return e.binaryPath
}

// Close shuts down the engine and any managed podman service.
func (e *Engine) Close(ctx context.Context) error {
	e.healthy.Store(false)
	if e.cancel != nil {
		e.cancel()
		select {
		case <-e.done:
		case <-time.After(5 * time.Second):
		}
	}
	return nil
}

// startService starts a podman system service on an ephemeral Unix socket.
func (e *Engine) startService(ctx context.Context, cfg *EngineConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	dataDir := cfg.DataDir
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".agents", "ycode", "container")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("create container data dir: %w", err)
	}

	socketPath := filepath.Join(dataDir, "podman.sock")
	// Remove stale socket.
	os.Remove(socketPath)

	sctx, cancel := context.WithCancel(ctx)
	e.cancel = cancel

	cmd := exec.CommandContext(sctx, e.binaryPath, "system", "service",
		"--time=0", // no timeout, run until killed
		"unix://"+socketPath,
	)
	cmd.Stdout = os.Stderr // log podman output to stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start podman service: %w", err)
	}
	e.cmd = cmd
	e.socketPath = socketPath
	e.apiURL = "unix://" + socketPath

	// Wait for socket to become available.
	if err := waitForSocket(socketPath, 10*time.Second); err != nil {
		cancel()
		return fmt.Errorf("podman service did not start: %w", err)
	}

	e.healthy.Store(true)
	slog.Info("container: started podman service", "socket", socketPath)

	// Monitor process.
	go func() {
		defer close(e.done)
		err := cmd.Wait()
		e.healthy.Store(false)
		if err != nil && sctx.Err() == nil {
			slog.Warn("container: podman service exited unexpectedly", "error", err)
		}
	}()

	return nil
}

// discoverBinary finds the podman binary using the following priority:
//  1. Explicit path from config
//  2. $YCODE_CONTAINER_RUNTIME environment variable
//  3. Adjacent to the ycode binary: $(dirname ycode)/podman
//  4. System PATH: which podman
func discoverBinary(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit, nil
		}
		return "", fmt.Errorf("configured binary not found: %s", explicit)
	}

	if envPath := os.Getenv("YCODE_CONTAINER_RUNTIME"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}
	}

	// Adjacent to ycode binary.
	if exe, err := os.Executable(); err == nil {
		adjacent := filepath.Join(filepath.Dir(exe), "podman")
		if _, err := os.Stat(adjacent); err == nil {
			return adjacent, nil
		}
	}

	// System PATH.
	if path, err := exec.LookPath("podman"); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("podman binary not found in PATH")
}

// defaultSocketPath returns the default Podman user socket path.
func defaultSocketPath() string {
	// macOS: podman machine socket
	if uid := os.Getuid(); uid > 0 {
		socketPath := filepath.Join(os.TempDir(), fmt.Sprintf("podman-run-%d", uid), "podman", "podman.sock")
		if _, err := os.Stat(socketPath); err == nil {
			return socketPath
		}
	}

	// Linux: XDG runtime dir
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		socketPath := filepath.Join(xdg, "podman", "podman.sock")
		if _, err := os.Stat(socketPath); err == nil {
			return socketPath
		}
	}

	return ""
}

// socketAvailable checks if a Unix socket is accepting connections.
func socketAvailable(path string) bool {
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// waitForSocket polls until a Unix socket accepts connections or the timeout expires.
func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if socketAvailable(path) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("socket %s not available after %v", path, timeout)
}
