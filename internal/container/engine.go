// Package container provides embedded Podman-based container management for ycode.
// All operations use Podman's Go libraries and REST API bindings — no external
// podman binary is called. The Podman service runs either in-process (Linux)
// or via a managed VM (macOS/Windows).
package container

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qiangli/ycode/pkg/oci/bindings"
)

// Engine wraps the Podman container management layer.
// All container operations use the REST API bindings (pure Go).
// The engine connects to an existing socket, starts an in-process
// service (Linux), or auto-provisions a VM (macOS).
type Engine struct {
	connCtx    context.Context // connection context for REST API bindings
	socketPath string
	apiURL     string

	healthy atomic.Bool
	mu      sync.Mutex

	cancel context.CancelFunc
	done   chan struct{}
}

// NewEngine creates a container engine. It connects to an existing Podman
// socket, starts an in-process service (Linux), or auto-provisions a VM
// (macOS/Windows). No external podman binary is required.
func NewEngine(ctx context.Context, cfg *EngineConfig) (*Engine, error) {
	e := &Engine{
		done: make(chan struct{}),
	}

	// 1. Try explicit or existing socket.
	socketPath := cfg.SocketPath
	if socketPath == "" {
		socketPath = defaultSocketPath()
	}

	if socketPath != "" && socketAvailable(socketPath) {
		if err := e.connectToSocket(ctx, socketPath); err != nil {
			return nil, err
		}
		slog.Info("container: connected to existing socket", "socket", socketPath)
		return e, nil
	}

	// 2. Linux/FreeBSD: start in-process API server (no binary, no VM).
	if canStartInProcess() {
		if err := e.startServiceInProcess(ctx, cfg); err != nil {
			slog.Warn("container: in-process service failed", "error", err)
		} else {
			if err := e.connectToSocket(ctx, e.socketPath); err != nil {
				return nil, fmt.Errorf("connect to in-process service: %w", err)
			}
			return e, nil
		}
	}

	// 3. macOS/Windows: auto-provision and start VM via Go libraries.
	if !canStartInProcess() {
		slog.Info("container: no socket found, auto-provisioning machine via Go API")
		mcfg := DefaultMachineConfig()
		if err := EnsureMachine(ctx, mcfg); err != nil {
			return nil, fmt.Errorf("auto-provision machine: %w (no external podman binary needed — this uses the embedded Go libraries)", err)
		}

		// Machine started — try socket.
		if sp := defaultSocketPath(); sp != "" && socketAvailable(sp) {
			if err := e.connectToSocket(ctx, sp); err != nil {
				return nil, err
			}
			slog.Info("container: connected to machine", "socket", sp)
			return e, nil
		}

		return nil, fmt.Errorf("machine started but no socket available")
	}

	return nil, fmt.Errorf("container engine: no socket found, in-process service failed, and machine provisioning not available on this platform")
}

// EngineConfig holds configuration for the container engine.
type EngineConfig struct {
	SocketPath string // explicit socket path (optional)
	DataDir    string // data directory for podman storage
}

// ConnCtx returns the REST API connection context for direct bindings access.
func (e *Engine) ConnCtx() context.Context {
	return e.connCtx
}

// Healthy returns true if the engine can communicate with podman.
func (e *Engine) Healthy() bool {
	return e.healthy.Load()
}

// Close shuts down the engine and any managed service.
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

// connectToSocket establishes a REST API connection to the given socket.
func (e *Engine) connectToSocket(ctx context.Context, socketPath string) error {
	e.socketPath = socketPath
	e.apiURL = "unix://" + socketPath
	connCtx, err := bindings.NewConnection(ctx, e.apiURL)
	if err != nil {
		return fmt.Errorf("connect to socket %s: %w", socketPath, err)
	}
	e.connCtx = connCtx
	e.healthy.Store(true)
	return nil
}

// HostGateway returns the hostname that containers use to reach the host.
func (e *Engine) HostGateway() string {
	return "host.containers.internal"
}

// defaultBinCacheDir returns the cache directory for extracted companion binaries.
func defaultBinCacheDir() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "ycode", "bin")
	}
	return filepath.Join(os.TempDir(), "ycode-bin")
}

// defaultSocketPath returns the default Podman user socket path.
func defaultSocketPath() string {
	var candidates []string

	tmpDir := os.TempDir()
	candidates = append(candidates,
		filepath.Join(tmpDir, "podman", "podman-machine-default-api.sock"),
		filepath.Join(tmpDir, "podman", "podman-machine-ycode-default-api.sock"),
	)

	if uid := os.Getuid(); uid > 0 {
		candidates = append(candidates,
			filepath.Join(tmpDir, fmt.Sprintf("podman-run-%d", uid), "podman", "podman.sock"),
		)
	}

	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		candidates = append(candidates,
			filepath.Join(xdg, "podman", "podman.sock"),
		)
	}

	if host := os.Getenv("CONTAINER_HOST"); host != "" {
		if strings.HasPrefix(host, "unix://") {
			path := strings.TrimPrefix(host, "unix://")
			candidates = append([]string{path}, candidates...)
		}
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
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
