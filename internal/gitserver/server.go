// Package gitserver provides an embedded Gitea-based git server for ycode.
// Gitea runs in-process — no external binary or subprocess needed.
// It manages local git repositories for agent swarm coordination, providing
// branch isolation, PR workflows, and a web UI for human review.
package gitserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"

	giteaembed "code.gitea.io/gitea/embed"
)

// Server wraps the in-process Gitea server for agent swarm git operations.
type Server struct {
	cfg     *ServerConfig
	dataDir string
	inner   *giteaembed.Server
	healthy atomic.Bool
}

// ServerConfig holds configuration for the git server.
type ServerConfig struct {
	DataDir  string // data directory (repos, DB, config)
	AppName  string // display name (default: "ycode Git")
	HTTPOnly bool   // disable SSH access (default: true)
	Token    string // admin API token
	SubPath  string // URL sub-path when behind a reverse proxy (e.g. "/git")
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

	return &Server{
		cfg:     cfg,
		dataDir: cfg.DataDir,
	}, nil
}

// Start initializes and launches the in-process Gitea server.
func (s *Server) Start(ctx context.Context) error {
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return fmt.Errorf("create gitea data dir: %w", err)
	}

	// Write minimal app.ini for local single-user mode.
	if err := s.writeConfig(); err != nil {
		return fmt.Errorf("write gitea config: %w", err)
	}

	// Start Gitea in-process via the embed package.
	inner, err := giteaembed.NewServer(ctx, giteaembed.Config{
		WorkPath:   s.dataDir,
		CustomPath: filepath.Join(s.dataDir, "custom"),
		CustomConf: filepath.Join(s.dataDir, "custom", "conf", "app.ini"),
		AppName:    s.cfg.AppName,
	})
	if err != nil {
		return fmt.Errorf("init gitea: %w", err)
	}

	if err := inner.Start(ctx); err != nil {
		return fmt.Errorf("start gitea: %w", err)
	}

	s.inner = inner
	s.healthy.Store(true)
	slog.Info("gitserver: started (in-process)", "port", inner.Port(), "data", s.dataDir)
	return nil
}

// Stop gracefully shuts down the git server.
func (s *Server) Stop(ctx context.Context) error {
	s.healthy.Store(false)
	if s.inner != nil {
		return s.inner.Stop(ctx)
	}
	return nil
}

// Healthy returns true if the server is responding.
func (s *Server) Healthy() bool {
	return s.healthy.Load() && s.inner != nil && s.inner.Healthy()
}

// Port returns the HTTP port.
func (s *Server) Port() int {
	if s.inner == nil {
		return 0
	}
	return s.inner.Port()
}

// BaseURL returns the server's HTTP base URL.
func (s *Server) BaseURL() string {
	if s.inner == nil || s.inner.Port() == 0 {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d", s.inner.Port())
}

// HTTPHandler returns the Gitea HTTP handler for mounting on the reverse proxy.
func (s *Server) HTTPHandler() http.Handler {
	if s.inner == nil {
		return nil
	}
	return s.inner.HTTPHandler()
}

// writeConfig writes a minimal Gitea app.ini for local single-user operation.
func (s *Server) writeConfig() error {
	confDir := filepath.Join(s.dataDir, "custom", "conf")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return err
	}

	dbPath := filepath.Join(s.dataDir, "gitea.db")
	repoRoot := filepath.Join(s.dataDir, "repositories")

	subPath := s.cfg.SubPath
	if subPath == "" {
		subPath = "/"
	}

	ini := fmt.Sprintf(`[server]
HTTP_ADDR = 127.0.0.1
DOMAIN    = localhost
APP_NAME  = %s
OFFLINE_MODE = true
ROOT_URL  = http://localhost%s

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
`, s.cfg.AppName, subPath, dbPath, repoRoot,
		filepath.Join(s.dataDir, "log"),
		s.cfg.HTTPOnly,
	)

	return os.WriteFile(filepath.Join(confDir, "app.ini"), []byte(ini), 0o644)
}
