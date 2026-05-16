// Package gitserver provides an embedded Gitea-based git server for ycode.
// Gitea runs in-process — no external binary or subprocess needed.
// It manages local git repositories for agent swarm coordination, providing
// branch isolation, PR workflows, and a web UI for human review.
package gitserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
	// PublicRootURL is the externally-visible URL Gitea should advertise
	// in generated links and asset prefixes (e.g.
	// "http://127.0.0.1:58080/git/"). When set, it is written as
	// ROOT_URL in app.ini, and Gitea derives AppSubURL from its path.
	// Empty disables the override; Gitea falls back to its own default.
	PublicRootURL string
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

	// Pre-allocate the HTTP port and bake it into app.ini so generated
	// git hook scripts (which load this same app.ini in a fresh process)
	// resolve the correct LOCAL_ROOT_URL when calling back to Gitea's
	// internal API. Without this, hooks default to port 3000 and fail.
	port, err := allocPort()
	if err != nil {
		return fmt.Errorf("allocate gitea port: %w", err)
	}
	if err := s.writeConfig(port); err != nil {
		return fmt.Errorf("write gitea config: %w", err)
	}

	// Start Gitea in-process via the embed package.
	inner, err := giteaembed.NewServer(ctx, giteaembed.Config{
		WorkPath:   s.dataDir,
		CustomPath: filepath.Join(s.dataDir, "custom"),
		CustomConf: filepath.Join(s.dataDir, "custom", "conf", "app.ini"),
		Port:       port,
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

// allocPort grabs a free TCP port. Brief race window between close and
// the embed package's Listen, but in practice the OS does not reassign
// within the millisecond gap.
func allocPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, nil
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

// writeConfig writes a minimal Gitea app.ini for local single-user
// operation. The HTTP_PORT and LOCAL_ROOT_URL must be pinned so that
// generated git hook scripts — which load this same file in a separate
// process — can call back to Gitea's internal API on the right port.
func (s *Server) writeConfig(port int) error {
	confDir := filepath.Join(s.dataDir, "custom", "conf")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return err
	}

	dbPath := filepath.Join(s.dataDir, "gitea.db")
	repoRoot := filepath.Join(s.dataDir, "repositories")

	// Reuse the persisted INTERNAL_TOKEN if Gitea generated one on a
	// previous run; otherwise generate a stable one ourselves so that
	// hook handlers (loading this same config in a child process)
	// authenticate against the running server with matching credentials.
	internalToken := readExistingInternalToken(filepath.Join(confDir, "app.ini"))
	if internalToken == "" {
		internalToken = randomTokenSecret()
	}

	localURL := fmt.Sprintf("http://127.0.0.1:%d/", port)

	// ROOT_URL controls Gitea's link & asset URL generation. When ycode
	// runs Gitea behind the observability proxy at /git/, set ROOT_URL
	// to the proxy-fronted URL so emitted asset paths carry the sub-path
	// the browser is actually requesting from (e.g. /git/assets/*). With
	// USE_SUB_URL_PATH left at its default (false), Gitea still expects
	// requests *without* the prefix — which matches the proxy's
	// StripPrefix mount.
	rootURLLine := ""
	if s.cfg.PublicRootURL != "" {
		rootURLLine = fmt.Sprintf("ROOT_URL       = %s\n", s.cfg.PublicRootURL)
	}

	ini := fmt.Sprintf(`[server]
HTTP_ADDR      = 127.0.0.1
HTTP_PORT      = %d
DOMAIN         = localhost
APP_NAME       = %s
OFFLINE_MODE   = true
%sLOCAL_ROOT_URL = %s

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
INSTALL_LOCK   = true
INTERNAL_TOKEN = %s

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
`, port, s.cfg.AppName, rootURLLine, localURL, dbPath, repoRoot, internalToken,
		filepath.Join(s.dataDir, "log"),
		s.cfg.HTTPOnly,
	)

	return os.WriteFile(filepath.Join(confDir, "app.ini"), []byte(ini), 0o644)
}

// readExistingInternalToken returns INTERNAL_TOKEN from a previous
// app.ini if present, else "". Best-effort line scan; we don't pull
// in an INI parser for one optional value.
func readExistingInternalToken(appIni string) string {
	data, err := os.ReadFile(appIni)
	if err != nil {
		return ""
	}
	const key = "INTERNAL_TOKEN"
	for _, line := range splitLines(string(data)) {
		trim := trimSpaces(line)
		if !startsWith(trim, key) {
			continue
		}
		rest := trimSpaces(trim[len(key):])
		if rest == "" || rest[0] != '=' {
			continue
		}
		return trimSpaces(rest[1:])
	}
	return ""
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func trimSpaces(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// randomTokenSecret returns a high-entropy hex string used as
// INTERNAL_TOKEN when no prior token exists.
func randomTokenSecret() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("gitserver: rand.Read: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

var _ = strconv.Itoa // keep strconv if future fmt-string changes drop %d
