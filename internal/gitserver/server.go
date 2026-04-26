// Package gitserver provides an embedded Gitea-based git server for ycode.
// It manages local git repositories for agent swarm coordination, providing
// branch isolation, PR workflows, and a web UI for human review.
package gitserver

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"time"

	// Anchor import to ensure gitea embed dependencies are preserved by go mod tidy.
	_ "code.gitea.io/gitea/embed"
)

// Server wraps the embedded Gitea server for agent swarm git operations.
type Server struct {
	cfg     *ServerConfig
	dataDir string
	port    int

	binaryPath string
	cmd        *exec.Cmd
	cancel     context.CancelFunc
	done       chan struct{}
	healthy    atomic.Bool
}

// ServerConfig holds configuration for the git server.
type ServerConfig struct {
	DataDir  string // SQLite + repos stored here
	AppName  string // display name (default: "ycode Git")
	HTTPOnly bool   // disable SSH (default: true)
	Token    string // admin API token
}

// NewServer creates a git server instance.
func NewServer(cfg *ServerConfig) (*Server, error) {
	if cfg.AppName == "" {
		cfg.AppName = "ycode Git"
	}
	if cfg.DataDir == "" {
		home, _ := os.UserHomeDir()
		cfg.DataDir = filepath.Join(home, ".agents", "ycode", "gitea")
	}

	// Discover gitea binary.
	binaryPath, err := discoverGitea()
	if err != nil {
		return nil, err
	}

	return &Server{
		cfg:        cfg,
		dataDir:    cfg.DataDir,
		binaryPath: binaryPath,
		done:       make(chan struct{}),
	}, nil
}

// Start launches the Gitea server on an ephemeral port.
func (s *Server) Start(ctx context.Context) error {
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return fmt.Errorf("create gitea data dir: %w", err)
	}

	// Allocate ephemeral port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("allocate gitea port: %w", err)
	}
	s.port = listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Write minimal app.ini for local single-user mode.
	if err := s.writeConfig(); err != nil {
		return fmt.Errorf("write gitea config: %w", err)
	}

	sctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	cmd := exec.CommandContext(sctx, s.binaryPath, "web",
		"--custom-path", filepath.Join(s.dataDir, "custom"),
		"--config", filepath.Join(s.dataDir, "custom", "conf", "app.ini"),
		"--work-path", s.dataDir,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Dir = s.dataDir

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start gitea: %w", err)
	}
	s.cmd = cmd

	// Wait for HTTP endpoint.
	if err := s.waitForHealthy(10 * time.Second); err != nil {
		cancel()
		return fmt.Errorf("gitea did not become healthy: %w", err)
	}

	s.healthy.Store(true)
	slog.Info("gitserver: started", "port", s.port, "data", s.dataDir)

	// Monitor process.
	go func() {
		defer close(s.done)
		err := cmd.Wait()
		s.healthy.Store(false)
		if err != nil && sctx.Err() == nil {
			slog.Warn("gitserver: gitea exited unexpectedly", "error", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the git server.
func (s *Server) Stop(ctx context.Context) error {
	s.healthy.Store(false)
	if s.cancel != nil {
		s.cancel()
		select {
		case <-s.done:
		case <-time.After(5 * time.Second):
		}
	}
	return nil
}

// Healthy returns true if the server is responding.
func (s *Server) Healthy() bool {
	return s.healthy.Load()
}

// Port returns the HTTP port.
func (s *Server) Port() int {
	return s.port
}

// BaseURL returns the server's HTTP base URL.
func (s *Server) BaseURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", s.port)
}

// HTTPHandler returns a reverse proxy handler for mounting on the StackManager proxy.
func (s *Server) HTTPHandler() http.Handler {
	if s.port == 0 {
		return nil
	}
	target, _ := url.Parse(s.BaseURL())
	return httputil.NewSingleHostReverseProxy(target)
}

// writeConfig writes a minimal Gitea app.ini for local single-user operation.
func (s *Server) writeConfig() error {
	confDir := filepath.Join(s.dataDir, "custom", "conf")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return err
	}

	dbPath := filepath.Join(s.dataDir, "gitea.db")
	repoRoot := filepath.Join(s.dataDir, "repositories")

	ini := fmt.Sprintf(`[server]
HTTP_ADDR = 127.0.0.1
HTTP_PORT = %d
ROOT_URL  = http://127.0.0.1:%d/git/
DOMAIN    = localhost
APP_NAME  = %s
OFFLINE_MODE = true

[database]
DB_TYPE = sqlite3
PATH    = %s

[repository]
ROOT = %s

[service]
DISABLE_REGISTRATION   = true
REQUIRE_SIGNIN_CONFIRM = false
ENABLE_NOTIFY_MAIL     = false

[security]
INSTALL_LOCK = true

[session]
PROVIDER = file

[log]
MODE = file
LEVEL = Warn
ROOT_PATH = %s

[mailer]
ENABLED = false

[ssh]
DISABLE = %t

[api]
ENABLE_SWAGGER = false
`, s.port, s.port, s.cfg.AppName, dbPath, repoRoot,
		filepath.Join(s.dataDir, "log"),
		s.cfg.HTTPOnly,
	)

	return os.WriteFile(filepath.Join(confDir, "app.ini"), []byte(ini), 0o644)
}

// waitForHealthy polls the health endpoint until it responds.
func (s *Server) waitForHealthy(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", s.port))
		if err == nil {
			resp.Body.Close()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("gitea health check timed out after %v", timeout)
}

// discoverGitea finds the gitea binary.
func discoverGitea() (string, error) {
	if envPath := os.Getenv("YCODE_GITEA_PATH"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}
	}

	// Adjacent to ycode binary.
	if exe, err := os.Executable(); err == nil {
		adjacent := filepath.Join(filepath.Dir(exe), "gitea")
		if _, err := os.Stat(adjacent); err == nil {
			return adjacent, nil
		}
	}

	// System PATH.
	if path, err := exec.LookPath("gitea"); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("gitea binary not found (install gitea or set $YCODE_GITEA_PATH)")
}
