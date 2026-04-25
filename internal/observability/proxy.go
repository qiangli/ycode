package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strconv"
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
	mux        *http.ServeMux // stored for late handler registration
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
// If the proxy is already running, the handler is also registered on the live mux.
func (p *ProxyServer) AddHandler(pathPrefix string, handler http.Handler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handlers[pathPrefix] = handler
	if p.mux != nil {
		p.mux.Handle(pathPrefix, http.StripPrefix(strings.TrimSuffix(pathPrefix, "/"), handler))
	}
}

// Start begins serving HTTP requests.
func (p *ProxyServer) Start(_ context.Context) error {
	// Extract port from listenAddr for availability check.
	_, portStr, _ := net.SplitHostPort(p.listenAddr)
	if portStr != "" {
		port, _ := strconv.Atoi(portStr)
		if port > 0 && !IsPortAvailable(port) {
			return fmt.Errorf("proxy: port %d already in use", port)
		}
	}
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

	p.mux = mux
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
	b.WriteString(`<!DOCTYPE html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><title>ycode Pulse</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
html,body{height:100%;overflow:hidden}
body{font-family:-apple-system,BlinkMacSystemFont,"SF Pro Text",sans-serif;
background:linear-gradient(135deg,#2c3e50 0%,#3a4a5c 50%,#2c3e50 100%);color:#fff}

/* Toggle button — fixed top-left */
#btn-toggle{position:fixed;z-index:100;top:16px;left:16px;width:40px;height:40px;
border-radius:10px;border:none;cursor:pointer;display:flex;align-items:center;justify-content:center;
background:rgba(0,0,0,0.45);color:rgba(255,255,255,0.85);
box-shadow:0 2px 10px rgba(0,0,0,0.3);backdrop-filter:blur(8px);transition:all .15s}
#btn-toggle:hover{background:rgba(0,0,0,0.6);color:#fff}
#btn-toggle svg{width:18px;height:18px;fill:currentColor}
/* Home button — fixed bottom-left */
#btn-home{position:fixed;z-index:100;bottom:16px;left:16px;width:40px;height:40px;
border-radius:10px;border:none;cursor:pointer;display:flex;align-items:center;justify-content:center;
background:rgba(0,0,0,0.45);color:rgba(255,255,255,0.85);
box-shadow:0 2px 10px rgba(0,0,0,0.3);backdrop-filter:blur(8px);transition:all .15s}
#btn-home:hover{background:rgba(0,0,0,0.6);color:#fff}
#btn-home svg{width:18px;height:18px;fill:currentColor}

/* Grid home screen */
#grid-home{height:100%;overflow-y:auto;padding:60px 20px}
.grid-container{max-width:720px;margin:0 auto}
.grid-container h1{font-size:1.4em;font-weight:500;color:rgba(255,255,255,0.85);margin-bottom:28px}
.grid{display:grid;grid-template-columns:repeat(auto-fill,80px);gap:24px 28px}
.tile{display:flex;flex-direction:column;align-items:center;text-decoration:none;gap:8px;cursor:pointer}
.tile .icon{width:60px;height:60px;border-radius:14px;display:flex;align-items:center;
justify-content:center;font-size:26px;font-weight:600;color:#fff;
box-shadow:0 2px 8px rgba(0,0,0,0.3);transition:transform .15s}
.tile:hover .icon{transform:scale(1.08)}
.tile .label{font-size:11px;color:rgba(255,255,255,0.8);text-align:center;
max-width:80px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.grid-footer{margin-top:48px;padding-top:16px;border-top:1px solid rgba(255,255,255,0.15)}
.grid-footer a{color:rgba(255,255,255,0.5);text-decoration:none;font-size:12px}
.grid-footer a:hover{color:rgba(255,255,255,0.8)}

/* Grid iframe (full screen when an app is open) */
#grid-frame{position:absolute;top:0;left:0;width:100%;height:100%;border:none;background:#fff}

/* List view */
#list-view{display:flex;height:100%}
.list-panel{width:220px;min-width:220px;overflow-y:auto;padding-top:64px;
background:rgba(0,0,0,0.25);border-right:1px solid rgba(255,255,255,0.1)}
.list-item{display:flex;align-items:center;gap:12px;padding:12px 16px;cursor:pointer;
text-decoration:none;color:#fff;transition:background .12s;border-left:3px solid transparent}
.list-item:hover{background:rgba(255,255,255,0.06)}
.list-item.active{background:rgba(255,255,255,0.1);border-left-color:rgba(255,255,255,0.6)}
.list-item .icon{width:36px;height:36px;border-radius:10px;display:flex;align-items:center;
justify-content:center;font-size:16px;font-weight:600;color:#fff;flex-shrink:0;
box-shadow:0 1px 4px rgba(0,0,0,0.3)}
.list-item .label{font-size:13px;color:rgba(255,255,255,0.9)}
.list-content{flex:1;position:relative;background:#1a1a2e}
.list-content iframe{width:100%;height:100%;border:none}
.list-placeholder{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;
color:rgba(255,255,255,0.4);font-size:14px}
.hidden{display:none!important}
</style></head><body>

<!-- Toggle button — top-left -->
<button id="btn-toggle" onclick="toggleView()" title="Switch view">
<svg id="ico-list" viewBox="0 0 16 16"><rect x="1" y="2" width="4" height="3" rx="0.5"/><rect x="7" y="2.5" width="8" height="2" rx="0.5"/><rect x="1" y="7" width="4" height="3" rx="0.5"/><rect x="7" y="7.5" width="8" height="2" rx="0.5"/><rect x="1" y="12" width="4" height="3" rx="0.5"/><rect x="7" y="12.5" width="8" height="2" rx="0.5"/></svg>
<svg id="ico-grid" class="hidden" viewBox="0 0 16 16"><rect x="1" y="1" width="6" height="6" rx="1"/><rect x="9" y="1" width="6" height="6" rx="1"/><rect x="1" y="9" width="6" height="6" rx="1"/><rect x="9" y="9" width="6" height="6" rx="1"/></svg>
</button>
<!-- Home button — bottom-left (grid mode only, when app is open) -->
<button id="btn-home" class="hidden" onclick="gridBack()" title="Back to apps">
<svg viewBox="0 0 20 20"><path d="M10.707 2.293a1 1 0 00-1.414 0l-7 7a1 1 0 001.414 1.414L4 10.414V17a1 1 0 001 1h2a1 1 0 001-1v-2a1 1 0 011-1h2a1 1 0 011 1v2a1 1 0 001 1h2a1 1 0 001-1v-6.586l.293.293a1 1 0 001.414-1.414l-7-7z"/></svg>
</button>

<!-- Grid mode: home + iframe overlay -->
<div id="grid-mode">
<div id="grid-home">
<div class="grid-container">
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
			`<div class="tile" onclick="gridOpen('%s')"><div class="icon" style="background:%s">%s</div><span class="label">%s</span></div>`,
			prefix, color, initial, displayName,
		))
	}

	b.WriteString(`</div>
<div class="grid-footer"><a href="/healthz">/healthz</a></div>
</div></div>
<iframe id="grid-frame" class="hidden"></iframe>
</div>

<!-- List mode -->
<div id="list-view" class="hidden">
<div class="list-panel">`)

	for i, prefix := range allPrefixes {
		name := strings.Trim(prefix, "/")
		initial := strings.ToUpper(name[:1])
		displayName := strings.ToUpper(name[:1]) + name[1:]
		color := colors[i%len(colors)]
		b.WriteString(fmt.Sprintf(
			`<div class="list-item" data-href="%s" onclick="selectItem(this)"><div class="icon" style="background:%s">%s</div><span class="label">%s</span></div>`,
			prefix, color, initial, displayName,
		))
	}

	b.WriteString(`</div>
<div class="list-content">
<iframe id="list-frame"></iframe>
<div id="list-placeholder" class="list-placeholder">Select an item to view</div>
</div></div>

<script>
var mode='grid';
function toggleView(){
mode=mode==='grid'?'list':'grid';
document.getElementById('grid-mode').classList.toggle('hidden',mode!=='grid');
document.getElementById('list-view').classList.toggle('hidden',mode!=='list');
document.getElementById('ico-list').classList.toggle('hidden',mode==='list');
document.getElementById('ico-grid').classList.toggle('hidden',mode==='grid');
document.getElementById('btn-home').classList.add('hidden');
if(mode==='list'){var f=document.querySelector('.list-item');if(f&&!document.querySelector('.list-item.active'))selectItem(f)}
}
function gridOpen(href){
document.getElementById('grid-frame').src=href;
document.getElementById('grid-frame').classList.remove('hidden');
document.getElementById('grid-home').classList.add('hidden');
document.getElementById('btn-home').classList.remove('hidden');
document.getElementById('btn-toggle').classList.add('hidden');
}
function gridBack(){
document.getElementById('grid-frame').classList.add('hidden');
document.getElementById('grid-frame').src='';
document.getElementById('grid-home').classList.remove('hidden');
document.getElementById('btn-home').classList.add('hidden');
document.getElementById('btn-toggle').classList.remove('hidden');
}
function selectItem(el){
document.querySelectorAll('.list-item').forEach(function(e){e.classList.remove('active')});
el.classList.add('active');
document.getElementById('list-frame').src=el.dataset.href;
document.getElementById('list-placeholder').classList.add('hidden');
}
</script>
</body></html>`)
	fmt.Fprint(w, b.String())
}

func (p *ProxyServer) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok","routes":%d}`, len(p.routes)+len(p.handlers))
}
