// Package gateway exposes the user-facing podman + ollama endpoints for
// a running ycode. It is the single boundary that hides whether the
// backing service is embedded (in-process libpod, local ollama runner)
// or remote (proxied via cloudbox to a different machine).
//
// Tools and agents that ycode spawns get DOCKER_HOST / CONTAINER_HOST /
// OLLAMA_HOST pointing at the gateway, so they don't need to know
// anything about cloudbox, the matrix tunnel, MCP, or how the daemon
// is reached.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"time"
)

// Mode picks the routing strategy for a backend.
type Mode string

const (
	// ModeEmbedded routes to a local upstream provided by the caller
	// (e.g. ycode's own libpod socket / ollama runner port). The whole
	// request stays on this host.
	ModeEmbedded Mode = "embedded"
	// ModeRemote routes to a cloudbox-proxied URL with Bearer auth.
	// Used when this ycode is a client of another machine's services.
	ModeRemote Mode = "remote"
)

// PodmanBackend describes where the gateway's podman endpoint should
// forward requests.
//
//	Embedded mode: Upstream is a unix socket path (or unix:// URL) that
//	    speaks the libpod REST API.
//	Remote mode:   URL is the cloudbox-proxied https endpoint, and the
//	    TokenFile is a file holding a Bearer token (re-read on each
//	    request so cloudbox token rotation works without a ycode restart).
type PodmanBackend struct {
	Mode      Mode
	Upstream  string // socket path or unix:// URL (embedded)
	URL       string // https://.../h/<host>/app/podman/ (remote)
	TokenFile string // path to Bearer token file (remote)
}

// OllamaBackend mirrors PodmanBackend for ollama.
//
//	Embedded mode: Upstream is the in-process runner's HTTP base URL
//	    (e.g. http://127.0.0.1:11434).
//	Remote mode:   URL is the cloudbox-proxied https endpoint, TokenFile
//	    a Bearer token file.
type OllamaBackend struct {
	Mode      Mode
	Upstream  string // http(s):// of the local runner (embedded)
	URL       string // https://.../h/<host>/app/ollama/ (remote)
	TokenFile string // path to Bearer token file (remote)
}

// Config drives Start. Both backends may be configured independently.
type Config struct {
	Podman PodmanBackend
	Ollama OllamaBackend
	// PodmanSocketPath, when non-empty, overrides the auto-derived
	// per-PID path. Used by tests; production callers leave it empty.
	PodmanSocketPath string
}

// Endpoints is what the gateway publishes to consumers (env vars,
// manifest.json, etc.). Empty fields mean the corresponding backend
// failed to start.
type Endpoints struct {
	// PodmanSocket is the local socket path (or named pipe name on
	// Windows). Set DOCKER_HOST=unix://<this> + CONTAINER_HOST=unix://<this>.
	PodmanSocket string
	// OllamaURL is http://127.0.0.1:<port>. Set OLLAMA_HOST=<this>.
	OllamaURL string
	// PodmanMode / OllamaMode are the resolved modes — useful in the
	// manifest so paired clients can see what they're talking to.
	PodmanMode Mode
	OllamaMode Mode
}

// Gateway owns the listeners and per-backend reverse proxies.
type Gateway struct {
	cfg Config

	podmanLn  net.Listener
	podmanSrv *http.Server
	ollamaLn  net.Listener
	ollamaSrv *http.Server

	endpoints Endpoints

	mu     sync.Mutex
	closed bool
}

// Start opens the listeners and begins serving. Returns the Gateway and
// the resolved Endpoints. The caller is responsible for calling Close.
//
// Backends with a missing/empty config (Mode unset) are skipped — the
// corresponding endpoint field in Endpoints stays empty.
func Start(_ context.Context, cfg Config) (*Gateway, error) {
	g := &Gateway{cfg: cfg}

	if cfg.Podman.Mode != "" {
		if err := g.startPodman(); err != nil {
			_ = g.Close()
			return nil, fmt.Errorf("podman gateway: %w", err)
		}
	}
	if cfg.Ollama.Mode != "" {
		if err := g.startOllama(); err != nil {
			_ = g.Close()
			return nil, fmt.Errorf("ollama gateway: %w", err)
		}
	}
	return g, nil
}

// Endpoints returns what the gateway is currently publishing.
func (g *Gateway) Endpoints() Endpoints { return g.endpoints }

// Env returns the env-var map that should be injected into subprocesses
// (`ycode shell`, agent_shell MCP, etc.) so they reach the gateway by
// default. Empty values are omitted.
func (g *Gateway) Env() map[string]string {
	out := map[string]string{}
	if g.endpoints.PodmanSocket != "" {
		val := "unix://" + g.endpoints.PodmanSocket
		out["DOCKER_HOST"] = val
		out["CONTAINER_HOST"] = val
	}
	if g.endpoints.OllamaURL != "" {
		out["OLLAMA_HOST"] = g.endpoints.OllamaURL
	}
	return out
}

// Close stops the listeners and cleans up the socket file. Safe to call
// multiple times.
func (g *Gateway) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	g.closed = true

	var firstErr error
	shutdown := func(srv *http.Server) {
		if srv == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	shutdown(g.podmanSrv)
	shutdown(g.ollamaSrv)
	if g.podmanLn != nil {
		_ = g.podmanLn.Close()
	}
	if g.ollamaLn != nil {
		_ = g.ollamaLn.Close()
	}
	// Best-effort socket cleanup. Listener.Close usually leaves the
	// inode behind on unix; we want the next ycode-serve to be able to
	// bind the same path.
	if g.endpoints.PodmanSocket != "" {
		_ = os.Remove(g.endpoints.PodmanSocket)
	}
	return firstErr
}

// newReverseProxy builds an httputil.ReverseProxy with a sane Director.
// For embedded callers, transport.DialContext dials the backing socket;
// for remote callers, transport uses the system roots and a bearer wrap.
func newReverseProxy(target *url.URL, transport http.RoundTripper) *httputil.ReverseProxy {
	rp := httputil.NewSingleHostReverseProxy(target)
	if transport != nil {
		rp.Transport = transport
	}
	rp.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		// Don't leak the upstream URL on errors; this is the user's own
		// machine, but consistent with what cloudbox does.
		slog.Error("gateway proxy error", "err", err)
		http.Error(w, "gateway backend unavailable: "+err.Error(), http.StatusBadGateway)
	}
	return rp
}

// bearerInjector wraps base so every outgoing request gets the current
// Bearer token from tokenFile. The file is read on each request — cheap,
// and lets cloudbox rotate the token without a ycode restart.
type bearerInjector struct {
	base      http.RoundTripper
	tokenFile string
}

func (b *bearerInjector) RoundTrip(req *http.Request) (*http.Response, error) {
	if b.tokenFile != "" {
		raw, err := os.ReadFile(b.tokenFile)
		if err != nil {
			return nil, fmt.Errorf("read token file %s: %w", b.tokenFile, err)
		}
		token := trimLine(string(raw))
		if token == "" {
			return nil, errors.New("token file is empty")
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return b.base.RoundTrip(req)
}

func trimLine(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	return s
}
