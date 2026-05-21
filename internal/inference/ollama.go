package inference

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ollama/ollama/envconfig"
	ollamaserver "github.com/ollama/ollama/server"
	"golang.org/x/crypto/ssh"

	runnerEmbed "github.com/qiangli/ycode/internal/inference/runner_embed"
	ollamaweb "github.com/qiangli/ycode/internal/inference/web"
)

// OllamaComponent implements the observability.Component interface for
// the embedded Ollama HTTP server. It drives ollama's own server package
// in-process — same handler set as the standalone `ollama serve` daemon
// (api/tags, api/chat, api/embed, api/pull, /v1/...), with the model
// scheduler spawning ycode-runner subprocesses as needed via the
// `ycode runner` subcommand (see cmd/ycode/runner.go).
//
// The default bind is whatever OLLAMA_HOST resolves to (127.0.0.1:11434
// upstream-canonical), so any tool pointed at the standard ollama
// endpoint connects to ycode without a config change.
type OllamaComponent struct {
	cfg     *Config
	dataDir string

	mu    sync.Mutex
	ln    net.Listener
	serve chan error // closed when ollamaserver.Serve returns

	healthy atomic.Bool
	otel    *otelState // nil if OTEL not configured
}

// serveOnce gates the lifetime of ollamaserver.Serve to ONE call per
// process. ollama's server.Serve calls http.Handle("/", h) on the global
// http.DefaultServeMux (for free pprof), and the stdlib mux panics on
// duplicate registration. The natural design — one ycode serve, one
// embedded ollama — already calls Start at most once; serveOnce just
// makes that constraint explicit (and survivable from tests).
var serveOnce sync.Once

// NewOllamaComponent creates a component that runs the embedded Ollama
// HTTP server. dataDir is the directory for model storage and runtime
// data (used as $OLLAMA_MODELS fallback).
func NewOllamaComponent(cfg *Config, dataDir string) *OllamaComponent {
	return &OllamaComponent{
		cfg:     cfg,
		dataDir: dataDir,
	}
}

func (o *OllamaComponent) Name() string { return "ollama" }

// prepare runs the side-effects Start needs: create the data dir, set
// $OLLAMA_MODELS, and ensure the ed25519 keypair exists. Split out so
// tests can exercise them without driving ollamaserver.Serve (which is
// process-global and one-shot — see serveOnce).
func (o *OllamaComponent) prepare() error {
	if err := os.MkdirAll(o.dataDir, 0o755); err != nil {
		return fmt.Errorf("ollama: create data dir: %w", err)
	}
	if o.cfg != nil && o.cfg.ModelsDir != "" {
		os.Setenv("OLLAMA_MODELS", o.cfg.ModelsDir)
	}
	if err := ensureOllamaKeypair(); err != nil {
		return fmt.Errorf("ollama: ensure keypair: %w", err)
	}
	return nil
}

