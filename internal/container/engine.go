// Package container provides embedded Podman-based container management for ycode.
// Operations use Podman's REST API client bindings (pure Go, no CLI shelling).
// The Podman service is either an existing socket or started as a managed process.
package container

import (
	"context"
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

	podmanEmbed "github.com/qiangli/ycode/internal/container/podman_embed"
	"go.podman.io/podman/v6/pkg/bindings"
)

// Engine wraps the Podman container management layer.
// It connects to a Podman service via REST API bindings (pure Go)
// and optionally manages the service process lifecycle.
type Engine struct {
	connCtx    context.Context // connection context for REST API bindings
	socketPath string
	apiURL     string

	healthy atomic.Bool
	mu      sync.Mutex

	// Only used when we start our own podman service.
	binaryPath string
	cmd        *exec.Cmd
	cancel     context.CancelFunc
	done       chan struct{}
}

// NewEngine creates a container engine. It connects to an existing Podman
// socket or starts a new Podman service and establishes a REST API connection.
func NewEngine(ctx context.Context, cfg *EngineConfig) (*Engine, error) {
	e := &Engine{
		done: make(chan struct{}),
	}

	// Try existing socket first.
	socketPath := cfg.SocketPath
	if socketPath == "" {
		socketPath = defaultSocketPath()
	}

	if socketPath != "" && socketAvailable(socketPath) {
		e.socketPath = socketPath
		e.apiURL = "unix://" + socketPath
		connCtx, err := bindings.NewConnection(ctx, e.apiURL)
		if err != nil {
			return nil, fmt.Errorf("connect to podman socket: %w", err)
		}
		e.connCtx = connCtx
		e.healthy.Store(true)
		slog.Info("container: connected to existing podman socket", "socket", socketPath)
		return e, nil
	}

	// Try in-process API server first (Linux: uses libpod directly, no binary).
	if canStartInProcess() {
		if err := e.startServiceInProcess(ctx, cfg); err != nil {
			slog.Warn("container: in-process service failed, falling back to binary", "error", err)
		} else {
			connCtx, err := bindings.NewConnection(ctx, e.apiURL)
			if err != nil {
				return nil, fmt.Errorf("connect to in-process service: %w", err)
			}
			e.connCtx = connCtx
			return e, nil
		}
	}

	// macOS/other: auto-provision and start podman machine if needed.
	if !canStartInProcess() {
		slog.Info("container: no socket found, auto-provisioning machine")
		mcfg := DefaultMachineConfig()
		if err := EnsureMachine(ctx, mcfg); err != nil {
			slog.Warn("container: machine auto-provision failed", "error", err)
		} else {
			// Machine started — try socket again.
			if sp := defaultSocketPath(); sp != "" && socketAvailable(sp) {
				e.socketPath = sp
				e.apiURL = "unix://" + sp
				connCtx, err := bindings.NewConnection(ctx, e.apiURL)
				if err != nil {
					return nil, fmt.Errorf("connect to machine socket: %w", err)
				}
				e.connCtx = connCtx
				e.healthy.Store(true)
				slog.Info("container: connected to machine", "socket", sp)
				return e, nil
			}
		}
	}

	// Fallback: start podman as an external service process.
	path, err := discoverBinary(cfg.BinaryPath)
	if err != nil {
		return nil, fmt.Errorf("podman not found: %w (install podman, start podman machine, or connect to a remote host)", err)
	}
	e.binaryPath = path

	if err := e.startService(ctx, cfg); err != nil {
		return nil, fmt.Errorf("start podman service: %w", err)
	}

	// Establish REST API connection to the started service.
	connCtx, err := bindings.NewConnection(ctx, e.apiURL)
	if err != nil {
		return nil, fmt.Errorf("connect to started podman service: %w", err)
	}
	e.connCtx = connCtx

	return e, nil
}

// EngineConfig holds configuration for the container engine.
type EngineConfig struct {
	BinaryPath string // explicit path to podman binary (optional, only for service start)
	SocketPath string // explicit socket path (optional)
	DataDir    string // data directory for podman storage
}

// ConnCtx returns the REST API connection context for direct bindings access.
func (e *Engine) ConnCtx() context.Context {
	return e.connCtx
}

// Run executes a raw podman CLI command. This is only used by the `ycode podman`
// pass-through CLI subcommand. All internal container operations use REST bindings.
func (e *Engine) Run(ctx context.Context, args ...string) ([]byte, error) {
	if e.binaryPath == "" {
		return nil, fmt.Errorf("podman binary not available (connected via socket only; use REST APIs)")
	}
	cmd := exec.CommandContext(ctx, e.binaryPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("podman %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return out, nil
}

// Healthy returns true if the engine can communicate with podman.
func (e *Engine) Healthy() bool {
	return e.healthy.Load()
}

// BinaryPath returns the discovered podman binary path (empty if socket-only).
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
// This is the only operation that uses exec.Command — all subsequent
// container operations use the REST API bindings.
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
	os.Remove(socketPath) // Remove stale socket.

	sctx, cancel := context.WithCancel(ctx)
	e.cancel = cancel

	cmd := exec.CommandContext(sctx, e.binaryPath, "system", "service",
		"--timeout=0",
		"unix://"+socketPath,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start podman service: %w", err)
	}
	e.cmd = cmd
	e.socketPath = socketPath
	e.apiURL = "unix://" + socketPath

	if err := waitForSocket(socketPath, 10*time.Second); err != nil {
		cancel()
		return fmt.Errorf("podman service did not start: %w", err)
	}

	e.healthy.Store(true)
	slog.Info("container: started podman service", "socket", socketPath)

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

// discoverBinary finds the podman binary using a priority search:
//  1. Explicit path from config
//  2. Self-extracting embedded binary (single-binary deploys)
//  3. $YCODE_CONTAINER_RUNTIME environment variable
//  4. Adjacent to ycode binary: $(dirname ycode)/podman
//  5. System PATH
func discoverBinary(explicit string) (string, error) {
	// 1. Explicit path.
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit, nil
		}
		return "", fmt.Errorf("configured binary not found: %s", explicit)
	}

	// 2. Self-extracting embedded binary.
	if podmanEmbed.Available() {
		cacheDir := defaultBinCacheDir()
		if p, err := podmanEmbed.EnsurePodman(cacheDir); err == nil {
			slog.Info("container: using embedded podman", "path", p)
			return p, nil
		}
	}

	// 3. Environment variable.
	if envPath := os.Getenv("YCODE_CONTAINER_RUNTIME"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}
	}

	// 4. Adjacent to ycode binary.
	if exe, err := os.Executable(); err == nil {
		adjacent := filepath.Join(filepath.Dir(exe), "podman")
		if _, err := os.Stat(adjacent); err == nil {
			return adjacent, nil
		}
	}

	// 5. System PATH.
	if path, err := exec.LookPath("podman"); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("podman not found: no embedded binary, not in PATH, not adjacent to ycode, not via $YCODE_CONTAINER_RUNTIME")
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
	candidates := []string{}

	tmpDir := os.TempDir()
	candidates = append(candidates,
		filepath.Join(tmpDir, "podman", "podman-machine-default-api.sock"),
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
