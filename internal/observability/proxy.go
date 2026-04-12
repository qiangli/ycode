package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// ProxyServer routes requests to backend services based on path prefix.
// It supports both reverse-proxy backends (URL) and in-process handlers.
// It listens on a single fixed port (e.g. 58080).
type ProxyServer struct {
	listenAddr string
	mu         sync.RWMutex
	routes     map[string]*url.URL     // path prefix -> backend URL
	handlers   map[string]http.Handler // path prefix -> in-process handler
	server     *http.Server
}

// NewProxyServer creates a reverse proxy listening on bindAddr:port.
func NewProxyServer(bindAddr string, port int) *ProxyServer {
	return &ProxyServer{
		listenAddr: fmt.Sprintf("%s:%d", bindAddr, port),
		routes:     make(map[string]*url.URL),
		handlers:   make(map[string]http.Handler),
	}
}

// AddRoute registers a reverse-proxy backend for a path prefix.
// Example: AddRoute("/prometheus/", "http://127.0.0.1:39821")
func (p *ProxyServer) AddRoute(pathPrefix string, backend *url.URL) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.routes[pathPrefix] = backend
}

// AddHandler registers an in-process HTTP handler for a path prefix.
func (p *ProxyServer) AddHandler(pathPrefix string, handler http.Handler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handlers[pathPrefix] = handler
}

// Start begins serving HTTP requests.
func (p *ProxyServer) Start(_ context.Context) error {
	mux := http.NewServeMux()

	p.mu.RLock()
	// Sort routes by length descending so more specific paths match first.
	prefixes := make([]string, 0, len(p.routes))
	for prefix := range p.routes {
		prefixes = append(prefixes, prefix)
	}
	sort.Slice(prefixes, func(i, j int) bool {
		return len(prefixes[i]) > len(prefixes[j])
	})
	for _, prefix := range prefixes {
		backend := p.routes[prefix]
		mux.Handle(prefix, p.reverseProxy(prefix, backend))
	}

	// Register in-process handlers.
	handlerPrefixes := make([]string, 0, len(p.handlers))
	for prefix := range p.handlers {
		handlerPrefixes = append(handlerPrefixes, prefix)
	}
	sort.Slice(handlerPrefixes, func(i, j int) bool {
		return len(handlerPrefixes[i]) > len(handlerPrefixes[j])
	})
	for _, prefix := range handlerPrefixes {
		handler := p.handlers[prefix]
		mux.Handle(prefix, http.StripPrefix(strings.TrimSuffix(prefix, "/"), handler))
	}
	p.mu.RUnlock()

	// Landing page at root.
	mux.HandleFunc("/", p.landingPage)

	// Aggregated health check.
	mux.HandleFunc("/healthz", p.healthz)

	p.server = &http.Server{
		Addr:    p.listenAddr,
		Handler: mux,
	}

	slog.Info("proxy: starting", "addr", p.listenAddr)
	go func() {
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("proxy: listen failed", "error", err)
		}
	}()
	return nil
}

// Stop gracefully shuts down the proxy server.
func (p *ProxyServer) Stop(ctx context.Context) error {
	if p.server == nil {
		return nil
	}
	return p.server.Shutdown(ctx)
}

// Addr returns the listen address.
func (p *ProxyServer) Addr() string {
	return p.listenAddr
}

func (p *ProxyServer) reverseProxy(_ string, target *url.URL) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		// Forward the full path including the prefix. Backends are configured
		// with their path prefix and expect to receive the prefixed path.
		req.Host = target.Host
	}
	return proxy
}

func (p *ProxyServer) landingPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><title>ycode Observability</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,"SF Pro Text",sans-serif;
background:linear-gradient(135deg,#2c3e50 0%,#3a4a5c 50%,#2c3e50 100%);
min-height:100vh;padding:60px 20px;color:#fff}
.container{max-width:720px;margin:0 auto}
h1{font-size:1.4em;font-weight:500;color:rgba(255,255,255,0.85);margin-bottom:28px}
.grid{display:grid;grid-template-columns:repeat(auto-fill,80px);gap:24px 28px}
.tile{display:flex;flex-direction:column;align-items:center;text-decoration:none;gap:8px}
.icon{width:60px;height:60px;border-radius:14px;display:flex;align-items:center;
justify-content:center;font-size:26px;font-weight:600;color:#fff;
box-shadow:0 2px 8px rgba(0,0,0,0.3);transition:transform .15s}
.tile:hover .icon{transform:scale(1.08)}
.label{font-size:11px;color:rgba(255,255,255,0.8);text-align:center;
max-width:80px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.footer{margin-top:48px;padding-top:16px;border-top:1px solid rgba(255,255,255,0.15)}
.footer a{color:rgba(255,255,255,0.5);text-decoration:none;font-size:12px}
.footer a:hover{color:rgba(255,255,255,0.8)}
</style></head><body><div class="container">
<h1>Favorites</h1><div class="grid">`)

	// Predefined colors for consistent icon appearance.
	colors := []string{
		"#3478f6", "#34c759", "#ff9500", "#ff3b30",
		"#af52de", "#5ac8fa", "#ff2d55", "#00c7be",
	}

	p.mu.RLock()
	allPrefixes := make([]string, 0, len(p.routes)+len(p.handlers))
	for prefix := range p.routes {
		allPrefixes = append(allPrefixes, prefix)
	}
	for prefix := range p.handlers {
		allPrefixes = append(allPrefixes, prefix)
	}
	p.mu.RUnlock()
	sort.Strings(allPrefixes)

	for i, prefix := range allPrefixes {
		name := strings.Trim(prefix, "/")
		initial := strings.ToUpper(name[:1])
		displayName := strings.ToUpper(name[:1]) + name[1:]
		color := colors[i%len(colors)]
		b.WriteString(fmt.Sprintf(
			`<a class="tile" href="%s"><div class="icon" style="background:%s">%s</div><span class="label">%s</span></a>`,
			prefix, color, initial, displayName,
		))
	}

	b.WriteString(`</div>
<div class="footer"><a href="/healthz">/healthz</a></div>
</div></body></html>`)
	fmt.Fprint(w, b.String())
}

func (p *ProxyServer) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok","routes":%d}`, len(p.routes)+len(p.handlers))
}
