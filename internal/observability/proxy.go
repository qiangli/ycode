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

func (p *ProxyServer) reverseProxy(prefix string, target *url.URL) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		// Strip the prefix so /prometheus/graph -> /graph on the backend.
		req.URL.Path = strings.TrimPrefix(req.URL.Path, strings.TrimSuffix(prefix, "/"))
		if req.URL.Path == "" {
			req.URL.Path = "/"
		}
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
	b.WriteString("<!DOCTYPE html><html><head><title>ycode Observability</title>")
	b.WriteString("<style>body{font-family:sans-serif;max-width:800px;margin:40px auto;padding:0 20px}")
	b.WriteString("a{color:#0066cc;text-decoration:none}a:hover{text-decoration:underline}")
	b.WriteString("li{margin:8px 0}</style></head><body>")
	b.WriteString("<h1>ycode Observability Stack</h1><ul>")

	p.mu.RLock()
	for prefix := range p.routes {
		name := strings.Trim(prefix, "/")
		b.WriteString(fmt.Sprintf("<li><a href=\"%s\">%s</a></li>", prefix, name))
	}
	for prefix := range p.handlers {
		name := strings.Trim(prefix, "/")
		b.WriteString(fmt.Sprintf("<li><a href=\"%s\">%s</a></li>", prefix, name))
	}
	p.mu.RUnlock()

	b.WriteString("</ul><p><a href=\"/healthz\">/healthz</a> — aggregated health check</p>")
	b.WriteString("</body></html>")
	fmt.Fprint(w, b.String())
}

func (p *ProxyServer) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok","routes":%d}`, len(p.routes)+len(p.handlers))
}