// ensureOllamaKeypair generates the ed25519 keypair at
// ~/.ollama/id_ed25519{,.pub} if it doesn't already exist. ollama's
// model-pull path (server/download.go → manifest signing) blows up with
// `open ~/.ollama/id_ed25519: no such file or directory` without it.
// Mirrors initializeKeypair() in upstream cmd/cmd.go — replicated here
// instead of imported so we don't pull in ollama's whole CLI dep tree.
func ensureOllamaKeypair() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	privKeyPath := filepath.Join(home, ".ollama", "id_ed25519")
	pubKeyPath := filepath.Join(home, ".ollama", "id_ed25519.pub")

	if _, err := os.Stat(privKeyPath); err == nil {
		return nil // already present
	} else if !os.IsNotExist(err) {
		return err
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	privBytes, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(privKeyPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(privKeyPath, pem.EncodeToMemory(privBytes), 0o600); err != nil {
		return err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return err
	}
	if err := os.WriteFile(pubKeyPath, ssh.MarshalAuthorizedKey(sshPub), 0o644); err != nil {
		return err
	}
	slog.Info("ollama: generated identity keypair", "path", privKeyPath)
	return nil
}

// Start binds the ollama HTTP listener (default 127.0.0.1:11434, override
// via OLLAMA_HOST) and runs ollama's server.Serve in a goroutine. Model
// loading is lazy — the listener returns ready before any model is
// downloaded or any runner subprocess is spawned. The actual runner
// (ycode-runner, from runner_embed) is spawned by ollama's scheduler on
// the first inference call, on its own ephemeral port.
//
// Only the first Start call in a process actually launches the embedded
// server — subsequent calls bind a new listener but the global serve
// goroutine started by the first call keeps running. This is a
// constraint inherited from ollama's server package (which uses
// http.DefaultServeMux); a typical ycode serve lifecycle calls Start
// exactly once.
func (o *OllamaComponent) Start(ctx context.Context) error {
	// Defense in depth — the canonical hard-stop lives at the serve
	// entry point (cmd/ycode/serve.go: runAllServices), which fails
	// before any component is constructed. The stack manager demotes
	// per-component Start errors to warnings, so a check here alone
	// wouldn't actually stop serve from coming up. We keep the
	// check for tests / future entry points that bypass serve.
	if !runnerEmbed.Available() {
		return fmt.Errorf("ollama: %w", ErrRunnerNotInstalled)
	}

	if err := o.prepare(); err != nil {
		return err
	}

	hostAddr := envconfig.Host().Host
	ln, err := net.Listen("tcp", hostAddr)
	if err != nil {
		return fmt.Errorf("ollama: listen on %s: %w", hostAddr, err)
	}

	o.mu.Lock()
	o.ln = ln
	o.serve = make(chan error, 1)
	o.mu.Unlock()

	started := false
	serveOnce.Do(func() {
		started = true
		go func(ln net.Listener, done chan<- error) {
			slog.Info("ollama: serving on", "addr", ln.Addr().String())
			err := ollamaserver.Serve(ln)
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("ollama: server exited", "error", err)
				o.healthy.Store(false)
				o.updateOTELGauges()
			}
			done <- err
			close(done)
		}(ln, o.serve)
	})
	if !started {
		// Already started elsewhere in this process. Close the spare
		// listener — the running server holds its own — and surface a
		// soft warning. Callers should treat this as already-healthy.
		_ = ln.Close()
		o.mu.Lock()
		o.ln = nil
		o.mu.Unlock()
		slog.Warn("ollama: server already running in this process; Start is a no-op")
		o.healthy.Store(true)
		return nil
	}

	// Wait for the server to start accepting before declaring healthy.
	// ollamaserver.Serve does scheduler init synchronously after
	// http.Handle but before http.Serve, so a TCP probe on the listener
	// address is a reliable readiness signal.
	if err := waitTCPReady(ln.Addr().String(), 5*time.Second); err != nil {
		// Don't fail Start — the listener is bound, the server will
		// finish coming up shortly. Just log it.
		slog.Warn("ollama: server slow to accept connections", "error", err)
	}

	o.healthy.Store(true)
	o.traceRunnerStart(ctx)
	o.updateOTELGauges()
	return nil
}

func (o *OllamaComponent) Stop(ctx context.Context) error {
	o.healthy.Store(false)
	o.updateOTELGauges()

	o.mu.Lock()
	ln := o.ln
	serve := o.serve
	o.ln = nil
	o.mu.Unlock()

	if ln == nil {
		return nil
	}
	closeErr := ln.Close() // ollamaserver.Serve returns http.ErrServerClosed

	// Drain the serve goroutine so we don't leak it.
	if serve != nil {
		select {
		case <-serve:
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
		}
	}
	return closeErr
}

func (o *OllamaComponent) Healthy() bool { return o.healthy.Load() }

// HTTPHandler returns a reverse-proxy mounted at /ollama/ on the
// dashboard, so the management UI keeps working and so users on the
// dashboard host can reach the API without the OLLAMA_HOST port. API
// paths (/api/*, /v1/*) proxy to the embedded ollama server; everything
// else serves the embedded management SPA.
func (o *OllamaComponent) HTTPHandler() http.Handler {
	base := o.BaseURL()
	if base == "" {
		return nil
	}
	target, err := url.Parse(base)
	if err != nil {
		return nil
	}
	apiProxy := httputil.NewSingleHostReverseProxy(target)
	staticHandler := ollamaweb.Handler()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") {
			apiProxy.ServeHTTP(w, r)
			return
		}
		staticHandler.ServeHTTP(w, r)
	})
}

// Port returns the bound TCP port (typically 11434).
func (o *OllamaComponent) Port() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.ln == nil {
		return 0
	}
	if addr, ok := o.ln.Addr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

// BaseURL returns the full Ollama API base URL clients should hit.
func (o *OllamaComponent) BaseURL() string {
	o.mu.Lock()
	ln := o.ln
	o.mu.Unlock()
	if ln == nil {
		return ""
	}
	// Use ConnectableHost so 0.0.0.0 binds resolve to 127.0.0.1 for
	// in-process clients (the MCP proxy, the local provider).
	return "http://" + envconfig.ConnectableHost().Host
}

// waitTCPReady probes the listener address until it accepts connections
// or the deadline expires.
func waitTCPReady(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("%s not accepting after %v", addr, timeout)
}
