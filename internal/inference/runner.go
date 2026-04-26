package inference

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// RunnerManager manages the lifecycle of the external C++ inference runner.
// It handles discovery, spawning, health monitoring, and crash recovery.
type RunnerManager struct {
	binaryPath string
	port       int
	cmd        *exec.Cmd
	healthy    atomic.Bool
	restarts   atomic.Int32
	mu         sync.Mutex
	cancel     context.CancelFunc
	done       chan struct{}

	maxRestarts int
	healthURL   string
}

// NewRunnerManager creates a runner manager. The runner binary is discovered
// using the following priority:
//  1. Explicit runnerPath from config
//  2. $OLLAMA_RUNNERS environment variable
//  3. Adjacent to the ycode binary: $(dirname ycode)/ollama
//  4. System PATH: which ollama
func NewRunnerManager(cfg *Config) (*RunnerManager, error) {
	path, err := discoverRunner(cfg.RunnerPath)
	if err != nil {
		return nil, fmt.Errorf("inference runner not found: %w (set runnerPath in config or install ollama)", err)
	}

	return &RunnerManager{
		binaryPath:  path,
		maxRestarts: 3,
		done:        make(chan struct{}),
	}, nil
}

// Start spawns the runner on an ephemeral port and waits for it to become healthy.
func (r *RunnerManager) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Allocate ephemeral port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("runner: allocate port: %w", err)
	}
	r.port = ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	r.healthURL = fmt.Sprintf("http://127.0.0.1:%d", r.port)

	return r.spawn(ctx)
}

// spawn starts the runner process and monitors it.
func (r *RunnerManager) spawn(ctx context.Context) error {
	rctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	r.cmd = exec.CommandContext(rctx, r.binaryPath, "serve")
	r.cmd.Env = append(os.Environ(),
		fmt.Sprintf("OLLAMA_HOST=127.0.0.1:%d", r.port),
	)
	r.cmd.Stdout = os.Stderr // Route runner output to ycode's stderr.
	r.cmd.Stderr = os.Stderr

	if err := r.cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("runner: start %s: %w", r.binaryPath, err)
	}

	slog.Info("inference: runner started", "pid", r.cmd.Process.Pid, "port", r.port, "binary", r.binaryPath)

	// Wait for health.
	if err := r.waitForHealth(ctx); err != nil {
		r.cmd.Process.Kill()
		cancel()
		return fmt.Errorf("runner: health check failed: %w", err)
	}

	r.healthy.Store(true)

	// Monitor for unexpected exit.
	go r.monitor(ctx)

	return nil
}

// waitForHealth polls the runner's health endpoint with exponential backoff.
func (r *RunnerManager) waitForHealth(ctx context.Context) error {
	client := &http.Client{Timeout: 2 * time.Second}
	backoff := 100 * time.Millisecond

	for i := 0; i < 30; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := client.Get(r.healthURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				slog.Info("inference: runner healthy", "port", r.port)
				return nil
			}
		}

		time.Sleep(backoff)
		if backoff < 2*time.Second {
			backoff *= 2
		}
	}

	return fmt.Errorf("runner did not become healthy at %s", r.healthURL)
}

// monitor watches for runner process exit and attempts restart.
func (r *RunnerManager) monitor(ctx context.Context) {
	defer close(r.done)

	err := r.cmd.Wait()
	r.healthy.Store(false)

	select {
	case <-ctx.Done():
		// Intentional shutdown — don't restart.
		slog.Info("inference: runner stopped (context cancelled)")
		return
	default:
	}

	slog.Warn("inference: runner exited unexpectedly", "error", err, "restarts", r.restarts.Load())

	// Attempt restart with exponential backoff.
	for {
		restartCount := r.restarts.Add(1)
		if int(restartCount) > r.maxRestarts {
			slog.Error("inference: runner restart limit reached", "max", r.maxRestarts)
			return
		}

		backoff := time.Duration(1<<(restartCount-1)) * time.Second // 1s, 2s, 4s
		slog.Info("inference: restarting runner", "attempt", restartCount, "backoff", backoff)
		time.Sleep(backoff)

		r.mu.Lock()
		r.done = make(chan struct{})
		err := r.spawn(ctx)
		r.mu.Unlock()

		if err != nil {
			slog.Error("inference: runner restart failed", "error", err, "attempt", restartCount)
			continue
		}
		return
	}
}

// Stop kills the runner process.
func (r *RunnerManager) Stop(ctx context.Context) error {
	r.healthy.Store(false)
	if r.cancel != nil {
		r.cancel()
	}
	if r.cmd != nil && r.cmd.Process != nil {
		r.cmd.Process.Kill()
	}
	// Wait for monitor goroutine to finish.
	select {
	case <-r.done:
	case <-ctx.Done():
	}
	return nil
}

// Healthy returns true if the runner process is alive and responsive.
func (r *RunnerManager) Healthy() bool {
	return r.healthy.Load()
}

// Port returns the port the runner is listening on.
func (r *RunnerManager) Port() int {
	return r.port
}

// Restarts returns the number of times the runner has been restarted.
func (r *RunnerManager) Restarts() int32 {
	return r.restarts.Load()
}

// BaseURL returns the runner's HTTP base URL.
func (r *RunnerManager) BaseURL() string {
	return r.healthURL
}

// discoverRunner finds the runner binary using a priority search.
func discoverRunner(explicit string) (string, error) {
	// 1. Explicit path.
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit, nil
		}
		return "", fmt.Errorf("explicit runner path %q not found", explicit)
	}

	// 2. $OLLAMA_RUNNERS env var.
	if p := os.Getenv("OLLAMA_RUNNERS"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	// 3. Adjacent to the current binary.
	if exe, err := os.Executable(); err == nil {
		adjacent := filepath.Join(filepath.Dir(exe), "ollama")
		if _, err := os.Stat(adjacent); err == nil {
			return adjacent, nil
		}
	}

	// 4. System PATH.
	if p, err := exec.LookPath("ollama"); err == nil {
		return p, nil
	}

	return "", fmt.Errorf("ollama binary not found in PATH, adjacent to ycode, or via $OLLAMA_RUNNERS")
}
